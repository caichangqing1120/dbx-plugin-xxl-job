package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"

	sdk "github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
)

type mcpDefinition struct{ name, method, access, description, fields, required string }

var mcpDefinitions = []mcpDefinition{
	{"xxljob_info", "xxljob/info", "read", "Inspect current connection metadata and service access.", "", ""},
	{"xxljob_groups", "xxljob/groups", "read", "List authorized executors.", "", ""},
	{"xxljob_jobs", "xxljob/jobs", "read", "List jobs with bounded pagination and existing filters.", "start length jobGroup triggerStatus jobDesc executorHandler author", ""},
	{"xxljob_logs", "xxljob/logs", "read", "List scheduling logs; query before requesting log content.", "start length jobGroup jobId logStatus filterTime", ""},
	{"xxljob_log_content", "xxljob/logContent", "read", "Read bounded log content using the identity from a previous logs result.", "executorAddress triggerTime logId fromLineNum", "logId fromLineNum"},
	{"xxljob_jobs_by_group", "xxljob/jobsByGroup", "read", "List jobs belonging to an authorized executor.", "jobGroup", "jobGroup"},
	{"xxljob_start", "xxljob/start", "write", "Enable scheduling for an existing job; explicit confirmation required.", "id", "id"},
	{"xxljob_stop", "xxljob/stop", "write", "Disable scheduling for an existing job; explicit confirmation required.", "id", "id"},
	{"xxljob_trigger", "xxljob/trigger", "full", "Execute an existing job once; explicit confirmation required. Never automatically retry unknown outcomes.", "id executorParam addressList", "id"},
	{"xxljob_save_job", "xxljob/saveJob", "full", "Create or edit a job using existing validation; explicit confirmation required.", "id jobGroup scheduleType scheduleConf misfireStrategy jobDesc author alarmEmail executorRouteStrategy executorHandler executorParam executorBlockStrategy executorTimeout executorFailRetryCount glueType childJobId", "jobGroup jobDesc author executorRouteStrategy executorBlockStrategy executorTimeout executorFailRetryCount glueType scheduleType misfireStrategy"},
	{"xxljob_remove_job", "xxljob/removeJob", "full", "Delete a job; explicit confirmation required.", "id", "id"},
	{"xxljob_save_group", "xxljob/saveGroup", "full", "Administrator executor create/edit; explicit confirmation required.", "id appname title addressType addressList", "appname title addressType"},
	{"xxljob_remove_group", "xxljob/removeGroup", "full", "Administrator executor deletion; explicit confirmation required.", "id", "id"},
}

type mcpCacheEntry struct {
	fingerprint [32]byte
	session     *session
	used        time.Time
	secrets     []string
}
type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations"`
}

func mcpSchema(d mcpDefinition) map[string]any {
	properties := map[string]any{"connectionId": map[string]any{"type": "string", "description": "Optional host-bound connection ID; must match lifecycle.connection.id.", "minLength": 1, "maxLength": 256}}
	numeric := map[string][2]float64{"id": {1, 2147483647}, "jobGroup": {-1, 2147483647}, "jobId": {0, 2147483647}, "start": {0, 10000000}, "length": {1, 100}, "triggerStatus": {-1, 1}, "logStatus": {-1, 3}, "logId": {1, 9007199254740991}, "triggerTime": {1, 9007199254740991}, "fromLineNum": {1, 9007199254740991}, "executorTimeout": {0, 2147483647}, "executorFailRetryCount": {0, 2147483647}, "addressType": {0, 1}, "order": {0, 2147483647}, "taskLimit": {1, 2000}}
	enums := map[string][]string{"scheduleType": {"NONE", "CRON", "FIX_RATE"}, "misfireStrategy": {"DO_NOTHING", "FIRE_ONCE_NOW"}, "executorRouteStrategy": {"FIRST", "LAST", "ROUND", "RANDOM", "CONSISTENT_HASH", "LEAST_FREQUENTLY_USED", "LEAST_RECENTLY_USED", "FAILOVER", "BUSYOVER", "SHARDING_BROADCAST"}, "executorBlockStrategy": {"SERIAL_EXECUTION", "DISCARD_LATER", "COVER_EARLY"}}
	for _, key := range strings.Fields(d.fields) {
		schema := map[string]any{"type": "string", "description": "Existing service form field: " + key + ".", "maxLength": 256 * 1024}
		if bounds, ok := numeric[key]; ok {
			if key == "jobGroup" && !strings.HasSuffix(d.method, "/jobs") {
				bounds[0] = 1
				if strings.HasSuffix(d.method, "/logs") {
					bounds[0] = 0
				}
			}
			schema = map[string]any{"type": "integer", "description": "Existing service integer field: " + key + ".", "minimum": bounds[0], "maximum": bounds[1]}
		}
		if values, ok := enums[key]; ok {
			schema["enum"] = values
		}
		if key == "executorAddress" || key == "triggerTime" {
			schema["description"] = "Log identity from a prior logs result; required by legacy Admin, ignored by public Admin 3.4."
		}
		properties[key] = schema
	}
	required := strings.Fields(d.required)
	if d.access != "read" {
		properties["confirmed"] = map[string]any{"type": "boolean", "description": "Explicit caller confirmation of this exact mutation; never inferred or enabled automatically."}
		required = append(required, "confirmed")
	}
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func mcpValidate(d mcpDefinition, args map[string]any) error {
	schema := mcpSchema(d)
	props := schema["properties"].(map[string]any)
	for _, key := range schema["required"].([]string) {
		if _, ok := args[key]; !ok {
			return fmt.Errorf("missing argument %s", key)
		}
	}
	for key, value := range args {
		field, ok := props[key].(map[string]any)
		if !ok {
			return fmt.Errorf("unsupported argument %s", key)
		}
		switch field["type"] {
		case "string":
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("%s must be a string", key)
			}
			if len(text) > field["maxLength"].(int) {
				return fmt.Errorf("%s is too large", key)
			}
			if values, ok := field["enum"].([]string); ok && !oneOf(text, values...) {
				return fmt.Errorf("invalid %s", key)
			}
		case "integer":
			number, ok := value.(float64)
			if !ok || math.Trunc(number) != number || number < field["minimum"].(float64) || number > field["maximum"].(float64) {
				return fmt.Errorf("invalid integer %s", key)
			}
		case "boolean":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("%s must be a boolean", key)
			}
		}
	}
	return nil
}

func (p *plugin) handleMCP(method string, raw json.RawMessage) (any, *sdk.PluginError) {
	if method == "mcp/tools" {
		tools := make([]mcpTool, 0, len(mcpDefinitions))
		for _, d := range mcpDefinitions {
			tools = append(tools, mcpTool{d.name, d.description, mcpSchema(d), map[string]any{"readOnlyHint": d.access == "read", "destructiveHint": d.access == "full", "idempotentHint": d.access == "read", "openWorldHint": true}})
		}
		return map[string]any{"tools": tools}, nil
	}
	result, e := p.mcpCall(raw)
	if e != nil {
		return mcpResult(e.Error(), true), nil
	}
	return result, nil
}
func mcpResult(text string, failed bool) any {
	return map[string]any{"content": []map[string]string{{"type": "text", "text": text}}, "isError": failed}
}
func (p *plugin) mcpCall(raw json.RawMessage) (any, error) {
	if len(raw) > 1024*1024 {
		return nil, errors.New("MCP request exceeds 1 MiB")
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal(raw, &envelope) != nil || envelope == nil {
		return nil, errors.New("invalid MCP call object")
	}
	for key := range envelope {
		if key != "tool" && key != "arguments" && key != "lifecycle" {
			return nil, errors.New("unsupported MCP call field")
		}
	}
	var name string
	if json.Unmarshal(envelope["tool"], &name) != nil {
		return nil, errors.New("missing tool name")
	}
	var definition *mcpDefinition
	for i := range mcpDefinitions {
		if mcpDefinitions[i].name == name {
			definition = &mcpDefinitions[i]
			break
		}
	}
	if definition == nil {
		return nil, errors.New("unknown MCP tool")
	}
	var args map[string]any
	if json.Unmarshal(envelope["arguments"], &args) != nil || args == nil {
		return nil, errors.New("arguments must be an object")
	}
	if e := mcpValidate(*definition, args); e != nil {
		return nil, e
	}
	var lifecycle params
	if json.Unmarshal(envelope["lifecycle"], &lifecycle) != nil {
		return nil, errors.New("missing lifecycle connection")
	}
	id := lifecycle.Connection.ID
	if id == "" || strings.TrimSpace(id) != id || len(id) > 256 {
		return nil, errors.New("missing or invalid lifecycle connection ID")
	}
	if value, ok := args["connectionId"]; ok && value != id {
		return nil, errors.New("argument connectionId does not match lifecycle connection ID")
	}
	if definition.access != "read" {
		if lifecycle.Connection.ReadOnly || lifecycle.Connection.Config.ReadOnly {
			return nil, errors.New("当前连接为只读，禁止修改或执行任务")
		}
		if args["confirmed"] != true {
			return nil, errors.New("操作尚未确认")
		}
	}
	form := map[string]any{}
	for _, key := range strings.Fields(definition.fields) {
		if value, ok := args[key]; ok {
			form[key] = value
		}
	}
	// Validate before authentication; only existing whitelisted handlers can reach the transport.
	built, e := buildForm(definition.method, form, lifecycle.Connection.Config.AdminVersion)
	if e != nil {
		return nil, e
	}
	if definition.access != "read" {
		if e := validateMutation(definition.method, built); e != nil {
			return nil, e
		}
	}
	fingerprintBytes, _ := json.Marshal(struct {
		Connection connection
		Runtime    runtimeEndpoint
	}{lifecycle.Connection, lifecycle.Runtime})
	fingerprint := sha256.Sum256(fingerprintBytes)
	p.mcpMu.Lock()
	defer p.mcpMu.Unlock()
	if p.mcpSessions == nil {
		p.mcpSessions = map[string]*mcpCacheEntry{}
	}
	entry := p.mcpSessions[id]
	if entry != nil && entry.fingerprint != fingerprint {
		entry.session.close()
		delete(p.mcpSessions, id)
		entry = nil
	}
	if entry == nil {
		if len(p.mcpSessions) >= 32 {
			var oldestID string
			var oldest time.Time
			for key, value := range p.mcpSessions {
				if oldestID == "" || value.used.Before(oldest) {
					oldestID, oldest = key, value.used
				}
			}
			p.mcpSessions[oldestID].session.close()
			delete(p.mcpSessions, oldestID)
		}
		s, e := newSession(lifecycle.Connection, lifecycle.Runtime)
		if e != nil {
			return nil, errors.New(mcpSanitize(e.Error(), []string{lifecycle.Connection.Password}))
		}
		if e = s.probe(); e != nil {
			s.close()
			return nil, errors.New(mcpSanitize(e.Error(), []string{lifecycle.Connection.Password}))
		}
		entry = &mcpCacheEntry{fingerprint: fingerprint, session: s}
		entry.secrets = mcpCookieSecrets(s, nil)
		p.mcpSessions[id] = entry
	}
	entry.used = time.Now()
	// Never replace or disconnect the workbench's sessions. Related MCP calls share only their own provenance.
	worker := newPlugin()
	worker.sessions[id] = entry.session
	value, e := worker.handle(definition.method, params{ConnectionID: id, Form: form, Confirmed: args["confirmed"] == true})
	entry.secrets = mcpCookieSecrets(entry.session, entry.secrets)
	redactions := append([]string{lifecycle.Connection.Password}, entry.secrets...)
	if e != nil {
		return nil, errors.New(mcpSanitize(e.Error(), redactions))
	}
	encoded, e := json.Marshal(value)
	if e != nil {
		return nil, errors.New("cannot encode MCP result")
	}
	var output any
	if json.Unmarshal(encoded, &output) != nil {
		return nil, errors.New("invalid MCP result")
	}
	output = mcpRedact(output, redactions)
	encoded, e = json.Marshal(output)
	if e != nil {
		return nil, errors.New("cannot encode MCP result")
	}
	if len(encoded) > 1024*1024 {
		return nil, errors.New("MCP result exceeds 1 MiB; narrow the query. The operation may already have completed; do not automatically retry")
	}
	return mcpResult(string(encoded), false), nil
}

var mcpSecretLabels = regexp.MustCompile(`(?i)(["']?(?:password|passwd|token|secret|api[_-]?key|access[_-]?key|credentials?)["']?\s*[:=]\s*)("[^"\\]*(?:\\.[^"\\]*)*"|'[^']*'|[^\s,;}]+)`)
var mcpHeaderSecrets = regexp.MustCompile(`(?i)(["']?(?:authorization|set-cookie|cookie)["']?\s*[:=]\s*)[^\r\n]+`)
var mcpURL = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)
var mcpBearer = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/=-]+`)

func mcpSanitize(text string, secrets []string) string {
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[REDACTED]")
		}
	}
	// Structured task parameters can contain JSON inside a string. Decode those
	// objects/arrays before recursing so quoted keys retain their semantics.
	var structured any
	if json.Unmarshal([]byte(text), &structured) == nil {
		switch structured.(type) {
		case map[string]any, []any:
			if encoded, err := json.Marshal(mcpRedact(structured, secrets)); err == nil {
				text = string(encoded)
			}
		}
	}
	text = mcpURL.ReplaceAllStringFunc(text, func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "[REDACTED URL]"
		}
		parsed.User = nil
		values := parsed.Query()
		for key := range values {
			normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
			if strings.Contains(normalized, "password") || strings.Contains(normalized, "token") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "credential") || normalized == "apikey" || normalized == "accesskey" || normalized == "authorization" {
				values.Set(key, "[REDACTED]")
			}
		}
		parsed.RawQuery = values.Encode()
		parsed.Fragment, parsed.RawFragment = "", ""
		return parsed.String()
	})
	text = mcpSecretLabels.ReplaceAllString(text, `${1}"[REDACTED]"`)
	text = mcpHeaderSecrets.ReplaceAllString(text, `${1}"[REDACTED]"`)
	text = mcpBearer.ReplaceAllString(text, "Bearer [REDACTED]")
	if len(text) > 1024*1024 {
		return text[:1024*1024] + " [truncated]"
	}
	return text
}
func mcpRedact(value any, secrets []string) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
			if strings.Contains(normalized, "password") || strings.Contains(normalized, "secret") || strings.Contains(normalized, "token") || strings.Contains(normalized, "cookie") || strings.Contains(normalized, "credential") || normalized == "authorization" || normalized == "apikey" || normalized == "accesskey" || normalized == "gluesource" {
				v[key] = "[REDACTED]"
			} else {
				v[key] = mcpRedact(item, secrets)
			}
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = mcpRedact(item, secrets)
		}
		return v
	case string:
		return mcpSanitize(v, secrets)
	default:
		return value
	}
}

func mcpCookieSecrets(s *session, known []string) []string {
	if s.client.Jar != nil {
		u, _ := url.Parse(s.base + "/")
		for _, cookie := range s.client.Jar.Cookies(u) {
			found := cookie.Value == ""
			for _, value := range known {
				if value == cookie.Value {
					found = true
					break
				}
			}
			if !found {
				known = append(known, cookie.Value)
			}
		}
	}
	if len(known) > 64 {
		known = append([]string{}, known[len(known)-64:]...)
	}
	return known
}
