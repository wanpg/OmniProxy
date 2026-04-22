package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// 全局 HTTP 客户端（连接复用）
var httpClient = &http.Client{
	Timeout: 120 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	},
}

// 全局配置引用（后续通过参数注入）
var globalConfig *Config

func setConfig(cfg *Config) {
	globalConfig = cfg
}

// authMiddleware 认证中间件
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := extractAPIKey(r)
		if key == "" {
			http.Error(w, `{"error": "Missing API key"}`, http.StatusUnauthorized)
			return
		}

		// Admin Key 直接放行
		if globalConfig.IsAdminKey(key) {
			next.ServeHTTP(w, r)
			return
		}

		// 查找 Key 配置
		cfg := globalConfig.FindKeyConfig(key)
		if cfg == nil {
			http.Error(w, `{"error": "Invalid API key"}`, http.StatusUnauthorized)
			return
		}

		// 将配置注入到请求 Context（通过 Header 传递）
		r.Header.Set("X-Key-Provider", cfg.Provider)
		r.Header.Set("X-Key-Alias", cfg.Alias)
		r.Header.Set("X-Key-Hash", HashKey(key))
		next.ServeHTTP(w, r)
	}
}

// extractAPIKey 从请求中提取 API Key
func extractAPIKey(r *http.Request) string {
	// Bearer Token
	if auth := r.Header.Get("Authorization"); auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
		return auth
	}

	// x-api-key (Anthropic 风格)
	if key := r.Header.Get("x-api-key"); key != "" {
		return key
	}

	return ""
}

type modelOnlyRequest struct {
	Model string `json:"model"`
}

// handleChatCompletions 处理 OpenAI 风格的 /v1/chat/completions
func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	alias := r.Header.Get("X-Key-Alias")
	hash := r.Header.Get("X-Key-Hash")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error": "Failed to read request body"}`, http.StatusBadRequest)
		return
	}

	var req modelOnlyRequest
	if err := json.Unmarshal(body, &req); err != nil || req.Model == "" {
		http.Error(w, `{"error": "Invalid request body or missing model"}`, http.StatusBadRequest)
		return
	}

	provider := globalConfig.FindProviderByModel(req.Model)
	if provider == nil {
		http.Error(w, `{"error": "Model not found or not configured: `+req.Model+`"}`, http.StatusBadRequest)
		return
	}

	proxyRequest(w, r, provider.Name, alias, hash, req.Model, body)
}

// handleAnthropicMessages 处理 Anthropic 风格的 /v1/messages
func handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	alias := r.Header.Get("X-Key-Alias")
	hash := r.Header.Get("X-Key-Hash")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error": "Failed to read request body"}`, http.StatusBadRequest)
		return
	}

	var req modelOnlyRequest
	if err := json.Unmarshal(body, &req); err != nil || req.Model == "" {
		http.Error(w, `{"error": "Invalid request body or missing model"}`, http.StatusBadRequest)
		return
	}

	provider := globalConfig.FindProviderByModel(req.Model)
	if provider == nil {
		http.Error(w, `{"error": "Model not found or not configured: `+req.Model+`"}`, http.StatusBadRequest)
		return
	}

	proxyRequest(w, r, provider.Name, alias, hash, req.Model, body)
}

// proxyRequest 根据 provider 配置进行请求转发（流式）
func proxyRequest(w http.ResponseWriter, r *http.Request, providerName string, alias string, hash string, model string, body []byte) {
	provider := globalConfig.GetProvider(providerName)
	if provider == nil {
		http.Error(w, `{"error": "Provider not found"}`, http.StatusBadRequest)
		return
	}

	// Codex provider uses separate handler (different API format)
	if provider.AuthType == "codex" {
		handleCodexProxy(w, r, provider, alias, hash, model, body)
		return
	}

	start := time.Now()

	// 根据 auth_type 选择端点和 header
	var endpoint string
	var headers map[string]string

	if provider.AuthType == "anthropic" {
		endpoint = provider.APIBase + "/messages"
		headers = map[string]string{
			"x-api-key":         provider.APIKey,
			"anthropic-version": "2023-06-01",
			"content-type":       r.Header.Get("content-type"),
		}
	} else {
		endpoint = provider.APIBase + "/chat/completions"
		headers = map[string]string{
			"authorization": "Bearer " + provider.APIKey,
			"content-type":  r.Header.Get("content-type"),
		}
	}

	bodyReader := bytes.NewReader(body)
	req, err := http.NewRequest(r.Method, endpoint, bodyReader)
	if err != nil {
		http.Error(w, `{"error": "Failed to build upstream request"}`, http.StatusInternalServerError)
		return
	}
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}

	for k, v := range r.Header {
		lowerK := strings.ToLower(k)
		if lowerK == "host" || lowerK == "authorization" || lowerK == "x-api-key" || lowerK == "content-length" {
			continue
		}
		req.Header[k] = v
	}
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		http.Error(w, "Provider error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, v := range resp.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(resp.StatusCode)

	var respBuf bytes.Buffer
	if _, err := io.Copy(io.MultiWriter(w, &respBuf), resp.Body); err != nil {
		return
	}
	latencyMs := int(time.Since(start).Milliseconds())

	respBody := respBuf.Bytes()
	var promptTokens, completionTokens, totalTokens int
	if provider.AuthType == "anthropic" {
		promptTokens, completionTokens, totalTokens = extractAnthropicTokens(respBody)
	} else {
		promptTokens, completionTokens, totalTokens = extractTokens(respBody)
	}

	go func() {
		log := &RequestLog{
			KeyHash:          hash,
			KeyAlias:         alias,
			Provider:         providerName,
			Model:            model,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			LatencyMs:        latencyMs,
			StatusCode:       resp.StatusCode,
			CreatedAt:        Now(),
		}
		LogRequest(log)
	}()
}

// extractModelFromBody 从请求 body 中提取 model
func extractModelFromBody(data []byte) string {
	var reqData map[string]interface{}
	if err := json.Unmarshal(data, &reqData); err != nil {
		return ""
	}
	if m, ok := reqData["model"].(string); ok {
		return m
	}
	return ""
}

// extractTokens 从 OpenAI 响应提取 Token 使用量（支持流式 SSE）
func extractTokens(data []byte) (prompt, completion, total int) {
	// 先尝试直接解析（非流式）
	var resp map[string]interface{}
	if err := json.Unmarshal(data, &resp); err == nil {
		if usage, ok := resp["usage"].(map[string]interface{}); ok {
			if v, ok := usage["prompt_tokens"].(float64); ok {
				prompt = int(v)
			}
			if v, ok := usage["completion_tokens"].(float64); ok {
				completion = int(v)
			}
			if v, ok := usage["total_tokens"].(float64); ok {
				total = int(v)
			}
		}
		if total > 0 || prompt > 0 || completion > 0 {
			return prompt, completion, total
		}
	}

	// 流式 SSE：从最后一个有效 data 行提取 usage
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		jsonStr := strings.TrimPrefix(line, "data: ")
		if jsonStr == "[DONE]" {
			continue
		}
		var chunk map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStr), &chunk); err != nil {
			continue
		}
		if usage, ok := chunk["usage"].(map[string]interface{}); ok {
			if v, ok := usage["prompt_tokens"].(float64); ok {
				prompt = int(v)
			}
			if v, ok := usage["completion_tokens"].(float64); ok {
				completion = int(v)
			}
			if v, ok := usage["total_tokens"].(float64); ok {
				total = int(v)
			}
			if total > 0 || prompt > 0 || completion > 0 {
				return prompt, completion, total
			}
		}
	}

	return 0, 0, 0
}

// extractAnthropicTokens 从 Anthropic 响应提取 Token
func extractAnthropicTokens(data []byte) (prompt, completion, total int) {
	var resp map[string]interface{}
	if err := json.Unmarshal(data, &resp); err != nil {
		return 0, 0, 0
	}
	if usage, ok := resp["usage"].(map[string]interface{}); ok {
		if v, ok := usage["input_tokens"].(float64); ok {
			prompt = int(v)
		}
		if v, ok := usage["output_tokens"].(float64); ok {
			completion = int(v)
		}
	}
	return prompt, completion, prompt + completion
}

// handleAdminStats 处理 Admin 统计请求
func handleAdminStats(w http.ResponseWriter, r *http.Request) {
	authKey := extractAPIKey(r)
	legacyQueryKey := r.URL.Query().Get("key")

	// 兼容旧用法: /admin/stats?key=<admin_key>
	if authKey == "" && legacyQueryKey == globalConfig.AdminKey {
		authKey = legacyQueryKey
		legacyQueryKey = ""
	}

	if !globalConfig.IsAdminKey(authKey) {
		http.Error(w, `{"error":"Admin key required"}`, http.StatusUnauthorized)
		return
	}

	keyAlias := r.URL.Query().Get("key_alias")
	if keyAlias == "" {
		keyAlias = r.URL.Query().Get("alias")
	}
	if keyAlias == "" {
		keyAlias = legacyQueryKey
	}

	provider := r.URL.Query().Get("provider")
	days := 7
	if v := r.URL.Query().Get("days"); v != "" {
		if n := atoiSafe(v); n > 0 {
			days = n
		}
	}

	stats, err := GetStats(keyAlias, provider, days)
	if err != nil {
		http.Error(w, "Query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleAdminUI 管理页面
func handleAdminUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(adminUIHTML))
}

const adminUIHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Gateway Proxy Stats</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background:#0f1117;color:#e4e4e7;min-height:100vh}
.container{max-width:960px;margin:0 auto;padding:24px 20px}
h1{font-size:1.5rem;font-weight:600;margin-bottom:20px;color:#fff}
h1 span{color:#7c8aff}
.toolbar{display:flex;gap:12px;align-items:center;flex-wrap:wrap;margin-bottom:24px}
.toolbar input,.toolbar select{background:#1a1b26;border:1px solid #2a2b3d;color:#e4e4e7;padding:8px 12px;border-radius:6px;font-size:14px;outline:none}
.toolbar input:focus,.toolbar select:focus{border-color:#7c8aff}
.toolbar button{background:#7c8aff;color:#fff;border:none;padding:8px 18px;border-radius:6px;cursor:pointer;font-size:14px;font-weight:500}
.toolbar button:hover{background:#6a7ae0}
.summary{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:14px;margin-bottom:28px}
.card{background:#1a1b26;border:1px solid #2a2b3d;border-radius:10px;padding:18px}
.card .label{font-size:13px;color:#888;margin-bottom:6px}
.card .value{font-size:1.6rem;font-weight:700;color:#fff}
.card .value.blue{color:#7c8aff}
.card .value.green{color:#34d399}
.card .value.amber{color:#fbbf24}
.card .value.rose{color:#fb7185}
h2{font-size:1.1rem;font-weight:600;margin-bottom:14px;color:#ccc}
table{width:100%;border-collapse:collapse;background:#1a1b26;border-radius:10px;overflow:hidden;border:1px solid #2a2b3d}
th,td{padding:12px 16px;text-align:left;font-size:14px}
th{background:#22243a;color:#999;font-weight:500;border-bottom:1px solid #2a2b3d}
tr:not(:last-child) td{border-bottom:1px solid #1e1f30}
tr:hover td{background:#1e1f30}
.empty{text-align:center;color:#555;padding:40px 0}
.section{margin-bottom:28px}
.badge{display:inline-block;padding:2px 8px;border-radius:4px;font-size:12px;font-weight:500}
.badge.zhipu{background:#1a3a2a;color:#34d399}
.badge.minimax{background:#3a2a1a;color:#fbbf24}
.badge.openai{background:#1a2a3a;color:#60a5fa}
.err{color:#fb7185;font-size:14px;margin-bottom:12px}
</style>
</head>
<body>
<div class="container">
<h1>🤖 Gateway <span>Stats</span></h1>
<div class="toolbar">
  <input id="keyInput" type="password" placeholder="Admin Key">
  <select id="daysSelect">
    <option value="7">近 7 天</option>
    <option value="30">近 30 天</option>
    <option value="90">近 90 天</option>
    <option value="365">近 1 年</option>
  </select>
  <button onclick="loadStats()">刷新</button>
</div>
<div id="error" class="err" style="display:none"></div>
<div id="content" style="display:none">
  <div class="summary" id="summary"></div>
  <div class="section">
    <h2>🔑 按 Key 统计</h2>
    <table><thead><tr><th>Key</th><th>Provider</th><th>请求数</th><th>Prompt</th><th>Completion</th><th>Total Tokens</th></tr></thead><tbody id="keyBody"></tbody></table>
  </div>
  <div class="section">
    <h2>📊 按模型统计</h2>
    <table><thead><tr><th>模型</th><th>Provider</th><th>请求数</th><th>Total Tokens</th></tr></thead><tbody id="modelBody"></tbody></table>
  </div>
</div>
<div id="placeholder" class="empty">输入 Admin Key 后点击刷新</div>
</div>
<script>
var savedKey=localStorage.getItem('gp_admin_key')||'';
document.getElementById('keyInput').value=savedKey;
if(savedKey)loadStats();
function loadStats(){
  var key=document.getElementById('keyInput').value.trim();
  var days=document.getElementById('daysSelect').value;
  if(!key){showErr('请输入 Admin Key');return;}
  localStorage.setItem('gp_admin_key',key);
  fetch('/admin/stats?days='+days,{headers:{'Authorization':'Bearer '+key}})
    .then(function(r){if(!r.ok)throw new Error(r.status===401?'Admin Key 错误':'请求失败('+r.status+')');return r.json()})
    .then(function(d){render(d)})
    .catch(function(e){showErr(e.message)});
}
function showErr(m){document.getElementById('error').textContent=m;document.getElementById('error').style.display='block';}
function render(d){
  document.getElementById('error').style.display='none';
  document.getElementById('content').style.display='block';
  document.getElementById('placeholder').style.display='none';
  var s=d.summary||{};
  document.getElementById('summary').innerHTML=
    '<div class="card"><div class="label">总请求数</div><div class="value blue">'+fmt(s.total_requests)+'</div></div>'+
    '<div class="card"><div class="label">Prompt Tokens</div><div class="value green">'+fmt(s.total_prompt_tokens)+'</div></div>'+
    '<div class="card"><div class="label">Completion Tokens</div><div class="value amber">'+fmt(s.total_completion_tokens)+'</div></div>'+
    '<div class="card"><div class="label">Total Tokens</div><div class="value rose">'+fmt(s.total_tokens)+'</div></div>';
  var kb=document.getElementById('keyBody');
  if(d.by_key&&d.by_key.length){kb.innerHTML=d.by_key.map(function(k){return '<tr><td>'+esc(k.key_alias)+'</td><td><span class="badge '+(k.provider||'')+'">'+esc(k.provider)+'</span></td><td>'+fmt(k.requests)+'</td><td>'+fmt(k.prompt_tokens)+'</td><td>'+fmt(k.completion_tokens)+'</td><td>'+fmt(k.total_tokens)+'</td></tr>'}).join('')}else{kb.innerHTML='<tr><td colspan="6" class="empty">暂无数据</td></tr>'}
  var mb=document.getElementById('modelBody');
  if(d.by_model&&d.by_model.length){mb.innerHTML=d.by_model.map(function(m){return '<tr><td>'+esc(m.model)+'</td><td><span class="badge '+(m.provider||'')+'">'+esc(m.provider)+'</span></td><td>'+fmt(m.requests)+'</td><td>'+fmt(m.total_tokens)+'</td></tr>'}).join('')}else{mb.innerHTML='<tr><td colspan="4" class="empty">暂无数据</td></tr>'}
}
function fmt(n){return(n||0).toLocaleString()}
function esc(s){var d=document.createElement('div');d.textContent=s||'';return d.innerHTML}
</script>
</body>
</html>`

// handleHealth 健康检查
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// handleAuthStatus Codex auth status (Phase 2 OAuth 准备)
func handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := map[string]interface{}{
		"status":        "not_configured",
		"oauth_enabled":  false,
		"accounts":      0,
		"message":       "Phase 1: OAuth not yet implemented. Use manual access_token in config.yaml.",
	}
	if codexPool != nil {
		summary := codexPool.Summary()
		status["accounts"] = summary["total"]
		status["active"] = summary["active"]
		if summary["active"] > 0 {
			status["status"] = "active"
		}
	}
	json.NewEncoder(w).Encode(status)
}

// handleModels 返回支持的模型列表
func handleModels(w http.ResponseWriter, r *http.Request) {
	models := []map[string]interface{}{}
	for name, provider := range globalConfig.Providers {
		// 跳过无效 Key（placeholder）— codex provider 不需要 api_key
		if provider.AuthType != "codex" && strings.HasPrefix(provider.APIKey, "your-") {
			continue
		}
		for _, model := range provider.Models {
			models = append(models, map[string]interface{}{
				"id":       model,
				"object":   "model",
				"provider": name,
				"owned_by": "omniproxy",
			})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   models,
	})
}
