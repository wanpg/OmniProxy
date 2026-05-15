package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"gateway-proxy/config"
	"gateway-proxy/db"
	"gateway-proxy/provider"
)

// HandleAuth 验证 key 并返回角色信息
func HandleAuth(w http.ResponseWriter, r *http.Request) {
	key := provider.ExtractAPIKey(r)
	if key == "" {
		writeJSON(w, 401, map[string]string{"error": "missing api key"})
		return
	}
	cfg := config.Global
	if cfg.IsAdminKey(key) {
		writeJSON(w, 200, map[string]interface{}{
			"role":       "admin",
			"alias":      "Admin",
			"key_masked": config.MaskKey(key),
		})
		return
	}
	kc := cfg.FindKeyConfig(key)
	if kc == nil {
		writeJSON(w, 401, map[string]string{"error": "invalid api key"})
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"role":       "user",
		"alias":      kc.Alias,
		"key_masked": config.MaskKey(key),
		"provider":   kc.Provider,
		"models":     kc.Models,
	})
}

// HandleListKeys 列出所有 key（管理员）
func HandleListKeys(w http.ResponseWriter, r *http.Request) {
	key := provider.ExtractAPIKey(r)
	if !requireAdmin(key, w) {
		return
	}
	cfg := config.Global
	keys := make([]map[string]interface{}, 0, len(cfg.Keys))
	for _, k := range cfg.Keys {
		entry := map[string]interface{}{
			"alias":      k.Alias,
			"key_masked": config.MaskKey(k.Key),
			"provider":   k.Provider,
			"models":     k.Models,
			"is_admin":   k.Key == cfg.AdminKey,
		}
		keys = append(keys, entry)
	}
	writeJSON(w, 200, map[string]interface{}{"keys": keys})
}

// HandleCreateKey 新增 key（管理员）
func HandleCreateKey(w http.ResponseWriter, r *http.Request) {
	key := provider.ExtractAPIKey(r)
	if !requireAdmin(key, w) {
		return
	}
	var req struct {
		Key      string   `json:"key"`
		Alias    string   `json:"alias"`
		Provider string   `json:"provider"`
		Models   []string `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid body"})
		return
	}
	if req.Alias == "" {
		writeJSON(w, 400, map[string]string{"error": "alias is required"})
		return
	}
	if req.Key == "" {
		req.Key = config.GenerateKey()
	}
	kc := config.KeyConfig{
		Key:      req.Key,
		Alias:    req.Alias,
		Provider: req.Provider,
		Models:   req.Models,
	}
	if err := config.Global.AddKey(kc); err != nil {
		writeJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	_ = config.Global.Persist(config.ConfigPath)
	writeJSON(w, 201, map[string]interface{}{
		"alias":      kc.Alias,
		"key":        kc.Key, // 返回完整 key，仅此一次
		"key_masked": config.MaskKey(kc.Key),
	})
}

// HandleUpdateKey 修改 key（管理员）
func HandleUpdateKey(w http.ResponseWriter, r *http.Request) {
	key := provider.ExtractAPIKey(r)
	if !requireAdmin(key, w) {
		return
	}
	// URL: /admin/keys/{alias}
	alias := strings.TrimPrefix(r.URL.Path, "/admin/keys/")
	alias = strings.TrimRight(alias, "/")
	if alias == "" {
		writeJSON(w, 400, map[string]string{"error": "alias is required"})
		return
	}
	var req struct {
		Alias    string   `json:"alias"`
		Provider string   `json:"provider"`
		Models   []string `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid body"})
		return
	}
	newAlias := req.Alias
	if newAlias == "" {
		newAlias = alias
	}
	if err := config.Global.UpdateKeyByAlias(alias, newAlias, req.Provider, req.Models); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	_ = config.Global.Persist(config.ConfigPath)
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// HandleDeleteKey 删除 key（管理员）
func HandleDeleteKey(w http.ResponseWriter, r *http.Request) {
	key := provider.ExtractAPIKey(r)
	if !requireAdmin(key, w) {
		return
	}
	alias := strings.TrimPrefix(r.URL.Path, "/admin/keys/")
	alias = strings.TrimRight(alias, "/")
	if alias == "" {
		writeJSON(w, 400, map[string]string{"error": "alias is required"})
		return
	}
	if err := config.Global.DeleteKeyByAlias(alias); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	_ = config.Global.Persist(config.ConfigPath)
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

// HandleUserStats 用户自己的用量统计
func HandleUserStats(w http.ResponseWriter, r *http.Request) {
	key := provider.ExtractAPIKey(r)
	if key == "" {
		writeJSON(w, 401, map[string]string{"error": "missing api key"})
		return
	}
	cfg := config.Global
	kc := cfg.FindKeyConfig(key)
	if kc == nil && !cfg.IsAdminKey(key) {
		writeJSON(w, 401, map[string]string{"error": "invalid api key"})
		return
	}
	alias := "Admin"
	if kc != nil {
		alias = kc.Alias
	}
	days := 30
	if v := r.URL.Query().Get("days"); v != "" {
		if n := db.AtoiSafe(v); n > 0 {
			days = n
		}
	}
	stats, err := db.GetStats(alias, "", days)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, stats)
}

// HandleUserInfo 用户的 key 信息
func HandleUserInfo(w http.ResponseWriter, r *http.Request) {
	key := provider.ExtractAPIKey(r)
	if key == "" {
		writeJSON(w, 401, map[string]string{"error": "missing api key"})
		return
	}
	cfg := config.Global
	if cfg.IsAdminKey(key) {
		writeJSON(w, 200, map[string]interface{}{
			"role":       "admin",
			"alias":      "Admin",
			"key_masked": config.MaskKey(key),
			"models":     getAllModels(cfg),
		})
		return
	}
	kc := cfg.FindKeyConfig(key)
	if kc == nil {
		writeJSON(w, 401, map[string]string{"error": "invalid api key"})
		return
	}
	models := kc.Models
	if len(models) == 0 {
		allModels := getAllModels(cfg)
		models = make([]string, 0, len(allModels))
		for _, m := range allModels {
			models = append(models, m["id"].(string))
		}
	}
	writeJSON(w, 200, map[string]interface{}{
		"role":       "user",
		"alias":      kc.Alias,
		"key_masked": config.MaskKey(key),
		"provider":   kc.Provider,
		"models":     models,
	})
}

// helpers

func requireAdmin(key string, w http.ResponseWriter) bool {
	if key == "" {
		writeJSON(w, 401, map[string]string{"error": "missing api key"})
		return false
	}
	if !config.Global.IsAdminKey(key) {
		writeJSON(w, 403, map[string]string{"error": "admin key required"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func getAllModels(cfg *config.Config) []map[string]interface{} {
	models := []map[string]interface{}{}
	for name, prov := range cfg.Providers {
		if prov.AuthType != "codex" && strings.HasPrefix(prov.APIKey, "your-") {
			continue
		}
		for _, m := range prov.Models {
			models = append(models, map[string]interface{}{
				"id":       m,
				"provider": name,
			})
		}
		for vm := range prov.ModelMap {
			models = append(models, map[string]interface{}{
				"id":       vm,
				"provider": name,
			})
		}
	}
	return models
}
