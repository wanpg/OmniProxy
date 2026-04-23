package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"

	"gopkg.in/yaml.v3"
)

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

// Provider Provider 配置
type Provider struct {
	Name     string         `yaml:"-"`
	APIBase  string         `yaml:"api_base"`
	APIKey   string         `yaml:"api_key"`
	Models   []string       `yaml:"models"`
	AuthType string         `yaml:"auth_type"` // "openai", "anthropic", "codex"
	Accounts []CodexAccount `yaml:"accounts,omitempty"`
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
	}
	c.modelProviderIndex = make(map[string]string, modelCount)
	for name, p := range c.Providers {
		for _, m := range p.Models {
			c.modelProviderIndex[m] = name
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
