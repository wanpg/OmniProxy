package provider

import (
	"net/http"
	"strings"
	"time"
)

// HTTPClient 全局 HTTP 客户端
var HTTPClient = &http.Client{
	Timeout: 120 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	},
}

// ExtractAPIKey 从请求中提取 API Key
func ExtractAPIKey(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
		return auth
	}
	if key := r.Header.Get("x-api-key"); key != "" {
		return key
	}
	return ""
}

// WriteJSONError 写入 JSON 错误
func WriteJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write([]byte(`{"error":"` + msg + `"}`))
}
