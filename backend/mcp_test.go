package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	sdk "github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
)

// These literal expectations catch omissions, access downgrades and wrong endpoint dispatch.
var expectedMCP = map[string]string{
	"xxljob_info": "read", "xxljob_groups": "read", "xxljob_jobs": "read", "xxljob_logs": "read", "xxljob_log_content": "read", "xxljob_jobs_by_group": "read",
	"xxljob_start": "write", "xxljob_stop": "write", "xxljob_trigger": "full", "xxljob_save_job": "full", "xxljob_remove_job": "full", "xxljob_save_group": "full", "xxljob_remove_group": "full",
}

func mcpInvoke(t *testing.T, p *plugin, tool string, args map[string]any, lifecycle any) (bool, string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"tool": tool, "arguments": args, "lifecycle": lifecycle})
	value, e := p.Handle(sdk.RequestContext{}, "mcp/call", b, nil)
	if e != nil {
		t.Fatalf("MCP protocol error instead of CallToolResult: %v", e)
	}
	b, _ = json.Marshal(value)
	var result struct {
		IsError *bool                         `json:"isError"`
		Content []struct{ Type, Text string } `json:"content"`
	}
	if json.Unmarshal(b, &result) != nil || result.IsError == nil || len(result.Content) != 1 || result.Content[0].Type != "text" {
		t.Fatalf("bad result: %s", b)
	}
	return *result.IsError, result.Content[0].Text
}
func TestMCPDiscoveryManifestParityAndNoEffects(t *testing.T) {
	p := newPlugin()
	value, e := call(p, "mcp/tools", map[string]any{"connectionId": "not-open"})
	if e != nil {
		t.Fatalf("side-effect-free discovery missing: %v", e)
	}
	b, _ := json.Marshal(value)
	var result struct {
		Tools []struct {
			Name        string
			Description string
			InputSchema map[string]any
			Annotations map[string]any
		}
	}
	if json.Unmarshal(b, &result) != nil || len(result.Tools) != 13 {
		t.Fatalf("bad discovery: %s", b)
	}
	manifestBytes, err := os.ReadFile("../manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Version       string
		Contributions []struct {
			Type     string
			External bool `json:"external_tools"`
			AI       bool `json:"ai_tools"`
			Toolsets []map[string]any
			Tools    map[string]struct{ Access, Toolset string }
		}
	}
	if json.Unmarshal(manifestBytes, &manifest) != nil {
		t.Fatal("manifest invalid")
	}
	found := 0
	for _, contribution := range manifest.Contributions {
		if contribution.Type != "mcp" {
			continue
		}
		found++
		if !contribution.External || !contribution.AI || len(contribution.Tools) != 13 || len(contribution.Toolsets) < 2 {
			t.Fatalf("bad declaration: %+v", contribution)
		}
		seen := map[string]bool{}
		for _, tool := range result.Tools {
			access, ok := expectedMCP[tool.Name]
			if !ok || seen[tool.Name] {
				t.Fatalf("unknown/duplicate: %s", tool.Name)
			}
			seen[tool.Name] = true
			declaration, ok := contribution.Tools[tool.Name]
			if !ok || declaration.Access != access || declaration.Toolset == "" {
				t.Fatalf("access mismatch: %s", tool.Name)
			}
			if tool.Annotations["readOnlyHint"] != (access == "read") || tool.Annotations["destructiveHint"] != (access == "full") {
				t.Fatalf("unsafe hints: %+v", tool)
			}
			if tool.Description == "" || tool.InputSchema["type"] != "object" {
				t.Fatalf("missing schema: %+v", tool)
			}
			props, ok := tool.InputSchema["properties"].(map[string]any)
			if !ok || props["connectionId"] == nil {
				t.Fatal("missing bound identity")
			}
			for key, schema := range props {
				if schema.(map[string]any)["type"] == nil {
					t.Fatalf("untyped %s", key)
				}
			}
		}
	}
	if found != 1 || manifest.Version != "0.2.2" || version != "0.2.2" {
		t.Fatal("manifest/initialize version or contribution mismatch")
	}
	if len(p.sessions) != 0 {
		t.Fatal("discovery allocated UI sessions")
	}
}

type mcpFixture struct {
	mu          sync.Mutex
	paths       []string
	status      int
	echo        bool
	response    string
	rotate      string
	rejectLogin bool
}

func (f *mcpFixture) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.paths...)
}
func mcpFixtureServer(t *testing.T) (*httptest.Server, *mcpFixture, map[string]any) {
	t.Helper()
	f := &mcpFixture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.Method+" "+r.URL.Path)
		status, echo, response, rotate, rejectLogin := f.status, f.echo, f.response, f.rotate, f.rejectLogin
		f.mu.Unlock()
		r.ParseForm()
		if r.URL.Path == "/login" || r.URL.Path == "/auth/doLogin" {
			if rejectLogin {
				fmt.Fprint(w, `{"code":500}`)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "XXL_JOB_LOGIN_IDENTITY", Value: "fixture-cookie-secret", Path: "/"})
			fmt.Fprint(w, `{"code":200}`)
			return
		}
		if r.URL.Path == "/" {
			fmt.Fprint(w, `<a href="/jobgroup">Groups</a>`)
			return
		}
		if r.URL.Path == "/jobgroup" {
			fmt.Fprint(w, `<table id="joblog_list"><tr><td><button class="update" id="1" appname="demo-executor" title="Demo" order="1" addresstype="0" addresslist="" tasklimit="100"></button></td></tr></table>`)
			return
		}
		if rotate != "" {
			http.SetCookie(w, &http.Cookie{Name: "XXL_JOB_LOGIN_IDENTITY", Value: rotate, Path: "/"})
		}
		if response != "" {
			fmt.Fprint(w, response)
			return
		}
		if status != 0 {
			w.WriteHeader(status)
			fmt.Fprint(w, `{"code":500,"msg":"fixture failure"}`)
			return
		}
		if echo {
			fmt.Fprint(w, `{"code":500,"msg":"password=fixture-password token=fixture-token Cookie: fixture-cookie-secret"}`)
			return
		}
		switch r.URL.Path {
		case "/jobgroup/pageList":
			fmt.Fprint(w, `{"recordsTotal":1,"recordsFiltered":1,"data":[{"id":1,"appname":"demo-executor","title":"Demo","addressType":0,"addressList":""}]}`)
		case "/joblog/pageList":
			fmt.Fprint(w, `{"recordsTotal":1,"recordsFiltered":1,"data":[{"id":9,"jobGroup":1,"jobId":7,"executorAddress":"http://node:9999/","triggerTime":1720000000000}]}`)
		case "/jobinfo/pageList", "/operationlog/pageList":
			fmt.Fprint(w, `{"recordsTotal":1,"recordsFiltered":1,"data":[{"id":7,"jobGroup":1,"jobDesc":"Demo","glueSource":"private-source"}]}`)
		case "/joblog/getJobsByGroup":
			fmt.Fprint(w, `{"code":200,"content":[{"id":7,"jobGroup":1,"jobDesc":"Demo"}]}`)
		case "/joblog/logDetailCat":
			fmt.Fprint(w, `{"code":200,"content":{"end":true,"logContent":"safe log","fromLineNum":1,"toLineNum":2}}`)
		default:
			fmt.Fprint(w, `{"code":200,"content":null}`)
		}
	}))
	t.Cleanup(srv.Close)
	c := map[string]any{"id": "bound", "name": "fixture", "external_config": map[string]any{"base_url": srv.URL}}
	c["username"] = "fixture-user"
	c["password"] = "fixture-password"
	return srv, f, map[string]any{"connection": c, "runtime": map[string]any{}}
}
func TestMCPAllExistingEndpointsDispatchThroughSavedLifecycle(t *testing.T) {
	_, f, lifecycle := mcpFixtureServer(t)
	p := newPlugin()
	t.Cleanup(func() { closeMCPCache(p) })
	cases := []struct {
		tool, path string
		args       map[string]any
	}{
		{"xxljob_info", "", map[string]any{}},
		{"xxljob_groups", "POST /jobgroup/pageList", map[string]any{}},
		{"xxljob_jobs", "POST /jobinfo/pageList", map[string]any{"jobGroup": 1}},
		{"xxljob_logs", "POST /joblog/pageList", map[string]any{"jobGroup": 1}},
		{"xxljob_log_content", "POST /joblog/logDetailCat", map[string]any{"executorAddress": "http://node:9999/", "triggerTime": 1720000000000, "logId": 9, "fromLineNum": 1}},
		{"xxljob_jobs_by_group", "POST /joblog/getJobsByGroup", map[string]any{"jobGroup": 1}},
		{"xxljob_start", "POST /jobinfo/start", map[string]any{"id": 7, "confirmed": true}},
		{"xxljob_stop", "POST /jobinfo/stop", map[string]any{"id": 7, "confirmed": true}},
		{"xxljob_trigger", "POST /jobinfo/trigger", map[string]any{"id": 7, "confirmed": true}},
		{"xxljob_remove_job", "POST /jobinfo/remove", map[string]any{"id": 7, "confirmed": true}},
		{"xxljob_remove_group", "POST /jobgroup/remove", map[string]any{"id": 1, "confirmed": true}},
		{"xxljob_save_group", "POST /jobgroup/save", map[string]any{"appname": "demo-executor", "title": "Demo", "addressType": 0, "confirmed": true}},
		{"xxljob_save_group", "POST /jobgroup/update", map[string]any{"id": 1, "appname": "demo-executor", "title": "Demo", "addressType": 0, "confirmed": true}},
		{"xxljob_save_job", "POST /jobinfo/add", map[string]any{"jobGroup": 1, "jobDesc": "Demo", "author": "demo", "executorRouteStrategy": "FIRST", "executorHandler": "demoHandler", "executorBlockStrategy": "SERIAL_EXECUTION", "executorTimeout": 0, "executorFailRetryCount": 0, "glueType": "BEAN", "confirmed": true, "scheduleType": "CRON", "scheduleConf": "0 0/5 * * * ?", "misfireStrategy": "DO_NOTHING"}},
		{"xxljob_save_job", "POST /jobinfo/update", map[string]any{"id": 7, "jobGroup": 1, "jobDesc": "Demo", "author": "demo", "executorRouteStrategy": "FIRST", "executorHandler": "demoHandler", "executorBlockStrategy": "SERIAL_EXECUTION", "executorTimeout": 0, "executorFailRetryCount": 0, "glueType": "BEAN", "confirmed": true, "scheduleType": "CRON", "scheduleConf": "0 0/5 * * * ?", "misfireStrategy": "DO_NOTHING"}},
	}
	for _, tc := range cases {
		t.Run(tc.tool+tc.path, func(t *testing.T) {
			before := len(f.snapshot())
			failed, text := mcpInvoke(t, p, tc.tool, tc.args, lifecycle)
			if failed {
				t.Fatalf("call failed: %s", text)
			}
			paths := f.snapshot()[before:]
			if tc.path != "" && !containsMCPPath(paths, tc.path) {
				t.Fatalf("wrong endpoint: %v want %s", paths, tc.path)
			}
			if len(p.sessions) != 0 {
				t.Fatal("MCP overwrote UI sessions")
			}
			if tc.tool == "xxljob_jobs" && strings.Contains(text, "private-source") {
				t.Fatal("source leaked")
			}
		})
	}
}
func containsMCPPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
func TestMCPRejectsMalformedIdentityAndPolicyBeforeNetwork(t *testing.T) {
	_, f, life := mcpFixtureServer(t)
	p := newPlugin()
	tests := []struct {
		tool string
		args map[string]any
		life any
	}{
		{"xxljob_jobs", map[string]any{}, nil},
		{"xxljob_raw", map[string]any{}, life},
		{"xxljob_jobs", map[string]any{"connectionId": "other"}, life},
		{"xxljob_jobs", map[string]any{"connectionId": 7}, life},
		{"xxljob_jobs", map[string]any{"path": "/jobinfo/remove"}, life},
		{"xxljob_jobs", map[string]any{"form": map[string]any{"id": 7}}, life},
		{"xxljob_jobs", map[string]any{"password": "injected"}, life},
		{"xxljob_jobs", map[string]any{"read_only": false}, life},
		{"xxljob_jobs", map[string]any{"length": 101}, life},
		{"xxljob_jobs", map[string]any{"length": 1.5}, life},
		{"xxljob_jobs", map[string]any{"length": "25"}, life},
		{"xxljob_start", map[string]any{"id": 7}, life},
		{"xxljob_start", map[string]any{"id": 7, "confirmed": "true"}, life},
		{"xxljob_start", map[string]any{"id": 0, "confirmed": true}, life},
	}
	for _, tc := range tests {
		if failed, _ := mcpInvoke(t, p, tc.tool, tc.args, tc.life); !failed {
			t.Fatalf("unsafe call accepted %+v", tc)
		}
	}
	if paths := f.snapshot(); len(paths) != 0 {
		t.Fatalf("invalid calls caused authentication/network %v", paths)
	}
}
func TestMCPReadonlyLifecycleCannotInheritCachedWritePermissions(t *testing.T) {
	_, f, life := mcpFixtureServer(t)
	p := newPlugin()
	t.Cleanup(func() { closeMCPCache(p) })
	if bad, text := mcpInvoke(t, p, "xxljob_jobs", map[string]any{}, life); bad {
		t.Fatal(text)
	}
	before := f.snapshot()
	c := life["connection"].(map[string]any)
	for _, key := range []string{"read_only", "config"} {
		if key == "read_only" {
			c["read_only"] = true
		} else {
			delete(c, "read_only")
			c["external_config"].(map[string]any)["read_only"] = true
		}
		for tool, access := range expectedMCP {
			if access != "read" {
				if bad, _ := mcpInvoke(t, p, tool, map[string]any{"id": 7, "confirmed": true}, life); !bad {
					t.Fatalf("readonly accepted %s", tool)
				}
			}
		}
	}
	if !reflect.DeepEqual(before, f.snapshot()) {
		t.Fatal("readonly writes reached network")
	}
}
func TestMCPErrorEnvelopeRedactsSecretsAndNeverRetriesWrites(t *testing.T) {
	_, f, life := mcpFixtureServer(t)
	p := newPlugin()
	t.Cleanup(func() { closeMCPCache(p) })
	f.mu.Lock()
	f.echo = true
	f.mu.Unlock()
	failed, text := mcpInvoke(t, p, "xxljob_start", map[string]any{"id": 7, "confirmed": true}, life)
	if !failed || strings.Contains(text, "fixture-password") || strings.Contains(text, "fixture-token") || strings.Contains(text, "fixture-cookie-secret") {
		t.Fatalf("unsafe error: %s", text)
	}
	f.mu.Lock()
	f.echo = false
	f.status = 503
	f.mu.Unlock()
	before := len(f.snapshot())
	failed, text = mcpInvoke(t, p, "xxljob_start", map[string]any{"id": 7, "confirmed": true}, life)
	if !failed || !strings.Contains(text, "禁止自动重试") {
		t.Fatalf("unknown outcome lost: %s", text)
	}
	writes := 0
	for _, path := range f.snapshot()[before:] {
		if path == "POST /jobinfo/start" {
			writes++
		}
	}
	if writes != 1 {
		t.Fatalf("write retried %d", writes)
	}
}
func TestMCPMalformedCallReturnsErrorResult(t *testing.T) {
	for _, raw := range []string{`{`, `null`, `[]`, `{"tool":"xxljob_jobs","arguments":[]}`, `{"tool":"xxljob_jobs","arguments":null}`} {
		value, e := newPlugin().Handle(sdk.RequestContext{}, "mcp/call", json.RawMessage(raw), nil)
		b, _ := json.Marshal(value)
		if e != nil || !strings.Contains(string(b), `"isError":true`) {
			t.Fatalf("bad malformed envelope %s %s %v", raw, b, e)
		}
	}
}

func TestMCPCacheDoesNotRetainLifecyclePassword(t *testing.T) {
	_, _, life := mcpFixtureServer(t)
	p := newPlugin()
	if failed, text := mcpInvoke(t, p, "xxljob_jobs", map[string]any{}, life); failed {
		t.Fatal(text)
	}
	p.mcpMu.Lock()
	defer p.mcpMu.Unlock()
	entry := p.mcpSessions["bound"]
	defer entry.session.close()
	if entry.session.password != "" {
		t.Fatal("session kept login password")
	}
	for _, secret := range entry.secrets {
		if secret == "fixture-password" {
			t.Fatal("cache retained lifecycle password after authentication")
		}
	}
}

func closeMCPCache(p *plugin) {
	p.mcpMu.Lock()
	defer p.mcpMu.Unlock()
	for _, entry := range p.mcpSessions {
		entry.session.close()
	}
}
func TestMCPSuccessResultsRedactSecretsAndBoundOutput(t *testing.T) {
	_, f, life := mcpFixtureServer(t)
	p := newPlugin()
	defer closeMCPCache(p)
	if bad, text := mcpInvoke(t, p, "xxljob_logs", map[string]any{}, life); bad {
		t.Fatal(text)
	}
	f.mu.Lock()
	f.response = `{"code":200,"content":{"password":"opaque-password","access_key":"opaque-access-key","logContent":"Bearer opaque-bearer"}}`
	f.mu.Unlock()
	bad, text := mcpInvoke(t, p, "xxljob_log_content", map[string]any{"executorAddress": "http://node:9999/", "triggerTime": 1720000000000, "logId": 9, "fromLineNum": 1}, life)
	if bad || strings.Contains(text, "opaque-password") || strings.Contains(text, "opaque-access-key") || strings.Contains(text, "opaque-bearer") {
		t.Fatalf("unsafe result %s", text)
	}
	f.mu.Lock()
	f.response = `{"code":200,"content":{"logContent":"` + strings.Repeat("x", 1024*1024+10) + `"}}`
	f.mu.Unlock()
	bad, text = mcpInvoke(t, p, "xxljob_log_content", map[string]any{"executorAddress": "http://node:9999/", "triggerTime": 1720000000000, "logId": 9, "fromLineNum": 1}, life)
	if !bad || !strings.Contains(text, "exceeds 1 MiB") {
		t.Fatalf("oversized result accepted: %.100s", text)
	}
}
func TestMCPPreservesWorkbenchSessionAndFingerprintsLifecycle(t *testing.T) {
	_, f, life := mcpFixtureServer(t)
	p := newPlugin()
	defer closeMCPCache(p)
	b, _ := json.Marshal(life)
	var v params
	json.Unmarshal(b, &v)
	ui, e := newSession(v.Connection, v.Runtime)
	if e != nil {
		t.Fatal(e)
	}
	defer ui.close()
	p.sessions["bound"] = ui
	if bad, text := mcpInvoke(t, p, "xxljob_jobs", map[string]any{}, life); bad {
		t.Fatal(text)
	}
	first := p.mcpSessions["bound"].session
	life["connection"].(map[string]any)["external_config"].(map[string]any)["environment"] = "changed"
	if bad, text := mcpInvoke(t, p, "xxljob_info", map[string]any{}, life); bad || !strings.Contains(text, "changed") {
		t.Fatal(text)
	}
	if p.mcpSessions["bound"].session == first || p.sessions["bound"] != ui {
		t.Fatal("lifecycle changed without isolated cache replacement")
	}
	paths := f.snapshot()
	if len(paths) == 0 {
		t.Fatal("saved lifecycle never contacted its endpoint")
	}
}
func TestMCPRuntimeEndpointAndCacheInvalidation(t *testing.T) {
	srv1, f1, life := mcpFixtureServer(t)
	srv2, f2, _ := mcpFixtureServer(t)
	p := newPlugin()
	defer closeMCPCache(p)
	c := life["connection"].(map[string]any)
	c["host"] = "service.invalid"
	c["port"] = 8080
	c["transport_layers"] = []any{map[string]any{"type": "proxy"}}
	c["external_config"].(map[string]any)["base_url"] = "http://service.invalid:8080"
	for _, address := range []string{srv1.URL, srv2.URL} {
		u, _ := url.Parse(address)
		port, _ := strconv.Atoi(u.Port())
		life["runtime"] = map[string]any{"host": u.Hostname(), "port": port}
		if bad, text := mcpInvoke(t, p, "xxljob_jobs", map[string]any{}, life); bad {
			t.Fatal(text)
		}
	}
	if len(f1.snapshot()) == 0 || len(f2.snapshot()) == 0 {
		t.Fatal("cached session ignored transformed runtime endpoint")
	}
	before := len(f2.snapshot())
	life["runtime"] = map[string]any{}
	if bad, _ := mcpInvoke(t, p, "xxljob_jobs", map[string]any{}, life); !bad {
		t.Fatal("missing tunnel endpoint bypassed")
	}
	if len(f2.snapshot()) != before {
		t.Fatal("invalid runtime reused cached endpoint")
	}
}
func TestMCPCacheIsBoundedAndConcurrentCallsAreIsolated(t *testing.T) {
	_, _, life := mcpFixtureServer(t)
	p := newPlugin()
	defer closeMCPCache(p)
	for i := 0; i < 34; i++ {
		life["connection"].(map[string]any)["id"] = fmt.Sprintf("bound-%d", i)
		if bad, text := mcpInvoke(t, p, "xxljob_jobs", map[string]any{}, life); bad {
			t.Fatal(text)
		}
	}
	if len(p.mcpSessions) != 32 {
		t.Fatalf("unbounded cache %d", len(p.mcpSessions))
	}
	if p.mcpSessions["bound-0"] != nil {
		t.Fatal("oldest cache entry not evicted")
	}
	raw, _ := json.Marshal(map[string]any{"tool": "xxljob_jobs", "arguments": map[string]any{}, "lifecycle": life})
	var wg sync.WaitGroup
	failures := make(chan string, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, e := p.Handle(sdk.RequestContext{}, "mcp/call", raw, nil)
			b, _ := json.Marshal(value)
			if e != nil || strings.Contains(string(b), `"isError":true`) {
				failures <- fmt.Sprint(string(b), e)
			}
		}()
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
}

func TestMCPNormalUserCannotEscalateExecutorOrJobPermissions(t *testing.T) {
	writes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			http.SetCookie(w, &http.Cookie{Name: "XXL_JOB_LOGIN_IDENTITY", Value: "operator-test-cookie", Path: "/"})
			fmt.Fprint(w, `{"code":200}`)
		case "/":
			fmt.Fprint(w, `<a href="/jobinfo">Jobs</a>`)
		case "/jobinfo":
			fmt.Fprint(w, `<select id="jobGroup"><option value="1">Allowed</option></select>`)
		case "/joblog/getJobsByGroup":
			fmt.Fprint(w, `{"code":200,"content":[{"id":7,"jobGroup":1,"jobDesc":"Allowed"}]}`)
		default:
			writes++
			fmt.Fprint(w, `{"code":200,"content":null}`)
		}
	}))
	defer srv.Close()
	p := newPlugin()
	defer closeMCPCache(p)
	life := map[string]any{"connection": map[string]any{"id": "operator", "username": "fixture-user", "password": "fixture-password", "external_config": map[string]any{"base_url": srv.URL}}}
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{
		{"xxljob_jobs", map[string]any{"jobGroup": 2}},
		{"xxljob_trigger", map[string]any{"id": 8, "confirmed": true}},
		{"xxljob_save_group", map[string]any{"appname": "demo-executor", "title": "Demo", "addressType": 0, "confirmed": true}},
	} {
		if bad, _ := mcpInvoke(t, p, tc.tool, tc.args, life); !bad {
			t.Fatalf("permission escalation %s", tc.tool)
		}
	}
	if writes != 0 {
		t.Fatalf("forbidden endpoint reached %d", writes)
	}
}
func TestMCPLogContentCannotSpoofQueriedIdentity(t *testing.T) {
	_, f, life := mcpFixtureServer(t)
	p := newPlugin()
	defer closeMCPCache(p)
	if bad, text := mcpInvoke(t, p, "xxljob_logs", map[string]any{}, life); bad {
		t.Fatal(text)
	}
	before := f.snapshot()
	for _, args := range []map[string]any{
		{"executorAddress": "http://attacker.invalid/", "triggerTime": 1720000000000, "logId": 9, "fromLineNum": 1},
		{"executorAddress": "http://node:9999/", "triggerTime": 1720000000001, "logId": 9, "fromLineNum": 1},
		{"executorAddress": "http://node:9999/", "triggerTime": 1720000000000, "logId": 10, "fromLineNum": 1},
	} {
		if bad, _ := mcpInvoke(t, p, "xxljob_log_content", args, life); !bad {
			t.Fatal("spoofed log identity accepted")
		}
	}
	for _, path := range f.snapshot()[len(before):] {
		if path == "POST /joblog/logDetailCat" {
			t.Fatal("spoofed log endpoint reached")
		}
	}
}

func TestMCP34UsesVersionedEndpointsAndPortableForms(t *testing.T) {
	paths := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		r.ParseForm()
		switch r.URL.Path {
		case "/auth/doLogin":
			http.SetCookie(w, &http.Cookie{Name: "xxl-session", Value: "version34-test-cookie", Path: "/"})
			fmt.Fprint(w, `{"code":200}`)
		case "/":
			fmt.Fprint(w, `<a href="/jobgroup">Groups</a>`)
		case "/jobgroup/pageList":
			fmt.Fprint(w, `{"code":200,"data":{"total":1,"data":[{"id":1,"appname":"demo-executor","title":"Demo","addressType":0,"addressList":""}]}}`)
		case "/jobinfo/pageList":
			if r.Form.Has("start") || r.Form.Has("length") || !r.Form.Has("offset") || !r.Form.Has("pagesize") {
				t.Errorf("bad 3.4 pagination: %v", r.Form)
			}
			fmt.Fprint(w, `{"code":200,"data":{"total":1,"data":[{"id":7,"jobGroup":1,"jobDesc":"Demo"}]}}`)
		case "/joblog/pageList":
			fmt.Fprint(w, `{"code":200,"data":{"total":1,"data":[{"id":9,"jobGroup":1}]}}`)
		case "/joblog/logDetailCat":
			if r.Form.Has("executorAddress") || r.Form.Has("triggerTime") {
				t.Error("legacy log identity leaked onto 3.4 wire")
			}
			fmt.Fprint(w, `{"code":200,"data":{"end":true,"logContent":"safe"}}`)
		case "/jobinfo/start", "/jobinfo/stop", "/jobinfo/delete", "/jobgroup/delete":
			if r.Form.Has("id") || r.Form.Get("ids[]") == "" {
				t.Errorf("3.4 singular ID: %v", r.Form)
			}
			fmt.Fprint(w, `{"code":200,"data":null}`)
		default:
			fmt.Fprint(w, `{"code":200,"data":null}`)
		}
	}))
	defer srv.Close()
	p := newPlugin()
	defer closeMCPCache(p)
	life := map[string]any{"connection": map[string]any{"id": "version34", "username": "fixture-user", "password": "fixture-password", "external_config": map[string]any{"base_url": srv.URL, "admin_version": "3.4"}}}
	cases := []struct {
		tool, path string
		args       map[string]any
	}{
		{"xxljob_info", "GET /", map[string]any{}},
		{"xxljob_groups", "POST /jobgroup/pageList", map[string]any{}},
		{"xxljob_jobs", "POST /jobinfo/pageList", map[string]any{}},
		{"xxljob_jobs_by_group", "POST /jobinfo/pageList", map[string]any{"jobGroup": 1}},
		{"xxljob_logs", "POST /joblog/pageList", map[string]any{}},
		{"xxljob_log_content", "POST /joblog/logDetailCat", map[string]any{"logId": 9, "fromLineNum": 1}},
		{"xxljob_start", "POST /jobinfo/start", map[string]any{"id": 7, "confirmed": true}},
		{"xxljob_stop", "POST /jobinfo/stop", map[string]any{"id": 7, "confirmed": true}},
		{"xxljob_trigger", "POST /jobinfo/trigger", map[string]any{"id": 7, "confirmed": true}},
		{"xxljob_remove_job", "POST /jobinfo/delete", map[string]any{"id": 7, "confirmed": true}},
		{"xxljob_remove_group", "POST /jobgroup/delete", map[string]any{"id": 1, "confirmed": true}},
		{"xxljob_save_group", "POST /jobgroup/insert", map[string]any{"appname": "demo-executor", "title": "Demo", "addressType": 0, "confirmed": true}},
		{"xxljob_save_group", "POST /jobgroup/update", map[string]any{"id": 1, "appname": "demo-executor", "title": "Demo", "addressType": 0, "confirmed": true}},
	}
	job := map[string]any{"jobGroup": 1, "jobDesc": "Demo", "author": "demo", "executorRouteStrategy": "FIRST", "executorHandler": "demoHandler", "executorBlockStrategy": "SERIAL_EXECUTION", "executorTimeout": 0, "executorFailRetryCount": 0, "glueType": "BEAN", "confirmed": true, "scheduleType": "CRON", "scheduleConf": "0 0/5 * * * ?", "misfireStrategy": "DO_NOTHING"}
	cases = append(cases, struct {
		tool, path string
		args       map[string]any
	}{"xxljob_save_job", "POST /jobinfo/insert", job})
	updated := map[string]any{}
	for k, v := range job {
		updated[k] = v
	}
	updated["id"] = 7
	cases = append(cases, struct {
		tool, path string
		args       map[string]any
	}{"xxljob_save_job", "POST /jobinfo/update", updated})
	for _, tc := range cases {
		before := len(paths)
		if bad, text := mcpInvoke(t, p, tc.tool, tc.args, life); bad {
			t.Fatalf("%s failed: %s", tc.tool, text)
		}
		if !containsMCPPath(paths[before:], tc.path) {
			t.Fatalf("%s: %v", tc.tool, paths[before:])
		}
	}
}

func TestMCPHostDisconnectInvalidatesOnlyMatchingCache(t *testing.T) {
	_, _, life := mcpFixtureServer(t)
	p := newPlugin()
	defer closeMCPCache(p)
	if bad, text := mcpInvoke(t, p, "xxljob_jobs", map[string]any{}, life); bad {
		t.Fatal(text)
	}
	life["connection"].(map[string]any)["id"] = "other"
	if bad, text := mcpInvoke(t, p, "xxljob_jobs", map[string]any{}, life); bad {
		t.Fatal(text)
	}
	other := p.mcpSessions["other"].session
	if _, e := call(p, "connection/disconnect", map[string]any{"connection": map[string]any{"id": "bound"}}); e != nil {
		t.Fatal(e)
	}
	if p.mcpSessions["bound"] != nil {
		t.Fatal("host disconnect retained MCP session")
	}
	if p.mcpSessions["other"] == nil || p.mcpSessions["other"].session != other {
		t.Fatal("host disconnect disturbed unrelated MCP session")
	}
}

func TestMCPSchemasDescribeActualGroupRanges(t *testing.T) {
	value, e := call(newPlugin(), "mcp/tools", map[string]any{})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(value)
	var result struct {
		Tools []struct {
			Name        string
			InputSchema struct {
				Properties map[string]struct{ Minimum float64 }
			}
		}
	}
	json.Unmarshal(b, &result)
	want := map[string]float64{"xxljob_jobs": -1, "xxljob_logs": 0, "xxljob_save_job": 1, "xxljob_jobs_by_group": 1}
	for _, tool := range result.Tools {
		if minimum, ok := want[tool.Name]; ok && tool.InputSchema.Properties["jobGroup"].Minimum != minimum {
			t.Fatalf("%s group range schema differs from validation", tool.Name)
		}
	}
}

func TestMCPRedactsRotatedSessionCookieFromResult(t *testing.T) {
	_, f, life := mcpFixtureServer(t)
	p := newPlugin()
	defer closeMCPCache(p)
	if bad, text := mcpInvoke(t, p, "xxljob_logs", map[string]any{}, life); bad {
		t.Fatal(text)
	}
	f.mu.Lock()
	f.rotate = "rotated-cookie-secret"
	f.response = `{"code":200,"content":{"value":"rotated-cookie-secret"}}`
	f.mu.Unlock()
	bad, text := mcpInvoke(t, p, "xxljob_log_content", map[string]any{"executorAddress": "http://node:9999/", "triggerTime": 1720000000000, "logId": 9, "fromLineNum": 1}, life)
	if bad || strings.Contains(text, "rotated-cookie-secret") {
		t.Fatalf("rotated session cookie leaked: %s", text)
	}
}

func TestMCPCredentialChangeNeverReusesAuthenticatedSession(t *testing.T) {
	_, f, life := mcpFixtureServer(t)
	p := newPlugin()
	defer closeMCPCache(p)
	if bad, text := mcpInvoke(t, p, "xxljob_jobs", map[string]any{}, life); bad {
		t.Fatal(text)
	}
	before := len(f.snapshot())
	f.mu.Lock()
	f.rejectLogin = true
	f.mu.Unlock()
	life["connection"].(map[string]any)["password"] = "changed-fixture-password"
	if bad, _ := mcpInvoke(t, p, "xxljob_jobs", map[string]any{}, life); !bad {
		t.Fatal("new credentials inherited cached authentication")
	}
	if !reflect.DeepEqual(f.snapshot()[before:], []string{"POST /login"}) {
		t.Fatalf("credential change reused service session: %v", f.snapshot()[before:])
	}
	if p.mcpSessions["bound"] != nil {
		t.Fatal("old authenticated cache survived failed credential refresh")
	}
}
func TestMCPExpiredSessionIsNotAutomaticallyReauthenticatedOrReplayed(t *testing.T) {
	_, f, life := mcpFixtureServer(t)
	p := newPlugin()
	defer closeMCPCache(p)
	if bad, text := mcpInvoke(t, p, "xxljob_jobs", map[string]any{}, life); bad {
		t.Fatal(text)
	}
	f.mu.Lock()
	f.status = 401
	f.mu.Unlock()
	if bad, _ := mcpInvoke(t, p, "xxljob_jobs", map[string]any{}, life); !bad {
		t.Fatal("expired read accepted")
	}
	before := f.snapshot()
	if bad, _ := mcpInvoke(t, p, "xxljob_start", map[string]any{"id": 7, "confirmed": true}, life); !bad {
		t.Fatal("expired session accepted write")
	}
	if !reflect.DeepEqual(before, f.snapshot()) {
		t.Fatal("expired session reauthenticated or replayed a request")
	}
}

func TestMCPRedactsJSONStringsAndURLCredentialsAtServiceBoundary(t *testing.T) {
	_, f, life := mcpFixtureServer(t)
	p := newPlugin()
	defer closeMCPCache(p)
	if bad, output := mcpInvoke(t, p, "xxljob_logs", map[string]any{}, life); bad {
		t.Fatal(output)
	}
	payload := `{"password":"json-private-password","nested":{"token":"json-private-token"}}`
	logline := `prefix {"api_key":"log-private-api-key"} https://alice:url-private-password@service/path?token=url-private-token&job=node`
	for _, failed := range []bool{false, true} {
		response := map[string]any{"code": 200, "content": map[string]any{"executorParam": payload, "logContent": logline}}
		if failed {
			response = map[string]any{"code": 500, "msg": logline + " " + payload}
		}
		raw, _ := json.Marshal(response)
		f.mu.Lock()
		f.response = string(raw)
		f.mu.Unlock()
		bad, output := mcpInvoke(t, p, "xxljob_log_content", map[string]any{"executorAddress": "http://node:9999/", "triggerTime": 1720000000000, "logId": 9, "fromLineNum": 1}, life)
		if bad != failed {
			t.Fatalf("wrong failure state: %v %s", bad, output)
		}
		for _, secret := range []string{"json-private-password", "json-private-token", "log-private-api-key", "url-private-password", "url-private-token"} {
			if strings.Contains(output, secret) {
				t.Fatalf("secret leaked from tool response: %s", output)
			}
		}
	}
}
