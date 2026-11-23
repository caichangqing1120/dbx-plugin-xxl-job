package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

type config struct {
	BaseURL      string `json:"base_url"`
	Scheme       string `json:"scheme"`
	ContextPath  string `json:"context_path"`
	AdminVersion string `json:"admin_version"`
	Environment  string `json:"environment"`
	ReadOnly     bool   `json:"read_only"`
}
type connection struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Host            string           `json:"host"`
	Port            int              `json:"port"`
	Username        string           `json:"username"`
	Password        string           `json:"password"`
	Config          config           `json:"external_config"`
	ReadOnly        bool             `json:"read_only"`
	TransportLayers []transportLayer `json:"transport_layers"`
}
type transportLayer struct {
	Type    string `json:"type"`
	Enabled *bool  `json:"enabled"`
}
type runtimeEndpoint struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}
type session struct {
	mu                        sync.Mutex
	base                      string
	client                    *http.Client
	name, environment         string
	username, password        string
	readOnly                  bool
	adminVersion              string
	admin                     bool
	loggedIn, expired, closed bool
	allowed                   []group
	logRows                   map[int64]map[string]any
}

func newSession(c connection, runtime runtimeEndpoint) (*session, error) {
	adminVersion := c.Config.AdminVersion
	if adminVersion == "" {
		adminVersion = "2.3" // Previously saved connections predate the version selector.
	}
	if adminVersion != "2.3" && adminVersion != "3.0" && adminVersion != "3.4" {
		return nil, errors.New("仅支持已核验的官方 XXL-JOB Admin 2.3.x、3.0.x 和 3.4.x，请在连接配置中选择版本")
	}
	address := strings.TrimSpace(c.Config.BaseURL)
	if address == "" {
		scheme := c.Config.Scheme
		if scheme == "" {
			scheme = "http"
		}
		path := c.Config.ContextPath
		if path == "" {
			path = "/"
			if adminVersion == "2.3" || adminVersion == "3.0" {
				path = "/xxl-job-admin"
			}
		}
		if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.ContainsAny(path, "?#%\\ \r\n\t") {
			return nil, errors.New("应用路径必须以 / 开头，不得包含编码、查询参数或片段")
		}
		address = scheme + "://localhost" + path
	}
	u, e := url.Parse(address)
	if e != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(address, "\\\r\n\t ") {
		return nil, errors.New("服务地址必须是完整 HTTP/HTTPS URL，不能包含凭据、查询参数或片段")
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == ".." || part == "." {
			return nil, errors.New("服务地址包含非法路径")
		}
	}
	if strings.Contains(u.RawPath, "%") {
		return nil, errors.New("服务地址不支持编码路径")
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return nil, errors.New("服务端口无效")
		}
	}
	if c.Host != "" || c.Port != 0 {
		host := strings.TrimSpace(c.Host)
		if host == "" || strings.ContainsAny(host, "/@?#\\% \t\r\n[]") || (strings.Contains(host, ":") && net.ParseIP(host) == nil) {
			return nil, errors.New("请填写有效 IP 或域名，不要包含协议、端口或路径")
		}
		if c.Port < 1 || c.Port > 65535 {
			return nil, errors.New("端口必须在 1 到 65535 之间")
		}
		u.Host = net.JoinHostPort(host, strconv.Itoa(c.Port))
	} else if strings.TrimSpace(c.Config.BaseURL) == "" {
		return nil, errors.New("请填写 IP 或域名及端口")
	}
	activeTransport := false
	for _, layer := range c.TransportLayers {
		if layer.Enabled == nil || *layer.Enabled {
			if layer.Type != "ssh" && layer.Type != "proxy" && layer.Type != "http_tunnel" {
				return nil, errors.New("未知 DBX 传输层类型")
			}
			activeTransport = true
		}
	}
	u.Path = strings.TrimRight(u.Path, "/")
	transport := &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConnsPerHost: 4,
	}
	if activeTransport {
		servicePort := 80
		if u.Scheme == "https" {
			servicePort = 443
		}
		if u.Port() != "" {
			servicePort, _ = strconv.Atoi(u.Port())
		}
		if !strings.EqualFold(strings.TrimSpace(c.Host), u.Hostname()) || c.Port != servicePort {
			return nil, errors.New("已启用 DBX 隧道/代理；请填写 IP 或域名及端口作为服务目标")
		}
		if runtime.Host == "" || strings.ContainsAny(runtime.Host, "/@?#\\ \t\r\n") || runtime.Port < 1 || runtime.Port > 65535 {
			return nil, errors.New("DBX 未提供有效隧道端点；不会绕过代理直连，请检查隧道配置")
		}
		target := net.JoinHostPort(runtime.Host, strconv.Itoa(runtime.Port))
		// Route TCP through the host tunnel without rewriting HTTP Host or TLS SNI.
		transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext(ctx, network, target)
		}
	}
	jar, e := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if e != nil {
		return nil, e
	}
	return &session{base: u.String(), name: c.Name, environment: c.Config.Environment, username: c.Username, password: c.Password, readOnly: c.ReadOnly || c.Config.ReadOnly, adminVersion: adminVersion, logRows: map[int64]map[string]any{},
		client: &http.Client{Jar: jar, Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (s *session) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.password = ""
	s.client.Jar = nil
	s.allowed = nil
	s.logRows = nil
	s.client.CloseIdleConnections()
}
func (s *session) probe() error {
	defer func() { s.password = "" }()
	if strings.TrimSpace(s.username) == "" || strings.TrimSpace(s.password) == "" {
		return errors.New("请填写 XXL-JOB 用户名和密码")
	}
	loginPath := "/login"
	if s.adminVersion == "3.4" {
		loginPath = "/auth/doLogin"
	}
	body, e := s.raw("POST", loginPath, url.Values{"userName": {s.username}, "password": {s.password}}, false)
	if e != nil {
		return errors.New("登录失败，请检查服务地址、协议和网络；不会跟随重定向")
	}
	var reply struct {
		Code int `json:"code"`
	}
	if json.Unmarshal(body, &reply) != nil || reply.Code != 200 {
		return errors.New("登录失败，请检查用户名和密码")
	}
	u, _ := url.Parse(s.base + "/")
	found := false
	for _, cookie := range s.client.Jar.Cookies(u) {
		if cookie.Value != "" && (s.adminVersion == "3.4" || cookie.Name == "XXL_JOB_LOGIN_IDENTITY") {
			found = true
		}
	}
	if !found {
		return errors.New("登录未返回可用的会话 Cookie，请检查应用路径和协议")
	}
	s.loggedIn = true
	return s.refreshAccess()
}
func (s *session) active() error {
	if s.closed {
		return errors.New("连接已断开，请从 DBX 重新连接")
	}
	if s.expired {
		return errors.New("登录已失效，请从 DBX 重新连接；不会自动重登或重试操作")
	}
	return nil
}
func (s *session) expire(write bool) error {
	s.expired = true
	s.client.Jar = nil
	s.allowed = nil
	s.logRows = nil
	if write {
		return uncertain(true, "登录已失效，请从 DBX 重新连接")
	}
	return s.active()
}
func uncertain(write bool, reason string) error {
	if write {
		return fmt.Errorf("写入结果未知，请刷新核对，禁止自动重试（%s）", reason)
	}
	return fmt.Errorf("服务响应无效或不可达（%s）", reason)
}
func (s *session) raw(method, path string, form url.Values, write bool) ([]byte, error) {
	if e := s.active(); e != nil {
		return nil, e
	}
	req, e := http.NewRequest(method, s.base+path, strings.NewReader(form.Encode()))
	if e != nil {
		return nil, errors.New("服务请求地址无效")
	}
	req.Header.Set("Accept", "application/json, text/html;q=0.9")
	if method == "POST" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, e := s.client.Do(req)
	if e != nil {
		return nil, uncertain(write, "网络错误或超时")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return nil, s.expire(write)
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, s.expire(write)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, uncertain(write, fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
	body, e := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024+1))
	if e != nil || len(body) > 4*1024*1024 {
		return nil, uncertain(write, "响应过大或读取失败")
	}
	if loginHTML(body) {
		return nil, s.expire(write)
	}
	return body, nil
}
func (s *session) request(path string, form url.Values, write bool) (any, error) {
	raw, e := s.raw("POST", path, form, write)
	if e != nil {
		return nil, e
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result == nil {
		if strings.HasPrefix(strings.TrimSpace(string(raw)), "<") {
			if write {
				return nil, uncertain(true, "接口返回 HTML 而非 JSON，可能为登录页")
			}
			return nil, errors.New("接口返回 HTML 而非 JSON，请检查应用路径和原版部署")
		}
		return nil, uncertain(write, "不是 JSON 对象")
	}
	if code, ok := result["code"].(float64); ok {
		if code != 200 {
			msg, _ := result["msg"].(string)
			if msg == "" {
				msg = "服务端拒绝请求"
			}
			return nil, fmt.Errorf("%s（业务码 %.0f）", msg, code)
		}
		if s.adminVersion == "3.4" {
			if strings.HasSuffix(path, "/pageList") {
				page, ok := result["data"].(map[string]any)
				if !ok {
					return nil, uncertain(write, "缺少分页数据")
				}
				return normalizePage(page, write)
			}
			if _, ok := result["data"]; !ok {
				return nil, uncertain(write, "缺少响应数据")
			}
			return result["data"], nil
		}
		if strings.HasSuffix(path, "/pageList") {
			return nil, uncertain(write, "缺少分页数据")
		}
		return result["content"], nil
	}
	if (s.adminVersion == "2.3" || s.adminVersion == "3.0") && strings.HasSuffix(path, "/pageList") {
		return normalizeLegacyPage(result, write)
	}
	return nil, uncertain(write, "响应不符合所选 XXL-JOB 版本接口契约")
}
