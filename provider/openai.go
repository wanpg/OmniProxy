package provider

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"gateway-proxy/config"
	"gateway-proxy/db"
)

// HandleChatCompletions 处理 /v1/chat/completions 路由（含认证）
func HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	key := ExtractAPIKey(r)
	if key == "" {
		WriteJSONError(w, http.StatusUnauthorized, "Missing API key")
		return
	}

	cfg := config.Global
	if cfg == nil {
		WriteJSONError(w, http.StatusInternalServerError, "Config not loaded")
		return
	}

	// Admin Key 直接放行
	if cfg.IsAdminKey(key) {
		// Admin uses any model without key config
		proxyOpenAI(w, r, "", config.HashKey(key), key)
		return
	}

	kc := cfg.FindKeyConfig(key)
	if kc == nil {
		WriteJSONError(w, http.StatusUnauthorized, "Invalid API key")
		return
	}

	proxyOpenAI(w, r, kc.Alias, config.HashKey(key), key)
}

// proxyOpenAI 标准代理（OpenAI/Anthropic/MiniMax/Zhipu 格式）
func proxyOpenAI(w http.ResponseWriter, r *http.Request, alias, hash, apiKey string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		WriteJSONError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Model == "" {
		WriteJSONError(w, http.StatusBadRequest, "Invalid request body or missing model")
		return
	}

	cfg := config.Global
	prov := cfg.FindProviderByModel(req.Model)
	if prov == nil {
		WriteJSONError(w, http.StatusBadRequest, "Model not found or not configured: "+req.Model)
		return
	}

	// Codex 走专用 handler
	if prov.AuthType == "codex" {
		HandleCodexProxy(w, r, prov, alias, hash, req.Model, body)
		return
	}

	start := time.Now()

	var endpoint string
	var headers map[string]string

	if prov.AuthType == "anthropic" {
		endpoint = prov.APIBase + "/messages"
		headers = map[string]string{
			"x-api-key":         prov.APIKey,
			"anthropic-version": "2023-06-01",
			"content-type":      r.Header.Get("content-type"),
		}
	} else {
		endpoint = prov.APIBase + "/chat/completions"
		headers = map[string]string{
			"authorization": "Bearer " + prov.APIKey,
			"content-type":  r.Header.Get("content-type"),
		}
	}

	bodyReader := bytes.NewReader(body)
	httpReq, err := http.NewRequest(r.Method, endpoint, bodyReader)
	if err != nil {
		WriteJSONError(w, http.StatusInternalServerError, "Failed to build upstream request")
		return
	}
	httpReq.ContentLength = int64(len(body))
	httpReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}

	for k, v := range r.Header {
		lowerK := strings.ToLower(k)
		if lowerK == "host" || lowerK == "authorization" || lowerK == "x-api-key" || lowerK == "content-length" {
			continue
		}
		httpReq.Header[k] = v
	}
	for k, v := range headers {
		if v != "" {
			httpReq.Header.Set(k, v)
		}
	}

	resp, err := HTTPClient.Do(httpReq)
	if err != nil {
		WriteJSONError(w, http.StatusBadGateway, "Provider error: "+err.Error())
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
	if prov.AuthType == "anthropic" {
		promptTokens, completionTokens, totalTokens = extractAnthropicTokens(respBody)
	} else {
		promptTokens, completionTokens, totalTokens = extractTokens(respBody)
	}

	go func() {
		log := &db.RequestLog{
			KeyHash:          hash,
			KeyAlias:         alias,
			Provider:         prov.Name,
			Model:            req.Model,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			LatencyMs:        latencyMs,
			StatusCode:       resp.StatusCode,
			CreatedAt:        time.Now(),
		}
		db.Record(log)
	}()
}

func extractTokens(data []byte) (prompt, completion, total int) {
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
