package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk "github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
)

const pluginID = "io.github.caichangqing1120.xxljob"
const version = "0.2.2"

type plugin struct {
	mcpMu       sync.Mutex
	mcpSessions map[string]*mcpCacheEntry
	lifecycle   sync.Mutex
	mu          sync.RWMutex
	sessions    map[string]*session
}

func newPlugin() *plugin { return &plugin{sessions: map[string]*session{}} }

type params struct {
	Connection   connection      `json:"connection"`
	Runtime      runtimeEndpoint `json:"runtime"`
	ConnectionID string          `json:"connectionId"`
	Form         map[string]any  `json:"form"`
	Confirmed    bool            `json:"confirmed"`
}

func (p *plugin) Handle(_ sdk.RequestContext, method string, raw json.RawMessage, _ *sdk.Emitter) (any, *sdk.PluginError) {
	if method == "mcp/tools" || method == "mcp/call" {
		return p.handleMCP(method, raw)
	}
	var v params
	if json.Unmarshal(raw, &v) != nil {
		return nil, sdk.NewError(-32602, "无效请求参数")
	}
	// A host reconnect/disconnect releases only this identity's MCP cache, never unrelated workbench state.
	if (method == "connection/connect" || method == "connection/disconnect") && v.Connection.ID != "" {
		p.mcpMu.Lock()
		if entry := p.mcpSessions[v.Connection.ID]; entry != nil {
			entry.session.close()
			delete(p.mcpSessions, v.Connection.ID)
		}
		p.mcpMu.Unlock()
	}
	result, e := p.handle(method, v)
	if e != nil {
		return nil, sdk.NewError(-32000, e.Error())
	}
	return result, nil
}

var reads = map[string]string{"xxljob/jobs": "/jobinfo/pageList", "xxljob/logs": "/joblog/pageList", "xxljob/logContent": "/joblog/logDetailCat", "xxljob/jobsByGroup": "/joblog/getJobsByGroup"}
var mutations = map[string]string{"xxljob/start": "/jobinfo/start", "xxljob/stop": "/jobinfo/stop", "xxljob/trigger": "/jobinfo/trigger", "xxljob/removeJob": "/jobinfo/remove", "xxljob/saveJob": "/jobinfo/add", "xxljob/saveGroup": "/jobgroup/save", "xxljob/removeGroup": "/jobgroup/remove"}

func (p *plugin) handle(method string, v params) (any, error) {
	switch method {
	case "connection/test", "connection/connect":
		p.lifecycle.Lock()
		defer p.lifecycle.Unlock()
		if method == "connection/connect" && v.Connection.ID == "" {
			return nil, errors.New("缺少连接 ID")
		}
		if method == "connection/connect" {
			p.mu.Lock()
			old := p.sessions[v.Connection.ID]
			delete(p.sessions, v.Connection.ID)
			p.mu.Unlock()
			if old != nil {
				old.close()
			}
		}
		s, e := newSession(v.Connection, v.Runtime)
		if e != nil {
			return nil, e
		}
		if e = s.probe(); e != nil {
			s.close()
			return nil, e
		}
		if method == "connection/test" {
			s.close()
			return map[string]any{"success": true, "message": "XXL-JOB 登录及权限页面验证成功（Admin " + s.adminVersion + ".x）"}, nil
		}
		p.mu.Lock()
		old := p.sessions[v.Connection.ID]
		p.sessions[v.Connection.ID] = s
		p.mu.Unlock()
		if old != nil {
			old.close()
		}
		return map[string]any{"success": true}, nil
	case "connection/disconnect":
		p.lifecycle.Lock()
		defer p.lifecycle.Unlock()
		if v.Connection.ID == "" {
			return nil, errors.New("缺少连接 ID")
		}
		p.mu.Lock()
		old := p.sessions[v.Connection.ID]
		delete(p.sessions, v.Connection.ID)
		p.mu.Unlock()
		if old != nil {
			old.close()
		}
		return map[string]any{"success": true}, nil
	}
	path, isRead := reads[method]
	writePath, isWrite := mutations[method]
	if !isRead && !isWrite && method != "xxljob/info" && method != "xxljob/groups" {
		return nil, fmt.Errorf("不支持的操作 %s", method)
	}
	p.mu.RLock()
	s := p.sessions[v.ConnectionID]
	p.mu.RUnlock()
	if s == nil {
		return nil, errors.New("连接尚未建立或已经断开，请重新连接")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.active(); e != nil {
		return nil, e
	}
	if method == "xxljob/info" {
		if e := s.refreshAccess(); e != nil {
			return nil, e
		}
		return map[string]any{"name": s.name, "baseUrl": s.base, "environment": s.environment, "readOnly": s.readOnly, "authMode": "password", "admin": s.admin, "username": s.username, "adminVersion": s.adminVersion}, nil
	}
	if method == "xxljob/groups" {
		return s.groups()
	}
	if isWrite {
		if s.readOnly {
			return nil, errors.New("当前连接为只读，禁止修改或执行任务")
		}
		if !v.Confirmed {
			return nil, errors.New("操作尚未确认")
		}
		path = writePath
	}
	form, e := buildForm(method, v.Form, s.adminVersion)
	if e != nil {
		return nil, e
	}
	if e := s.refreshAccess(); e != nil {
		return nil, e
	}
	if method == "xxljob/jobsByGroup" {
		id, _ := strconv.Atoi(form.Get("jobGroup"))
		rows, e := s.jobsByGroup(id)
		if e != nil {
			return nil, e
		}
		options := []map[string]any{}
		for _, v := range rows {
			row := v.(map[string]any)
			options = append(options, map[string]any{"id": row["id"], "jobGroup": row["jobGroup"], "jobDesc": row["jobDesc"]})
		}
		return options, nil
	}
	if method == "xxljob/jobs" || method == "xxljob/logs" || method == "xxljob/saveJob" {
		id, _ := strconv.Atoi(form.Get("jobGroup"))
		if !s.permitGroup(id) {
			return nil, errors.New("没有该执行器的权限，请选择已授权执行器")
		}
	}
	if method == "xxljob/logs" && form.Get("jobId") != "0" {
		if e := s.permitJob(form.Get("jobId")); e != nil {
			return nil, e
		}
	}
	if method == "xxljob/logContent" {
		id, _ := strconv.ParseInt(form.Get("logId"), 10, 64)
		row := s.logRows[id]
		if row == nil {
			return nil, errors.New("请先查询调度日志，再查看该日志内容")
		}
		g, ok := row["jobGroup"].(float64)
		if !ok || !s.permitGroup(int(g)) {
			return nil, errors.New("没有该日志所属执行器的权限")
		}
		if s.adminVersion == "2.3" {
			address, _ := row["executorAddress"].(string)
			ms, e := logMillis(row["triggerTime"])
			if e != nil {
				return nil, e
			}
			if form.Get("executorAddress") != address || form.Get("triggerTime") != strconv.FormatInt(ms, 10) {
				return nil, errors.New("日志身份与查询结果不一致，请刷新后重试")
			}
		}
	}
	if isWrite {
		if e = validateMutation(method, form); e != nil {
			return nil, e
		}
		if strings.Contains(method, "Group") && !s.admin {
			return nil, errors.New("仅管理员可以管理执行器")
		}
		if !strings.Contains(method, "Group") && form.Get("id") != "" {
			if e := s.permitJob(form.Get("id")); e != nil {
				return nil, e
			}
		}
		if method == "xxljob/saveJob" && form.Get("id") != "" {
			path = "/jobinfo/update"
		}
		if method == "xxljob/saveGroup" && form.Get("id") != "" {
			path = "/jobgroup/update"
			groups, e := s.groups()
			if e != nil {
				return nil, e
			}
			match := false
			for _, g := range groups {
				if strconv.Itoa(g.ID) == form.Get("id") {
					match = true
					if g.Appname != form.Get("appname") {
						return nil, errors.New("编辑执行器不能变更 AppName")
					}
				}
			}
			if !match {
				return nil, errors.New("执行器不存在，请刷新后重试")
			}
		}
	}
	path, form, e = s.adaptRequest(method, path, form)
	if e != nil {
		return nil, e
	}
	result, e := s.request(path, form, isWrite)
	if e != nil {
		return nil, e
	}
	if method == "xxljob/jobs" || method == "xxljob/logs" {
		rows := result.(map[string]any)["data"].([]any)
		if len(s.logRows) > 1000 {
			s.logRows = map[int64]map[string]any{}
		}
		for _, v := range rows {
			row, ok := v.(map[string]any)
			if !ok {
				return nil, errors.New("分页记录不是对象")
			}
			g, ok := row["jobGroup"].(float64)
			if !s.admin && (!ok || !s.permitGroup(int(g))) {
				return nil, errors.New("服务返回未授权记录，已拒绝显示")
			}
			delete(row, "glueSource")
			if method == "xxljob/logs" {
				if id, ok := row["id"].(float64); ok && id > 0 && id == float64(int64(id)) {
					s.logRows[int64(id)] = row
				}
			}
		}
	}
	return result, nil
}
func logMillis(value any) (int64, error) {
	if n, ok := value.(float64); ok && n > 0 && n == float64(int64(n)) {
		return int64(n), nil
	}
	if v, ok := value.(string); ok {
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000-0700"} {
			if t, e := time.Parse(layout, v); e == nil {
				return t.UnixMilli(), nil
			}
		}
	}
	return 0, errors.New("日志触发时间不符合原版 JSON 日期契约")
}
func scalar(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), nil
		}
	}
	return "", errors.New("表单字段必须是字符串或整数")
}
func buildForm(method string, input map[string]any, version ...string) (url.Values, error) {
	fields := map[string]string{
		"xxljob/jobs":        "start length jobGroup triggerStatus jobDesc executorHandler author",
		"xxljob/jobsByGroup": "jobGroup",
		"xxljob/logs":        "start length jobGroup jobId logStatus filterTime",
		"xxljob/logContent":  "executorAddress triggerTime logId fromLineNum",
		"xxljob/saveJob":     "id jobGroup scheduleType scheduleConf misfireStrategy jobDesc author alarmEmail executorRouteStrategy executorHandler executorParam executorBlockStrategy executorTimeout executorFailRetryCount glueType childJobId",
		"xxljob/saveGroup":   "id appname title addressType addressList",
		"xxljob/trigger":     "id executorParam addressList",
		"xxljob/start":       "id", "xxljob/stop": "id", "xxljob/removeJob": "id", "xxljob/removeGroup": "id",
	}
	form := url.Values{}
	for _, key := range strings.Fields(fields[method]) {
		if value, ok := input[key]; ok && value != nil {
			str, e := scalar(value)
			if e != nil {
				return nil, fmt.Errorf("%s: %w", key, e)
			}
			if len(str) > 256*1024 {
				return nil, errors.New("表单字段过大")
			}
			form.Set(key, str)
		}
	}
	if strings.Contains(fields[method], "start length") {
		defaults := map[string]string{"start": "0", "length": "25"}
		if method == "xxljob/jobs" {
			defaults["jobGroup"] = "-1"
			defaults["triggerStatus"] = "-1"
		}
		if method == "xxljob/logs" {
			defaults["jobGroup"] = "0"
			defaults["jobId"] = "0"
			defaults["logStatus"] = "-1"
		}
		for key, value := range defaults {
			if form.Get(key) == "" {
				form.Set(key, value)
			}
		}
		if e := intRange(form, "start", 0, 10000000); e != nil {
			return nil, e
		}
		if e := intRange(form, "length", 1, 100); e != nil {
			return nil, e
		}
		if method == "xxljob/jobs" {
			if e := intRange(form, "jobGroup", -1, 2147483647); e != nil {
				return nil, e
			}
			if e := intRange(form, "triggerStatus", -1, 1); e != nil {
				return nil, e
			}
		}
		if method == "xxljob/logs" {
			for key, min := range map[string]int64{"jobGroup": 0, "jobId": 0, "logStatus": -1} {
				max := int64(2147483647)
				if key == "logStatus" {
					max = 3
				}
				if e := intRange(form, key, min, max); e != nil {
					return nil, e
				}
			}
		}
	}
	if method == "xxljob/jobsByGroup" {
		if e := intRange(form, "jobGroup", 1, 2147483647); e != nil {
			return nil, e
		}
	}
	if method == "xxljob/trigger" {
		if _, ok := form["executorParam"]; !ok {
			form.Set("executorParam", "")
		}
		if _, ok := form["addressList"]; !ok {
			form.Set("addressList", "")
		}
	}
	if method == "xxljob/logContent" {
		keys := []string{"logId", "fromLineNum"}
		if len(version) == 0 || version[0] != "3.4" {
			keys = append(keys, "triggerTime")
		}
		for _, key := range keys {
			if e := intRange(form, key, 1, 9007199254740991); e != nil {
				return nil, e
			}
		}
		if len(version) == 0 || version[0] != "3.4" {
			u, e := url.Parse(form.Get("executorAddress"))
			if e != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
				return nil, errors.New("执行器地址无效")
			}
		}
	}
	return form, nil
}
func intRange(form url.Values, key string, min, max int64) error {
	n, e := strconv.ParseInt(form.Get(key), 10, 64)
	if e != nil || n < min || n > max {
		return fmt.Errorf("%s 必须是 %d..%d 的整数", key, min, max)
	}
	return nil
}
func required(form url.Values, keys ...string) error {
	for _, key := range keys {
		if strings.TrimSpace(form.Get(key)) == "" {
			return fmt.Errorf("%s 不能为空", key)
		}
	}
	return nil
}
func oneOf(value string, options ...string) bool {
	for _, o := range options {
		if value == o {
			return true
		}
	}
	return false
}
func validateMutation(method string, form url.Values) error {
	if method != "xxljob/saveJob" && method != "xxljob/saveGroup" {
		return intRange(form, "id", 1, 2147483647)
	}
	if form.Get("id") != "" {
		if e := intRange(form, "id", 1, 2147483647); e != nil {
			return e
		}
	}
	if method == "xxljob/saveGroup" {
		if e := required(form, "appname", "title"); e != nil {
			return e
		}
		if len(form.Get("appname")) < 4 || len(form.Get("appname")) > 64 {
			return errors.New("AppName 长度必须为 4..64")
		}
		for key, bounds := range map[string][2]int64{"addressType": {0, 1}} {
			if e := intRange(form, key, bounds[0], bounds[1]); e != nil {
				return e
			}
		}
		if form.Get("addressType") == "1" {
			if e := required(form, "addressList"); e != nil {
				return e
			}
			for _, address := range strings.Split(form.Get("addressList"), ",") {
				u, e := url.Parse(strings.TrimSpace(address))
				if e != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
					return errors.New("手动注册地址必须是逗号分隔的 HTTP/HTTPS URL")
				}
			}
		}
		return nil
	}
	if e := required(form, "jobDesc", "author"); e != nil {
		return e
	}
	if !oneOf(form.Get("scheduleType"), "NONE", "CRON", "FIX_RATE") {
		return errors.New("调度类型无效")
	}
	if form.Get("scheduleType") == "CRON" {
		if e := required(form, "scheduleConf"); e != nil {
			return e
		}
	}
	if form.Get("scheduleType") == "FIX_RATE" {
		if e := intRange(form, "scheduleConf", 1, 2147483647); e != nil {
			return e
		}
	}
	if !oneOf(form.Get("misfireStrategy"), "DO_NOTHING", "FIRE_ONCE_NOW") {
		return errors.New("调度过期策略无效")
	}
	if e := intRange(form, "jobGroup", 1, 2147483647); e != nil {
		return e
	}
	for _, key := range []string{"executorTimeout", "executorFailRetryCount"} {
		if e := intRange(form, key, 0, 2147483647); e != nil {
			return e
		}
	}
	if form.Get("id") == "" && form.Get("glueType") != "BEAN" {
		return errors.New("首版仅支持新增 BEAN 任务")
	}
	if form.Get("glueType") == "BEAN" {
		if e := required(form, "executorHandler"); e != nil {
			return e
		}
	}
	if !oneOf(form.Get("executorRouteStrategy"), "FIRST", "LAST", "ROUND", "RANDOM", "CONSISTENT_HASH", "LEAST_FREQUENTLY_USED", "LEAST_RECENTLY_USED", "FAILOVER", "BUSYOVER", "SHARDING_BROADCAST") {
		return errors.New("路由策略无效")
	}
	if !oneOf(form.Get("executorBlockStrategy"), "SERIAL_EXECUTION", "DISCARD_LATER", "COVER_EARLY") {
		return errors.New("阻塞策略无效")
	}
	if ids := strings.TrimSpace(form.Get("childJobId")); ids != "" {
		for _, id := range strings.Split(ids, ",") {
			n, e := strconv.ParseInt(id, 10, 32)
			if e != nil || n < 1 {
				return errors.New("子任务 ID 必须是逗号分隔的正整数")
			}
		}
	}
	return nil
}
func main() {
	server := sdk.NewServer(sdk.Metadata{ID: pluginID, Version: version, Capabilities: []string{"connections"}}, newPlugin())
	if e := server.Serve(); e != nil {
		log.Fatal(e)
	}
}
