package main

import (
	"fmt"
	"net/url"
)

// XXL-JOB 3.0.x changed the pageList filter parameters from optional to
// @RequestParam(required = true). Omitting them makes Spring return HTTP 400.
// 2.3.x treats the same parameters as optional, so sending empty values keeps
// both official wire contracts valid.
func fillLegacyPageFilters(path string, form url.Values) {
	switch path {
	case "/jobgroup/pageList":
		for _, key := range []string{"appname", "title"} {
			if _, ok := form[key]; !ok {
				form.Set(key, "")
			}
		}
	case "/jobinfo/pageList":
		for _, key := range []string{"jobDesc", "executorHandler", "author"} {
			if _, ok := form[key]; !ok {
				form.Set(key, "")
			}
		}
	case "/joblog/pageList":
		if _, ok := form["filterTime"]; !ok {
			form.Set("filterTime", "")
		}
	}
}

// The UI consumes one pagination shape; wire formats remain version-specific.
func normalizeLegacyPage(page map[string]any, write bool) (any, error) {
	total, ok1 := page["recordsTotal"].(float64)
	filtered, ok2 := page["recordsFiltered"].(float64)
	_, ok3 := page["data"].([]any)
	if ok1 && ok2 && ok3 && validCount(total) && validCount(filtered) {
		return page, nil
	}
	return nil, uncertain(write, "旧版分页响应字段缺失")
}

func normalizePage(page map[string]any, write bool) (any, error) {
	total, ok1 := page["total"].(float64)
	rows, ok2 := page["data"].([]any)
	if !ok1 || !ok2 || !validCount(total) {
		return nil, uncertain(write, "新版分页响应字段缺失")
	}
	return map[string]any{"recordsTotal": total, "recordsFiltered": total, "data": rows}, nil
}

func validCount(value float64) bool {
	return value >= 0 && value <= 9007199254740991 && value == float64(int64(value))
}

func (s *session) adaptRequest(method, path string, form url.Values) (string, url.Values, error) {
	if s.adminVersion == "2.3" || s.adminVersion == "3.0" {
		fillLegacyPageFilters(path, form)
		if s.adminVersion == "3.0" && method == "xxljob/logContent" {
			// 3.0.x resolves the executor from the log row; these 2.3-only fields are unused.
			form.Del("executorAddress")
			form.Del("triggerTime")
		}
		return path, form, nil
	}
	adapted := url.Values{}
	for key, values := range form {
		adapted[key] = append([]string(nil), values...)
	}
	switch method {
	case "xxljob/jobs", "xxljob/logs":
		adapted.Set("offset", form.Get("start"))
		adapted.Set("pagesize", form.Get("length"))
		adapted.Del("start")
		adapted.Del("length")
		if method == "xxljob/jobs" {
			for _, key := range []string{"jobDesc", "executorHandler", "author"} {
				if _, ok := adapted[key]; !ok {
					adapted.Set(key, "")
				}
			}
		} else if _, ok := adapted["filterTime"]; !ok {
			adapted.Set("filterTime", "")
		}
	case "xxljob/saveJob":
		if form.Get("id") == "" {
			path = "/jobinfo/insert"
		}
	case "xxljob/saveGroup":
		if form.Get("id") == "" {
			path = "/jobgroup/insert"
		}
	case "xxljob/start", "xxljob/stop", "xxljob/removeJob", "xxljob/removeGroup":
		if form.Get("id") == "" {
			return "", nil, fmt.Errorf("%s 缺少任务或执行器 ID", method)
		}
		adapted.Set("ids[]", form.Get("id"))
		adapted.Del("id")
		switch method {
		case "xxljob/removeJob":
			path = "/jobinfo/delete"
		case "xxljob/removeGroup":
			path = "/jobgroup/delete"
		}
	case "xxljob/logContent":
		adapted.Del("executorAddress")
		adapted.Del("triggerTime")
	}
	return path, adapted, nil
}
