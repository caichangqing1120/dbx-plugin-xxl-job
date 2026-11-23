package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestOfficial30WireContract mirrors the required @RequestParam contract of the
// official XXL-JOB 3.0.0 controllers. Every pageList endpoint rejects a request
// that omits the filter parameters 2.3.x treated as optional (HTTP 400), which
// is exactly the failure reported against a real 3.0.0 deployment.
func TestOfficial30WireContract(t *testing.T) {
	requests := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path]++
		if r.URL.Path == "/xxl-job-admin/login" {
			r.ParseForm()
			if r.Method != "POST" || r.Form.Get("userName") != "admin" || r.Form.Get("password") != "fixture-password" {
				t.Error("3.0 login contract mismatch")
			}
			http.SetCookie(w, &http.Cookie{Name: "XXL_JOB_LOGIN_IDENTITY", Value: "fixture-cookie", Path: "/"})
			fmt.Fprint(w, `{"code":200,"content":null}`)
			return
		}
		if c, e := r.Cookie("XXL_JOB_LOGIN_IDENTITY"); e != nil || c.Value != "fixture-cookie" {
			t.Errorf("missing legacy session cookie on %s", r.URL.Path)
		}
		if r.URL.Path == "/xxl-job-admin/" {
			fmt.Fprint(w, `<html><a href="/xxl-job-admin/jobgroup">Groups</a></html>`)
			return
		}
		r.ParseForm()
		require := func(keys ...string) bool {
			for _, key := range keys {
				if !r.Form.Has(key) {
					t.Errorf("%s missing required parameter %s: %v", r.URL.Path, key, r.Form)
					http.Error(w, "Missing required parameter: "+key, http.StatusBadRequest)
					return false
				}
			}
			return true
		}
		switch r.URL.Path {
		case "/xxl-job-admin/jobgroup/pageList":
			if !require("start", "length", "appname", "title") {
				return
			}
			fmt.Fprint(w, `{"recordsTotal":1,"recordsFiltered":1,"data":[{"id":2,"appname":"fixture-app","title":"Fixture","addressType":0,"addressList":"http://executor/"}]}`)
		case "/xxl-job-admin/jobinfo/pageList":
			if !require("start", "length", "jobGroup", "triggerStatus", "jobDesc", "executorHandler", "author") {
				return
			}
			fmt.Fprint(w, `{"recordsTotal":1,"recordsFiltered":1,"data":[{"id":7,"jobGroup":2,"jobDesc":"Fixture job","triggerStatus":1}]}`)
		case "/xxl-job-admin/joblog/pageList":
			if !require("start", "length", "jobGroup", "jobId", "logStatus", "filterTime") {
				return
			}
			fmt.Fprint(w, `{"recordsTotal":1,"recordsFiltered":1,"data":[{"id":11,"jobGroup":2,"jobId":7,"triggerTime":"2026-11-23 10:00:00","executorAddress":"http://executor/","triggerCode":200,"handleCode":200}]}`)
		case "/xxl-job-admin/joblog/getJobsByGroup":
			if !require("jobGroup") {
				return
			}
			fmt.Fprint(w, `{"code":200,"content":[{"id":7,"jobGroup":2,"jobDesc":"Fixture job"}]}`)
		case "/xxl-job-admin/joblog/logDetailCat":
			if !require("logId", "fromLineNum") {
				return
			}
			if r.Form.Has("executorAddress") || r.Form.Has("triggerTime") {
				t.Errorf("3.0 logDetailCat received 2.3-only fields: %v", r.Form)
			}
			fmt.Fprint(w, `{"code":200,"content":{"fromLineNum":1,"toLineNum":1,"logContent":"ok","end":true}}`)
		case "/xxl-job-admin/jobinfo/start", "/xxl-job-admin/jobinfo/stop":
			if !require("id") {
				return
			}
			if r.Form.Has("ids[]") {
				t.Errorf("3.0 mutation used modern ids[]: %v", r.Form)
			}
			fmt.Fprint(w, `{"code":200,"content":null}`)
		default:
			t.Errorf("unexpected 3.0 endpoint %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := newPlugin()
	c := officialConnection(t, srv.URL+"/xxl-job-admin", "admin", "fixture-password")
	c.Config.AdminVersion = "3.0"
	if _, err := p.handle("connection/connect", params{Connection: c}); err != nil {
		t.Fatalf("3.0 connect failed: %v", err)
	}
	defer p.handle("connection/disconnect", params{Connection: connection{ID: c.ID}})

	info, err := p.handle("xxljob/info", params{ConnectionID: c.ID})
	if err != nil || info.(map[string]any)["adminVersion"] != "3.0" {
		t.Fatalf("version not exposed: %v %v", info, err)
	}
	if _, err := p.handle("xxljob/groups", params{ConnectionID: c.ID}); err != nil {
		t.Fatalf("groups failed: %v", err)
	}
	if _, err := p.handle("xxljob/jobs", params{ConnectionID: c.ID}); err != nil {
		t.Fatalf("jobs failed: %v", err)
	}
	if _, err := p.handle("xxljob/logs", params{ConnectionID: c.ID}); err != nil {
		t.Fatalf("logs failed: %v", err)
	}
	if _, err := p.handle("xxljob/jobsByGroup", params{ConnectionID: c.ID, Form: map[string]any{"jobGroup": float64(2)}}); err != nil {
		t.Fatalf("jobsByGroup failed: %v", err)
	}
	// The log row is retained by the preceding logs query; 3.0 needs no legacy log identity.
	if _, err := p.handle("xxljob/logContent", params{ConnectionID: c.ID, Form: map[string]any{"logId": float64(11), "fromLineNum": float64(1)}}); err != nil {
		t.Fatalf("logContent failed: %v", err)
	}
	for _, method := range []string{"xxljob/start", "xxljob/stop"} {
		if _, err := p.handle(method, params{ConnectionID: c.ID, Confirmed: true, Form: map[string]any{"id": float64(7)}}); err != nil {
			t.Fatalf("%s failed: %v", method, err)
		}
	}
	for _, path := range []string{"/xxl-job-admin/jobgroup/pageList", "/xxl-job-admin/jobinfo/pageList", "/xxl-job-admin/joblog/pageList"} {
		if requests[path] == 0 {
			t.Errorf("no request to %s", path)
		}
	}
	if requests["/xxl-job-admin/joblog/logDetailCat"] != 1 {
		t.Errorf("unexpected logDetailCat count %d", requests["/xxl-job-admin/joblog/logDetailCat"])
	}
}
