package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gateway-proxy/config"
	"gateway-proxy/db"
)

const (
	codexDefaultBaseURL = "https://chatgpt.com/backend-api/codex"
	codexUserAgent      = "codex-tui/0.118.0 (Mac OS 26.3.1; arm64) iTerm.app/3.6.9 (codex-tui; 0.118.0)"
	codexOpenAIBeta     = "responses_websockets=2026-02-06"
)

// CodexPool is the global account pool, initialized from config
var CodexPool *AccountPool

// codexAuthFile is the path to Codex CLI auth file
var codexAuthFile string

// CodexAuth represents the ~/.codex/auth.json structure
type CodexAuthFile struct {
	AuthMode  string          `json:"auth_mode"`
	Tokens    CodexAuthTokens `json:"tokens"`
	LastRefresh string        `json:"last_refresh"`
}

// CodexAuthTokens holds the token data from auth.json
type CodexAuthTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	AccountID    string `json:"account_id"`
}

// initCodexPool initializes the Codex account pool from multiple sources:
// 1. config.yaml accounts array
// 2. config.yaml api_key
// 3. ~/.codex/auth.json (Codex CLI auth file)
func InitCodex() {
	provider := config.Global.GetProvider("codex")
	if provider == nil {
		log.Println("[Codex] No codex provider configured, skipping")
		return
	}

	// 1. Try config.yaml accounts array
	if len(provider.Accounts) > 0 {
		CodexPool = NewAccountPool(provider.Accounts)
		log.Printf("[Codex] Loaded %d accounts from config.yaml", len(provider.Accounts))
	}

	// 2. Try config.yaml api_key
	if CodexPool == nil && provider.APIKey != "" && !strings.HasPrefix(provider.APIKey, "your-") {
		CodexPool = NewAccountPool([]config.CodexAccount{
			{AccessToken: provider.APIKey},
		})
		log.Println("[Codex] Loaded 1 account from config.yaml api_key")
	}

	// 3. Try ~/.codex/auth.json (Codex CLI auth file)
	if CodexPool == nil {
		home, err := os.UserHomeDir()
		if err == nil {
			p := filepath.Join(home, ".codex", "auth.json")
			if acc, err := loadCodexAuthFile(p); err == nil {
				CodexPool = NewAccountPool([]config.CodexAccount{*acc})
				codexAuthFile = p
				log.Printf("[Codex] Loaded account from %s (account_id: %s)", p, acc.AccountID)
			}
	}
	}

	if CodexPool != nil {
		log.Printf("[Codex] Account pool initialized: %s", CodexPool.Summary())
	}
}

// loadCodexAuthFile reads and parses the Codex CLI auth.json file
func loadCodexAuthFile(path string) (*config.CodexAccount, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read auth.json: %w", err)
	}

	var auth CodexAuthFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return nil, fmt.Errorf("parse auth.json: %w", err)
	}

	if auth.Tokens.AccessToken == "" {
		return nil, fmt.Errorf("auth.json has no access_token")
	}

	return &config.CodexAccount{
		AccessToken:  auth.Tokens.AccessToken,
		RefreshToken: auth.Tokens.RefreshToken,
		AccountID:    auth.Tokens.AccountID,
		Label:        "codex-cli",
	}, nil
}

// refreshCodexToken refreshes the access_token via OpenAI OAuth endpoint
// Falls back to codex CLI if HTTP refresh fails
func refreshCodexToken() error {
	// Try HTTP refresh first
	if err := refreshCodexTokenHTTP(); err == nil {
		return nil
	} else {
		log.Printf("[Codex] HTTP refresh failed: %v, trying codex CLI", err)
	}

	// Fallback to codex CLI
	codex, err := exec.LookPath("codex")
	if err != nil {
		return fmt.Errorf("codex CLI not found in PATH")
	}

	cmd := exec.Command(codex, "auth", "refresh")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("codex auth refresh failed: %w", err)
	}

	// Reload from auth file
	if codexAuthFile != "" && CodexPool != nil {
		if acc, err := loadCodexAuthFile(codexAuthFile); err == nil {
			CodexPool.UpdateToken(0, acc.AccessToken)
			log.Println("[Codex] Token refreshed via codex CLI")
		}
	}

	return nil
}

// refreshCodexTokenHTTP refreshes the Codex access_token using the OpenAI OAuth endpoint
func refreshCodexTokenHTTP() error {
	if codexAuthFile == "" || CodexPool == nil {
		return fmt.Errorf("no auth file or pool initialized")
	}

	// Read current auth file to get refresh_token
	acc, err := loadCodexAuthFile(codexAuthFile)
	if err != nil {
		return fmt.Errorf("read auth file: %w", err)
	}
	if acc.RefreshToken == "" {
		return fmt.Errorf("no refresh_token available")
	}

	// Call OpenAI OAuth token endpoint
	const tokenEndpoint = "https://auth.openai.com/oauth/token"
	const clientID = "app_EMoamEEZ73f0CkXaXp7hrann"

	payload := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": acc.RefreshToken,
		"client_id":     clientID,
	}
	body, _ := json.Marshal(payload)

	resp, err := http.Post(tokenEndpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("OAuth returned %d: %s", resp.StatusCode, string(respBody[:min(len(respBody), 200)]))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if result.AccessToken == "" {
		return fmt.Errorf("empty access_token in response")
	}

	// Update memory pool
	CodexPool.UpdateToken(0, result.AccessToken)

	// Persist to auth.json
	if err := persistCodexAuth(result.AccessToken, result.RefreshToken, result.IDToken, acc.AccountID); err != nil {
		log.Printf("[Codex] WARNING: token refreshed but failed to persist: %v", err)
	}

	log.Printf("[Codex] Token refreshed via OAuth (expires in %ds)", result.ExpiresIn)
	return nil
}

// persistCodexAuth writes updated tokens back to auth.json
func persistCodexAuth(accessToken, refreshToken, idToken, accountID string) error {
	if codexAuthFile == "" {
		return fmt.Errorf("no auth file path")
	}

	// Read current file
	data, err := os.ReadFile(codexAuthFile)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	var auth CodexAuthFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	// Update tokens
	auth.Tokens.AccessToken = accessToken
	if refreshToken != "" {
		auth.Tokens.RefreshToken = refreshToken
	}
	if idToken != "" {
		auth.Tokens.IDToken = idToken
	}
	auth.LastRefresh = time.Now().UTC().Format(time.RFC3339Nano)

	// Write back
	newData, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	if err := os.WriteFile(codexAuthFile, newData, 0600); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	return nil
}

// getCodexBaseURL returns the Codex API base URL from provider config or default
func getCodexBaseURL(provider *config.Provider) string {
	if provider.APIBase != "" {
		return provider.APIBase
	}
	return codexDefaultBaseURL
}

// handleCodexProxy handles requests for the codex provider.
// It converts Chat Completions format to Codex Responses API format,
// forwards to chatgpt.com, and converts the response back.
func HandleCodexProxy(w http.ResponseWriter, r *http.Request, provider *config.Provider, alias string, hash string, model string, body []byte) {
	start := time.Now()

	// 1. Parse the Chat Completions request
	var chatReq ChatCompletionRequest
	if err := json.Unmarshal(body, &chatReq); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	// 3. Resolve virtual model (e.g. gpt-5.5-high → gpt-5.5 + reasoning_effort=high)
	realModel, mappedReasoning := provider.ResolveModel(model)
	chatReq.Model = realModel
	if mappedReasoning != "" && chatReq.ReasoningEffort == "" {
		chatReq.ReasoningEffort = mappedReasoning // model_map default, explicit request takes priority
	}

	// 4. Acquire an account
	var accessToken string
	var accountID string
	var accountEntryID string

	if CodexPool != nil {
		entry := CodexPool.Acquire()
		if entry == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "No available Codex accounts")
			return
		}
		accessToken = entry.AccessToken
		accountID = entry.AccountID
		accountEntryID = entry.ID
	} else if provider.APIKey != "" && !strings.HasPrefix(provider.APIKey, "your-") {
		accessToken = provider.APIKey
	} else {
		writeJSONError(w, http.StatusServiceUnavailable, "No Codex accounts configured")
		return
	}

	// 3. Convert Chat Completions → Codex Responses API
	codexReq, err := ChatToResponses(&chatReq)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Request conversion failed: "+err.Error())
		return
	}

	// For non-streaming requests, we still send stream:true to Codex
	// and collect the response ourselves
	isStream := chatReq.Stream
	codexReq.Stream = true

	reqBody, err := json.Marshal(codexReq)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to marshal Codex request")
		return
	}

	// 4. Build HTTP request to Codex
	baseURL := getCodexBaseURL(provider)
	endpoint := baseURL + "/responses"

	httpReq, err := http.NewRequest("POST", endpoint, bytes.NewReader(reqBody))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Failed to build upstream request")
		return
	}
	httpReq.ContentLength = int64(len(reqBody))

	// Set headers
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("OpenAI-Beta", codexOpenAIBeta)
	httpReq.Header.Set("User-Agent", codexUserAgent)
	httpReq.Header.Set("x-openai-internal-codex-residency", "us")

	if accountID != "" {
		httpReq.Header.Set("Chatgpt-Account-Id", accountID)
	}

	// 5. Send request
	resp, err := HTTPClient.Do(httpReq)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "Upstream request failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	// 6. Handle error responses
	if resp.StatusCode != http.StatusOK {
		handleCodexError(w, resp, accountEntryID)
		logCodexRequest(alias, hash, "codex", model, 0, 0, 0, start, resp.StatusCode)
		return
	}

	// 7. Process response
	if isStream {
		// Streaming: convert SSE events and flush to client
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		usage, streamErr := StreamResponsesToChat(resp.Body, w, model)

		if streamErr != nil {
			log.Printf("[Codex] Stream error: %v", streamErr)
		}

		// Log stats
		promptTokens, completionTokens, totalTokens := 0, 0, 0
		if usage != nil {
			promptTokens = usage.InputTokens
			completionTokens = usage.OutputTokens
			totalTokens = usage.TotalTokens
		}
		logCodexRequest(alias, hash, "codex", model, promptTokens, completionTokens, totalTokens, start, http.StatusOK)

		// Release account (not rate limited since we got 200)
		if accountEntryID != "" {
			CodexPool.Release(accountEntryID, false, 0)
		}
	} else {
		// Non-streaming: collect full response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		chatResp, usage, collectErr := CollectResponsesToChat(resp.Body, model)
		if collectErr != nil {
			log.Printf("[Codex] Collect error: %v", collectErr)
			// Try to write error to client
			writeJSONError(w, http.StatusBadGateway, "Response collection failed: "+collectErr.Error())
			return
		}

		json.NewEncoder(w).Encode(chatResp)

		// Log stats
		promptTokens, completionTokens, totalTokens := 0, 0, 0
		if usage != nil {
			promptTokens = usage.InputTokens
			completionTokens = usage.OutputTokens
			totalTokens = usage.TotalTokens
		}
		logCodexRequest(alias, hash, "codex", model, promptTokens, completionTokens, totalTokens, start, http.StatusOK)

		if accountEntryID != "" {
			CodexPool.Release(accountEntryID, false, 0)
		}
	}
}

// handleCodexError processes non-200 responses from the Codex API
func handleCodexError(w http.ResponseWriter, resp *http.Response, accountEntryID string) {
	// Read error body
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if len(bodyStr) > 1000 {
		bodyStr = bodyStr[:1000] + "..."
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		// Token expired — try refresh first
		if accountEntryID != "" && CodexPool != nil {
			log.Printf("[Codex] Account %s token expired, attempting refresh...", accountEntryID)
			if refreshErr := refreshCodexToken(); refreshErr == nil {
				log.Printf("[Codex] Token refresh succeeded, retry is not automatic — client should retry")
			} else {
				log.Printf("[Codex] Token refresh failed: %v", refreshErr)
			}
			CodexPool.MarkExpired(accountEntryID)
		}
		writeJSONError(w, http.StatusBadGateway, "Codex token expired")
		log.Printf("[Codex] 401 Unauthorized: %s", bodyStr)

	case http.StatusTooManyRequests:
		// Rate limited
		retryAfter := 60 // default
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if sec, err := strconv.Atoi(ra); err == nil {
				retryAfter = sec
			}
		}
		if accountEntryID != "" && CodexPool != nil {
			CodexPool.Release(accountEntryID, true, retryAfter)
			log.Printf("[Codex] Account %s rate limited for %ds", accountEntryID, retryAfter)
		}
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		writeJSONError(w, http.StatusTooManyRequests, "Codex rate limit exceeded, retry after "+strconv.Itoa(retryAfter)+"s")
		log.Printf("[Codex] 429 Rate Limited: %s", bodyStr)

	default:
		// Other errors (4xx, 5xx)
		statusCode := resp.StatusCode
		if statusCode >= 500 {
			statusCode = http.StatusBadGateway // Don't expose upstream 5xx directly
		}
		writeJSONError(w, statusCode, fmt.Sprintf("Codex API error (%d): %s", resp.StatusCode, bodyStr))
		log.Printf("[Codex] %d Error: %s", resp.StatusCode, bodyStr)
	}
}

// writeJSONError writes a JSON error response
func writeJSONError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}

// logCodexRequest logs a codex request to the database (async)
func logCodexRequest(alias, hash, provider, model string, promptTokens, completionTokens, totalTokens int, start time.Time, statusCode int) {
	latencyMs := int(time.Since(start).Milliseconds())
	go func() {
		reqLog := &db.RequestLog{
			KeyHash:          hash,
			KeyAlias:         alias,
			Provider:         provider,
			Model:            model,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			LatencyMs:        latencyMs,
			StatusCode:       statusCode,
			CreatedAt:        time.Now(),
		}
		if err := db.Record(reqLog); err != nil {
			log.Printf("[Codex] Failed to log request: %v", err)
		}
	}()
}
