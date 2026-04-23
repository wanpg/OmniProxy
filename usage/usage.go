package usage

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"gateway-proxy/config"
	"gateway-proxy/provider"
)

// ─── Response types for the /admin/usage API ───

type UsageResponse struct {
	Timestamp int64                    `json:"timestamp"`
	Providers map[string]ProviderUsage `json:"providers"`
}

type ProviderUsage struct {
	Status string         `json:"status"` // "ok", "error", "not_configured"
	Error  string         `json:"error,omitempty"`

	// MiniMax fields
	Used       int `json:"used,omitempty"`
	Total      int `json:"total,omitempty"`
	Model      string `json:"model,omitempty"`

	// Zhipu fields
	Plan         string `json:"plan,omitempty"`
	ZhipuMcp     *ZhipuMcpUsage    `json:"mcp_monthly,omitempty"`
	ZhipuTokens  *ZhipuTokenUsage   `json:"tokens_5h,omitempty"`

	// Generic window fields (used by Codex and MiniMax)
	Window5h    *WindowUsage `json:"5h_window,omitempty"`
	WindowWeekly *WindowUsage `json:"weekly_window,omitempty"`
}

type WindowUsage struct {
	UsedPercent   int `json:"used_percent"`
	ResetSeconds  int `json:"reset_seconds"`
}

type MiniMaxUsage struct {
	Used         int `json:"used"`
	Total        int `json:"total"`
	ResetSeconds int `json:"reset_seconds"`
	Model        string `json:"model"`
}

// ZhipuMcpUsage represents MCP tool call limits (TIME_LIMIT = 5h window)
type ZhipuMcpUsage struct {
	Used         int `json:"used"`
	Total        int `json:"total"`
	Remaining    int `json:"remaining"`
	Percentage   int `json:"percentage"`
	ResetSeconds int `json:"reset_seconds"`
}

// ZhipuTokenUsage represents model token limits (TOKENS_LIMIT = 3h window)
type ZhipuTokenUsage struct {
	Percentage   int `json:"percentage"`
	ResetSeconds int `json:"reset_seconds"`
}

// ─── Raw API response types ───

type codexUsageResponse struct {
	PlanType  string `json:"plan_type"`
	RateLimit struct {
		Allowed      bool `json:"allowed"`
		LimitReached bool `json:"limit_reached"`
		PrimaryWindow struct {
			UsedPercent       int `json:"used_percent"`
			LimitWindowSec    int `json:"limit_window_seconds"`
			ResetAfterSec     int `json:"reset_after_seconds"`
		} `json:"primary_window"`
		SecondaryWindow struct {
			UsedPercent       int `json:"used_percent"`
			LimitWindowSec    int `json:"limit_window_seconds"`
			ResetAfterSec     int `json:"reset_after_seconds"`
		} `json:"secondary_window"`
	} `json:"rate_limit"`
}

type minimaxUsageResponse struct {
	ModelRemains []minimaxModelRemains `json:"model_remains"`
}

type minimaxModelRemains struct {
	ModelName              string `json:"model_name"`
	CurrentIntervalTotal   int    `json:"current_interval_total_count"`
	CurrentIntervalUsage   int    `json:"current_interval_usage_count"`
	RemainsTime            int    `json:"remains_time"`
	StartTime              int64  `json:"start_time"`
	EndTime                int64  `json:"end_time"`
	WeeklyStartTime         int64  `json:"weekly_start_time"`
	WeeklyEndTime           int64  `json:"weekly_end_time"`
}

type zhipuUsageResponse struct {
	Code int `json:"code"`
	Data struct {
		Limits []zhipuLimit `json:"limits"`
		Level  string       `json:"level"`
	} `json:"data"`
}

type zhipuLimit struct {
	Type           string `json:"type"`
	Unit           int    `json:"unit"`
	Number         int    `json:"number"`
	Usage          int    `json:"usage"`
	CurrentValue   int    `json:"currentValue"`
	Remaining      int    `json:"remaining"`
	Percentage     int    `json:"percentage"`
	NextResetTime  int64  `json:"nextResetTime"`
}

// ─── Provider usage fetchers ───

func fetchCodexUsage() ProviderUsage {
	if provider.CodexPool == nil {
		return ProviderUsage{Status: "not_configured"}
	}

	// Get any account for querying usage (not a real request)
	entry := provider.CodexPool.GetAnyAccount()
	if entry == nil {
		return ProviderUsage{Status: "not_configured"}
	}

	req, err := http.NewRequest("GET", "https://chatgpt.com/backend-api/wham/usage", nil)
	if err != nil {
		return ProviderUsage{Status: "error", Error: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+entry.AccessToken)
	if entry.AccountID != "" {
		req.Header.Set("Chatgpt-Account-Id", entry.AccountID)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := provider.HTTPClient.Do(req)
	if err != nil {
		return ProviderUsage{Status: "error", Error: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ProviderUsage{Status: "error", Error: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}

	var data codexUsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return ProviderUsage{Status: "error", Error: "parse error: " + err.Error()}
	}

	return ProviderUsage{
		Status: "ok",
		Window5h: &WindowUsage{
			UsedPercent:  data.RateLimit.PrimaryWindow.UsedPercent,
			ResetSeconds: data.RateLimit.PrimaryWindow.ResetAfterSec,
		},
		WindowWeekly: &WindowUsage{
			UsedPercent:  data.RateLimit.SecondaryWindow.UsedPercent,
			ResetSeconds: data.RateLimit.SecondaryWindow.ResetAfterSec,
		},
	}
}

func fetchMiniMaxUsage() ProviderUsage {
	prov := config.Global.GetProvider("minimax")
	if prov == nil || prov.APIKey == "" {
		return ProviderUsage{Status: "not_configured"}
	}

	req, err := http.NewRequest("GET", "https://www.minimaxi.com/v1/api/openplatform/coding_plan/remains", nil)
	if err != nil {
		return ProviderUsage{Status: "error", Error: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+prov.APIKey)

	resp, err := provider.HTTPClient.Do(req)
	if err != nil {
		return ProviderUsage{Status: "error", Error: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ProviderUsage{Status: "error", Error: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}

	var data minimaxUsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return ProviderUsage{Status: "error", Error: "parse error: " + err.Error()}
	}

	// Find MiniMax-M* entry
	// NOTE: current_interval_usage_count is actually remaining count, not used
	now := time.Now().UnixMilli()
	for _, m := range data.ModelRemains {
		if m.ModelName == "MiniMax-M*" {
			used := m.CurrentIntervalTotal - m.CurrentIntervalUsage
			pct := 0
			if m.CurrentIntervalTotal > 0 {
				pct = used * 100 / m.CurrentIntervalTotal
			}
			// Calculate reset from end_time, not remains_time
			resetMs := m.EndTime - now
			resetSec := int(resetMs / 1000)
			if resetSec < 0 {
				resetSec = 0
			}
			return ProviderUsage{
				Status: "ok",
				Window5h: &WindowUsage{
					UsedPercent:  pct,
					ResetSeconds: resetSec,
				},
				Used:  used,
				Total: m.CurrentIntervalTotal,
				Model: m.ModelName,
			}
		}
	}

	return ProviderUsage{Status: "error", Error: "no MiniMax-M* entry found"}
}

func fetchZhipuUsage() ProviderUsage {
	prov := config.Global.GetProvider("zhipu")
	if prov == nil || prov.APIKey == "" {
		return ProviderUsage{Status: "not_configured"}
	}

	req, err := http.NewRequest("GET", "https://open.bigmodel.cn/api/monitor/usage/quota/limit", nil)
	if err != nil {
		return ProviderUsage{Status: "error", Error: err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+prov.APIKey)

	resp, err := provider.HTTPClient.Do(req)
	if err != nil {
		return ProviderUsage{Status: "error", Error: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ProviderUsage{Status: "error", Error: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}

	var data zhipuUsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return ProviderUsage{Status: "error", Error: "parse error: " + err.Error()}
	}

	if data.Code != 200 {
		return ProviderUsage{Status: "error", Error: fmt.Sprintf("API code %d", data.Code)}
	}

	usage := ProviderUsage{
		Status: "ok",
		Plan:   data.Data.Level,
	}

	now := time.Now().UnixMilli()

	for _, lim := range data.Data.Limits {
		switch lim.Type {
		case "TIME_LIMIT":
			// TIME_LIMIT = MCP tool call limits (5h window)
			resetMs := lim.NextResetTime - now
			resetSec := int(resetMs / 1000)
			if resetSec < 0 {
				resetSec = 0
			}
			usage.ZhipuMcp = &ZhipuMcpUsage{
				Used:         lim.CurrentValue,
				Total:        lim.Usage,
				Remaining:    lim.Remaining,
				Percentage:   lim.Percentage,
				ResetSeconds: resetSec,
			}
		case "TOKENS_LIMIT":
			// TOKENS_LIMIT = model token limits (3h window)
			resetMs := lim.NextResetTime - now
			resetSec := int(resetMs / 1000)
			if resetSec < 0 {
				resetSec = 0
			}
			usage.ZhipuTokens = &ZhipuTokenUsage{
				Percentage:   lim.Percentage,
				ResetSeconds: resetSec,
			}
		}
	}

	return usage
}

// ─── Handler ───

func HandleUsage(w http.ResponseWriter, r *http.Request) {
	authKey := provider.ExtractAPIKey(r)
	if !config.Global.IsAdminKey(authKey) {
		http.Error(w, `{"error":"Admin key required"}`, http.StatusUnauthorized)
		return
	}

	providers := make(map[string]ProviderUsage, 3)
	providers["codex"] = fetchCodexUsage()
	providers["minimax"] = fetchMiniMaxUsage()
	providers["zhipu"] = fetchZhipuUsage()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(UsageResponse{
		Timestamp: time.Now().Unix(),
		Providers: providers,
	})
}
