package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"gateway-proxy/admin"
	"gateway-proxy/config"
	"gateway-proxy/db"
	"gateway-proxy/provider"
	"gateway-proxy/usage"
)

func main() {
	// Load config
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	config.SetGlobal(cfg)
	config.ConfigPath = "config.yaml"

	// Init database
	if err := db.Init(cfg.DBPath); err != nil {
		log.Fatalf("Failed to init database: %v", err)
	}

	// Init Codex pool
	provider.InitCodex()

	// Register routes
	mux := http.NewServeMux()

	// API routes (auth handled by provider)
	mux.HandleFunc("POST /v1/chat/completions", provider.HandleChatCompletions)

	// Admin routes
	mux.HandleFunc("GET /admin/stats", admin.HandleStats)
	mux.HandleFunc("GET /admin/usage", usage.HandleUsage)
	mux.HandleFunc("GET /admin/ui", admin.HandleUI)
	mux.HandleFunc("GET /admin/", admin.HandleUI)
	mux.HandleFunc("GET /admin/auth", admin.HandleAuth)
	mux.HandleFunc("GET /admin/keys", admin.HandleListKeys)
	mux.HandleFunc("POST /admin/keys", admin.HandleCreateKey)
	mux.HandleFunc("PUT /admin/keys/", admin.HandleUpdateKey)
	mux.HandleFunc("DELETE /admin/keys/", admin.HandleDeleteKey)
	mux.HandleFunc("GET /admin/user/stats", admin.HandleUserStats)
	mux.HandleFunc("GET /admin/user/info", admin.HandleUserInfo)

	// Utility routes
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /v1/models", handleModels)
	mux.HandleFunc("GET /auth/status", handleAuthStatus)

	addr := ":" + cfg.Port
	fmt.Printf("🚀 OmniProxy starting on %s\n", addr)
	fmt.Printf("   Admin UI: http://localhost%s/admin/ui\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func handleModels(w http.ResponseWriter, r *http.Request) {
	cfg := config.Global
	models := []map[string]interface{}{}
	for name, prov := range cfg.Providers {
		if prov.AuthType != "codex" && strings.HasPrefix(prov.APIKey, "your-") {
			continue
		}
		for _, model := range prov.Models {
			models = append(models, map[string]interface{}{
				"id":       model,
				"object":   "model",
				"provider": name,
				"owned_by": "qimiaobit",
			})
		}
		// Also list virtual models from model_map
		for vm := range prov.ModelMap {
			models = append(models, map[string]interface{}{
				"id":       vm,
				"object":   "model",
				"provider": name,
				"owned_by": "qimiaobit",
			})
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   models,
	})
}

func handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := map[string]interface{}{
		"status":        "not_configured",
		"oauth_enabled":  false,
		"accounts":      0,
		"message":       "Phase 1: OAuth not yet implemented. Use manual access_token in config.yaml.",
	}
	if provider.CodexPool != nil {
		summary := provider.CodexPool.Summary()
		status["accounts"] = summary["total"]
		status["active"] = summary["active"]
		if summary["active"] > 0 {
			status["status"] = "active"
		}
	}
	json.NewEncoder(w).Encode(status)
}
