package main

import (
	"database/sql"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// DB 全局数据库连接
var DB *sql.DB

// InitDB 初始化数据库
func InitDB(path string) error {
	var err error
	DB, err = sql.Open("sqlite3", path)
	if err != nil {
		return err
	}

	// 创建表
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

	_, err = DB.Exec(schema)
	return err
}

// LogRequest 记录请求
func LogRequest(log *RequestLog) error {
	_, err := DB.Exec(`
		INSERT INTO requests (key_hash, key_alias, provider, model, prompt_tokens, completion_tokens, total_tokens, latency_ms, status_code, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, log.KeyHash, log.KeyAlias, log.Provider, log.Model, log.PromptTokens, log.CompletionTokens, log.TotalTokens, log.LatencyMs, log.StatusCode, log.CreatedAt)
	return err
}

// GetStats 获取统计数据
func GetStats(key string, provider string, days int) (*StatsResponse, error) {
	// 构建查询条件
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

	// 汇总
	var summary Summary
	err := DB.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(prompt_tokens),0), COALESCE(SUM(completion_tokens),0), COALESCE(SUM(total_tokens),0)
		FROM requests `+conditions, args...).
		Scan(&summary.TotalRequests, &summary.TotalPromptTokens, &summary.TotalCompletionTokens, &summary.TotalTokens)
	if err != nil {
		return nil, err
	}

	// 按 Key 统计
	rows, err := DB.Query(`
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

	// 按 Model 统计
	rows2, err := DB.Query(`
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

// itoa 简单 int to string
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

func atoiSafe(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// Now 获取当前时间
func Now() time.Time {
	return time.Now()
}
