package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"

	"gopkg.in/yaml.v3"
)

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

// Provider Provider 配置
type Provider struct {
	Name     string        `yaml:"-"` // 由 GetProvider 自动注入
	APIBase  string        `yaml:"api_base"`
	APIKey   string        `yaml:"api_key"`
	Models   []string      `yaml:"models"`
	AuthType string        `yaml:"auth_type"` // "openai", "anthropic", 或 "codex"
	Accounts []CodexAccount `yaml:"accounts,omitempty"` // Codex 多账号
}

// KeyConfig Key 路由配置
type KeyConfig struct {
	Key      string   `yaml:"key"`
	Alias    string   `yaml:"alias"`
	Provider string   `yaml:"provider"`
	Models   []string `yaml:"models"`
}

// LoadConfig 加载配置
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// 默认值
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
	for _, provider := range c.Providers {
		modelCount += len(provider.Models)
	}
	c.modelProviderIndex = make(map[string]string, modelCount)
	for name, provider := range c.Providers {
		for _, model := range provider.Models {
			c.modelProviderIndex[model] = name
		}
	}
}

// HashKey 计算 Key 的 Hash（安全存储）
func HashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:8]) // 只取前 8 字节，足够识别又不暴露原 Key
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

// GetProvider 获取 Provider 配置（注入 Name）
func (c *Config) GetProvider(name string) *Provider {
	p, ok := c.Providers[name]
	if !ok {
		return nil
	}
	p.Name = name
	return &p
}

// FindProviderByModel 根据 model 名称自动路由到对应 provider
func (c *Config) FindProviderByModel(model string) *Provider {
	if c.modelProviderIndex != nil {
		if name, ok := c.modelProviderIndex[model]; ok {
			return c.GetProvider(name)
		}
		return nil
	}
	for name, provider := range c.Providers {
		for _, m := range provider.Models {
			if m == model {
				p := provider
				p.Name = name
				return &p
			}
		}
	}
	return nil
}

// IsValidModel 检查模型是否允许
func (k *KeyConfig) IsValidModel(model string) bool {
	if len(k.Models) == 0 {
		return true // 空数组表示不限制
	}
	for _, m := range k.Models {
		if m == model {
			return true
		}
	}
	return false
}
