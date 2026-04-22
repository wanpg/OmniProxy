package main

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
)

const (
	codexDefaultBaseURL = "https://chatgpt.com/backend-api/codex"
	codexUserAgent      = "codex-tui/0.118.0 (Mac OS 26.3.1; arm64) iTerm.app/3.6.9 (codex-tui; 0.118.0)"
	codexOpenAIBeta     = "responses_websockets=2026-02-06"
)

// codexPool is the global account pool, initialized from config
var codexPool *AccountPool

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
func initCodexPool() {
	provider := globalConfig.GetProvider("codex")
	if provider == nil {
		log.Println("[Codex] No codex provider configured, skipping")
		return
	}

	// 1. Try config.yaml accounts array
	if len(provider.Accounts) > 0 {
		codexPool = NewAccountPool(provider.Accounts)
		log.Printf("[Codex] Loaded %d accounts from config.yaml", len(provider.Accounts))
	}

	// 2. Try config.yaml api_key
	if codexPool == nil && provider.APIKey != "" && !strings.HasPrefix(provider.APIKey, "your-") {
		codexPool = NewAccountPool([]CodexAccount{
			{AccessToken: provider.APIKey},
		})
		log.Println("[Codex] Loaded 1 account from config.yaml api_key")
	}

	// 3. Try ~/.codex/auth.json (Codex CLI auth file)
	if codexPool == nil {
		home, err := os.UserHomeDir()
		if err == nil {
			p := filepath.Join(home, ".codex", "auth.json")
			if acc, err := loadCodexAuthFile(p); err == nil {
				codexPool = NewAccountPool([]CodexAccount{*acc})
				codexAuthFile = p
				log.Printf("[Codex] Loaded account from %s (account_id: %s)", p, acc.AccountID)
			}
	}
	}

	if codexPool != nil {
		log.Printf("[Codex] Account pool initialized: %s", codexPool.Summary())
	}
}

// loadCodexAuthFile reads and parses the Codex CLI auth.json file
func loadCodexAuthFile(path string) (*CodexAccount, error) {
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

	return &CodexAccount{
		AccessToken:  auth.Tokens.AccessToken,
		RefreshToken: auth.Tokens.RefreshToken,
		AccountID:    auth.Tokens.AccountID,
		Label:        "codex-cli",
	}, nil
}

// refreshCodexToken uses `codex auth refresh` to get a new access_token
func refreshCodexToken() error {
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
	if codexAuthFile != "" && codexPool != nil {
		if acc, err := loadCodexAuthFile(codexAuthFile); err == nil {
			// Update the first active account
			codexPool.UpdateToken(0, acc.AccessToken)
			log.Println("[Codex] Token refreshed via codex CLI")
		}
	}

	return nil
}

// getCodexBaseURL returns the Codex API base URL from provider config or default
func getCodexBaseURL(provider *Provider) string {
	if provider.APIBase != "" {
		return provider.APIBase
	}
	return codexDefaultBaseURL
}

// handleCodexProxy handles requests for the codex provider.
// It converts Chat Completions format to Codex Responses API format,
// forwards to chatgpt.com, and converts the response back.
func handleCodexProxy(w http.ResponseWriter, r *http.Request, provider *Provider, alias string, hash string, model string, body []byte) {
	start := time.Now()

	// 1. Parse the Chat Completions request
	var chatReq ChatCompletionRequest
	if err := json.Unmarshal(body, &chatReq); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	// 2. Acquire an account
	var accessToken string
	var accountID string
	var accountEntryID string

	if codexPool != nil {
		entry := codexPool.Acquire()
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
	resp, err := httpClient.Do(httpReq)
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
			codexPool.Release(accountEntryID, false, 0)
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
			codexPool.Release(accountEntryID, false, 0)
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
		if accountEntryID != "" && codexPool != nil {
			log.Printf("[Codex] Account %s token expired, attempting refresh...", accountEntryID)
			if refreshErr := refreshCodexToken(); refreshErr == nil {
				log.Printf("[Codex] Token refresh succeeded, retry is not automatic — client should retry")
			} else {
				log.Printf("[Codex] Token refresh failed: %v", refreshErr)
			}
			codexPool.MarkExpired(accountEntryID)
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
		if accountEntryID != "" && codexPool != nil {
			codexPool.Release(accountEntryID, true, retryAfter)
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
		reqLog := &RequestLog{
			KeyHash:          hash,
			KeyAlias:         alias,
			Provider:         provider,
			Model:            model,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			LatencyMs:        latencyMs,
			StatusCode:       statusCode,
			CreatedAt:        Now(),
		}
		if err := LogRequest(reqLog); err != nil {
			log.Printf("[Codex] Failed to log request: %v", err)
		}
	}()
}
