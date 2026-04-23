package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	// 加载配置
	configPath := "config.yaml"
	if envPath := os.Getenv("CONFIG_PATH"); envPath != "" {
		configPath = envPath
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 初始化数据库
	if err := InitDB(cfg.DBPath); err != nil {
		log.Fatalf("Failed to init DB: %v", err)
	}

	// 设置全局配置
	setConfig(cfg)

	// 初始化 Codex 账号池
	initCodexPool()

	// 路由设置
	mux := http.NewServeMux()

	// 健康检查
	mux.HandleFunc("/health", handleHealth)

	// 模型列表（公开）
	mux.HandleFunc("/v1/models", handleModels)

	// Admin 统计（需要 Admin Key）
	mux.HandleFunc("/admin/stats", handleAdminStats)
	mux.HandleFunc("/admin/usage", handleUsageAPI)
	mux.HandleFunc("/admin/ui", handleAdminUI)

	// OpenAI 风格聊天接口（需要普通 Key）
	mux.Handle("/v1/chat/completions", authMiddleware(handleChatCompletions))

	// Anthropic 风格接口
	mux.Handle("/v1/messages", authMiddleware(handleAnthropicMessages))

	// Anthropic 也支持 /v1/messages Beta 版本
	mux.Handle("/v1/messages_beta", authMiddleware(handleAnthropicMessages))

	// Codex auth status (Phase 2 OAuth 准备，Phase 1 返回占位)
	mux.HandleFunc("/auth/status", handleAuthStatus)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      mux,
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 120 * time.Second,
	}

	log.Printf("Gateway Proxy started on :%s", cfg.Port)
	log.Fatal(server.ListenAndServe())
}
