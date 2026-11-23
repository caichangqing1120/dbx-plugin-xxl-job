package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func manifestConnection(t *testing.T, serverURL string) connection {
	t.Helper()
	data, err := os.ReadFile("../manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Contributions []struct {
			Type   string `json:"type"`
			Fields []struct {
				Key     string `json:"key"`
				Default any    `json:"default"`
			} `json:"fields"`
		} `json:"contributions"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	defaults := map[string]any{}
	for _, contribution := range manifest.Contributions {
		if contribution.Type != "connection-provider" {
			continue
		}
		for _, field := range contribution.Fields {
			if field.Default != nil {
				defaults[field.Key] = field.Default
			}
		}
	}
	if defaults["admin_version"] == nil || defaults["context_path"] == nil || defaults["scheme"] == nil {
		t.Fatal("manifest connection defaults are missing")
	}
	host, port, err := net.SplitHostPort(strings.TrimPrefix(serverURL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	var number int
	if _, err := fmt.Sscan(port, &number); err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"admin_version": defaults["admin_version"], "context_path": defaults["context_path"], "scheme": defaults["scheme"]}
	encoded, err := json.Marshal(map[string]any{"id": "fixture", "host": host, "port": number, "username": "demo", "password": "test-only", "external_config": config})
	if err != nil {
		t.Fatal(err)
	}
	var c connection
	if err := json.Unmarshal(encoded, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestManifestDefaultsConnectToOfficialModernRoot(t *testing.T) {
	var requests []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case "/auth/doLogin":
			if r.Method != http.MethodPost {
				t.Error("login must be POST")
			}
			http.SetCookie(w, &http.Cookie{Name: "XXL_SSO_TOKEN", Value: "fixture", Path: "/"})
			fmt.Fprint(w, `{"code":200}`)
		case "/":
			fmt.Fprint(w, `<a href="/jobgroup">Groups</a>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := manifestConnection(t, srv.URL)
	if c.Config.AdminVersion != "3.4" {
		t.Fatalf("unexpected manifest version %q", c.Config.AdminVersion)
	}
	if _, err := newPlugin().handle("connection/test", params{Connection: c}); err != nil {
		t.Fatalf("manifest defaults failed connection/test: %v (requests %v)", err, requests)
	}
	if len(requests) != 2 || requests[0] != "/auth/doLogin" || requests[1] != "/" {
		t.Fatalf("unexpected routes %v", requests)
	}
}

func TestVersionPathsAndCustomPath(t *testing.T) {
	for _, tc := range []struct{ name, version, path, expected string }{
		{"legacy fallback", "", "", "/xxl-job-admin/login"},
		{"2.3 default", "2.3", "", "/xxl-job-admin/login"},
		{"3.0 default", "3.0", "", "/xxl-job-admin/login"},
		{"3.4 default", "3.4", "", "/auth/doLogin"},
		{"2.3 configured", "2.3", "/xxl-job-admin", "/xxl-job-admin/login"},
		{"3.0 configured", "3.0", "/xxl-job-admin", "/xxl-job-admin/login"},
		{"3.4 custom", "3.4", "/custom", "/custom/auth/doLogin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				permissionPath := strings.TrimSuffix(tc.expected, "/login")
				permissionPath = strings.TrimSuffix(permissionPath, "/auth/doLogin")
				if permissionPath == "" {
					permissionPath = "/"
				} else {
					permissionPath += "/"
				}
				if r.URL.Path == permissionPath {
					fmt.Fprintf(w, `<a href="%sjobgroup">Groups</a>`, permissionPath)
					return
				}
				if r.URL.Path != tc.expected {
					t.Errorf("request path %q, want %q", r.URL.Path, tc.expected)
					http.NotFound(w, r)
					return
				}
				cookie := "XXL_SSO_TOKEN"
				if tc.version != "3.4" {
					cookie = "XXL_JOB_LOGIN_IDENTITY"
				}
				http.SetCookie(w, &http.Cookie{Name: cookie, Value: "fixture", Path: "/"})
				fmt.Fprint(w, `{"code":200}`)
			}))
			defer srv.Close()
			c := manifestConnection(t, srv.URL)
			c.Config.AdminVersion, c.Config.ContextPath = tc.version, tc.path
			s, err := newSession(c, runtimeEndpoint{})
			if err != nil {
				t.Fatal(err)
			}
			defer s.close()
			if err := s.probe(); err != nil {
				t.Fatal(err)
			}
			if calls != 2 {
				t.Fatalf("want login and permission page, got %d calls", calls)
			}
		})
	}
}
