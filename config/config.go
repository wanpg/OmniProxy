package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// mu protects config mutations (key CRUD + persist)
var mu sync.Mutex

// Global 全局配置实例
var Global *Config

// SetGlobal 设置全局配置
func SetGlobal(cfg *Config) {
	Global = cfg
}

// Config 配置
type Config struct {
	Port      string              `yaml:"port"`
	AdminKey  string              `yaml:"admin_key"`
	Providers map[string]Provider `yaml:"providers"`
	Keys      []KeyConfig         `yaml:"keys"`
	DBPath    string              `yaml:"db_path"`

	keyIndex           map[string]*KeyConfig
	modelProviderIndex map[string]string
}

// CodexAccount represents a Codex account configuration
type CodexAccount struct {
	AccessToken  string `yaml:"access_token"`
	RefreshToken string `yaml:"refresh_token"`
	AccountID    string `yaml:"account_id"`
	Label        string `yaml:"label"`
}

// ModelMapEntry defines a virtual model → real model + defaults
type ModelMapEntry struct {
	Model           string `yaml:"model"`            // real model name to send upstream
	ReasoningEffort string `yaml:"reasoning_effort"` // auto-set reasoning effort
}

// Provider Provider 配置
type Provider struct {
	Name     string          `yaml:"-"`
	APIBase  string          `yaml:"api_base"`
	APIKey   string          `yaml:"api_key"`
	Models   []string        `yaml:"models"`
	AuthType string          `yaml:"auth_type"` // "openai", "anthropic", "codex"
	Accounts []CodexAccount  `yaml:"accounts,omitempty"`
	ModelMap map[string]ModelMapEntry `yaml:"model_map,omitempty"` // virtual model mapping
}

// KeyConfig Key 路由配置
type KeyConfig struct {
	Key      string   `yaml:"key"`
	Alias    string   `yaml:"alias"`
	Provider string   `yaml:"provider"`
	Models   []string `yaml:"models"`
}

// Load 加载配置
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	if cfg.DBPath == "" {
		cfg.DBPath = "gateway.db"
	}

	cfg.buildIndexes()
	return &cfg, nil
}

func (c *Config) buildIndexes() {
	c.keyIndex = make(map[string]*KeyConfig, len(c.Keys))
	for i := range c.Keys {
		c.keyIndex[c.Keys[i].Key] = &c.Keys[i]
	}

	modelCount := 0
	for _, p := range c.Providers {
		modelCount += len(p.Models)
		modelCount += len(p.ModelMap) // virtual models also count
	}
	c.modelProviderIndex = make(map[string]string, modelCount)
	for name, p := range c.Providers {
		for _, m := range p.Models {
			c.modelProviderIndex[m] = name
		}
		// Register virtual models from model_map
		for vm := range p.ModelMap {
			c.modelProviderIndex[vm] = name
		}
	}
}

// HashKey 计算 Key 的 Hash
func HashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:8])
}

// FindKeyConfig 根据 Key 查找配置
func (c *Config) FindKeyConfig(key string) *KeyConfig {
	if c.keyIndex != nil {
		return c.keyIndex[key]
	}
	for i := range c.Keys {
		if c.Keys[i].Key == key {
			return &c.Keys[i]
		}
	}
	return nil
}

// IsAdminKey 判断是否为 Admin Key
func (c *Config) IsAdminKey(key string) bool {
	return key == c.AdminKey
}

// GetProvider 获取 Provider 配置
func (c *Config) GetProvider(name string) *Provider {
	p, ok := c.Providers[name]
	if !ok {
		return nil
	}
	p.Name = name
	return &p
}

// ResolveModel resolves a virtual model to real model + optional reasoning_effort override
func (p *Provider) ResolveModel(model string) (realModel string, reasoningEffort string) {
	if entry, ok := p.ModelMap[model]; ok {
		return entry.Model, entry.ReasoningEffort
	}
	return model, "" // pass through
}

// FindProviderByModel 根据 model 路由到 provider
func (c *Config) FindProviderByModel(model string) *Provider {
	if c.modelProviderIndex != nil {
		if name, ok := c.modelProviderIndex[model]; ok {
			return c.GetProvider(name)
		}
		return nil
	}
	for name, p := range c.Providers {
		for _, m := range p.Models {
			if m == model {
				cp := p
				cp.Name = name
				return &cp
			}
		}
	}
	return nil
}

// IsValidModel 检查模型是否允许
func (k *KeyConfig) IsValidModel(model string) bool {
	if len(k.Models) == 0 {
		return true
	}
	for _, m := range k.Models {
		if m == model {
			return true
		}
	}
	return false
}

// MaskKey 脱敏显示 Key：保留前8后4，中间 ****
func MaskKey(key string) string {
	if len(key) <= 12 {
		return "****"
	}
	return key[:8] + "****" + key[len(key)-4:]
}

// GenerateKey 生成随机 API Key
func GenerateKey() string {
	b := make([]byte, 16)
	rand.Read(b)
	return "sk-qmbit-proxy-" + hex.EncodeToString(b)
}

// AddKey 新增 key
func (c *Config) AddKey(kc KeyConfig) error {
	mu.Lock()
	defer mu.Unlock()
	if c.FindKeyConfig(kc.Key) != nil {
		return fmt.Errorf("key already exists")
	}
	c.Keys = append(c.Keys, kc)
	c.buildIndexes()
	return nil
}

// UpdateKeyByAlias 根据 alias 修改 key
func (c *Config) UpdateKeyByAlias(alias string, newAlias string, provider string, models []string) error {
	mu.Lock()
	defer mu.Unlock()
	for i := range c.Keys {
		if c.Keys[i].Alias == alias {
			if alias == "Admin Key" {
				return fmt.Errorf("cannot modify admin key")
			}
			c.Keys[i].Alias = newAlias
			c.Keys[i].Provider = provider
			c.Keys[i].Models = models
			c.buildIndexes()
			return nil
		}
	}
	return fmt.Errorf("key not found: %s", alias)
}

// DeleteKeyByAlias 根据 alias 删除 key
func (c *Config) DeleteKeyByAlias(alias string) error {
	mu.Lock()
	defer mu.Unlock()
	if alias == "Admin Key" {
		return fmt.Errorf("cannot delete admin key")
	}
	for i, k := range c.Keys {
		if k.Alias == alias {
			c.Keys = append(c.Keys[:i], c.Keys[i+1:]...)
			c.buildIndexes()
			return nil
		}
	}
	return fmt.Errorf("key not found: %s", alias)
}

// Persist 将配置写回文件
func (c *Config) Persist(path string) error {
	mu.Lock()
	defer mu.Unlock()
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// ConfigPath stores the loaded config file path
var ConfigPath string
