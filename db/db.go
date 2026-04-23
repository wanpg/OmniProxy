package db

import (
	"database/sql"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Global 全局数据库连接
var Global *sql.DB

// Init 初始化数据库
func Init(path string) error {
	var err error
	Global, err = sql.Open("sqlite3", path)
	if err != nil {
		return err
	}

	schema := `
	CREATE TABLE IF NOT EXISTS requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key_hash TEXT NOT NULL,
		key_alias TEXT NOT NULL,
		provider TEXT NOT NULL,
		model TEXT NOT NULL,
		prompt_tokens INTEGER DEFAULT 0,
		completion_tokens INTEGER DEFAULT 0,
		total_tokens INTEGER DEFAULT 0,
		latency_ms INTEGER DEFAULT 0,
		status_code INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_requests_key_hash ON requests(key_hash);
	CREATE INDEX IF NOT EXISTS idx_requests_created_at ON requests(created_at);
	CREATE INDEX IF NOT EXISTS idx_requests_provider ON requests(provider);
	`
	_, err = Global.Exec(schema)
	return err
}

// RequestLog 请求日志
type RequestLog struct {
	KeyHash          string
	KeyAlias         string
	Provider         string
	Model            string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	LatencyMs        int
	StatusCode       int
	CreatedAt        time.Time
}

// StatsResponse 统计响应
type StatsResponse struct {
	Summary Summary     `json:"summary"`
	ByKey   []KeyStats  `json:"by_key"`
	ByModel []ModelStats `json:"by_model"`
}

// Summary 汇总统计
type Summary struct {
	TotalRequests         int `json:"total_requests"`
	TotalPromptTokens     int `json:"total_prompt_tokens"`
	TotalCompletionTokens int `json:"total_completion_tokens"`
	TotalTokens           int `json:"total_tokens"`
}

// KeyStats 按 Key 统计
type KeyStats struct {
	KeyAlias         string `json:"key_alias"`
	Provider         string `json:"provider"`
	Requests         int    `json:"requests"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
}

// ModelStats 按模型统计
type ModelStats struct {
	Model       string `json:"model"`
	Provider    string `json:"provider"`
	Requests    int    `json:"requests"`
	TotalTokens int    `json:"total_tokens"`
}

// Record 记录请求
func Record(log *RequestLog) error {
	_, err := Global.Exec(`
		INSERT INTO requests (key_hash, key_alias, provider, model, prompt_tokens, completion_tokens, total_tokens, latency_ms, status_code, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, log.KeyHash, log.KeyAlias, log.Provider, log.Model, log.PromptTokens, log.CompletionTokens, log.TotalTokens, log.LatencyMs, log.StatusCode, log.CreatedAt)
	return err
}

// GetStats 获取统计数据
func GetStats(key string, provider string, days int) (*StatsResponse, error) {
	conditions := "WHERE created_at >= datetime('now', ?)"
	args := []interface{}{"-" + itoa(days) + " days"}

	if key != "" {
		conditions += " AND key_alias = ?"
		args = append(args, key)
	}
	if provider != "" {
		conditions += " AND provider = ?"
		args = append(args, provider)
	}

	var summary Summary
	err := Global.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0), COALESCE(SUM(total_tokens),0)
		FROM requests `+conditions, args...).
		Scan(&summary.TotalRequests, &summary.TotalPromptTokens, &summary.TotalCompletionTokens, &summary.TotalTokens)
	if err != nil {
		return nil, err
	}

	rows, err := Global.Query(`
		SELECT key_alias, provider, COUNT(*), COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0), COALESCE(SUM(total_tokens),0)
		FROM requests `+conditions+`
		GROUP BY key_alias, provider
		ORDER BY COUNT(*) DESC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var byKey []KeyStats
	for rows.Next() {
		var s KeyStats
		if err := rows.Scan(&s.KeyAlias, &s.Provider, &s.Requests, &s.PromptTokens, &s.CompletionTokens, &s.TotalTokens); err != nil {
			return nil, err
		}
		byKey = append(byKey, s)
	}

	rows2, err := Global.Query(`
		SELECT model, provider, COUNT(*), COALESCE(SUM(total_tokens),0)
		FROM requests `+conditions+`
		GROUP BY model, provider
		ORDER BY COUNT(*) DESC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()

	var byModel []ModelStats
	for rows2.Next() {
		var s ModelStats
		if err := rows2.Scan(&s.Model, &s.Provider, &s.Requests, &s.TotalTokens); err != nil {
			return nil, err
		}
		byModel = append(byModel, s)
	}

	return &StatsResponse{
		Summary:  summary,
		ByKey:    byKey,
		ByModel:  byModel,
	}, nil
}

// AtoiSafe 安全字符串转整数
func AtoiSafe(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	n := len(b)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		b[n] = '-'
	}
	return string(b[n:])
}
