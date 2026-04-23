package provider

import (
	"fmt"
	"sync"
	"time"

	"gateway-proxy/config"
)

// AccountEntry represents a single Codex account in the pool
type AccountEntry struct {
	ID             string    `json:"id"`
	AccessToken    string    `json:"access_token"`
	RefreshToken   string    `json:"refresh_token"`
	AccountID      string    `json:"account_id"`
	Label          string    `json:"label"`
	Status         string    `json:"status"` // "active", "expired", "rate_limited"
	UsageCount     int       `json:"usage_count"`
	LastUsed       time.Time `json:"last_used"`
	RateLimitReset time.Time `json:"rate_limit_reset,omitempty"`
}

// AccountPool manages multiple Codex accounts with rotation
type AccountPool struct {
	mu       sync.RWMutex
	accounts []AccountEntry
}

// NewAccountPool creates an account pool from config entries
func NewAccountPool(accounts []config.CodexAccount) *AccountPool {
	pool := &AccountPool{}
	now := time.Now()
	for i, acc := range accounts {
		if acc.AccessToken == "" {
			continue
		}
		pool.accounts = append(pool.accounts, AccountEntry{
			ID:           fmt.Sprintf("acc_%d", i),
			AccessToken:  acc.AccessToken,
			RefreshToken: acc.RefreshToken,
			AccountID:    acc.AccountID,
			Label:        acc.Label,
			Status:       "active",
			LastUsed:     now,
		})
	}
	return pool
}

// Acquire returns the best available account using least_used strategy.
// Returns nil if no account is available.
func (p *AccountPool) Acquire() *AccountEntry {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	var best *AccountEntry
	bestUsage := int(^uint(0) >> 1) // max int

	for i := range p.accounts {
		entry := &p.accounts[i]

		// Check if rate limit has expired
		if entry.Status == "rate_limited" && !entry.RateLimitReset.IsZero() {
			if now.After(entry.RateLimitReset) {
				entry.Status = "active"
				entry.RateLimitReset = time.Time{}
			}
		}

		if entry.Status != "active" {
			continue
		}

		if entry.UsageCount < bestUsage {
			bestUsage = entry.UsageCount
			best = entry
		}
	}

	if best != nil {
		best.UsageCount++
		best.LastUsed = now
	}

	return best
}

// Release updates the account after a successful request.
// If rateLimited is true, marks the account as rate limited.
func (p *AccountPool) Release(entryID string, rateLimited bool, resetAfterSec int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range p.accounts {
		if p.accounts[i].ID == entryID {
			if rateLimited {
				p.accounts[i].Status = "rate_limited"
				if resetAfterSec > 0 {
					p.accounts[i].RateLimitReset = time.Now().Add(time.Duration(resetAfterSec) * time.Second)
				} else {
					p.accounts[i].RateLimitReset = time.Now().Add(60 * time.Second) // default 60s
				}
			}
			return
		}
	}
}

// UpdateToken updates the access_token for the account at the given index
func (p *AccountPool) UpdateToken(index int, newToken string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index >= 0 && index < len(p.accounts) {
		p.accounts[index].AccessToken = newToken
	}
}

// MarkExpired marks an account as expired (e.g. 401 from upstream)
func (p *AccountPool) MarkExpired(entryID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range p.accounts {
		if p.accounts[i].ID == entryID {
			p.accounts[i].Status = "expired"
			return
		}
	}
}

// HasActiveAccounts returns true if at least one active account exists
func (p *AccountPool) HasActiveAccounts() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, entry := range p.accounts {
		if entry.Status == "active" {
			return true
		}
	}
	return false
}

// GetActiveCount returns the number of active accounts
func (p *AccountPool) GetActiveCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, entry := range p.accounts {
		if entry.Status == "active" {
			count++
		}
	}
	return count
}

// GetAnyAccount returns any account from the pool (for usage queries, not real requests)
func (p *AccountPool) GetAnyAccount() *AccountEntry {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.accounts) == 0 {
		return nil
	}
	return &p.accounts[0]
}

// Summary returns a summary of the pool status
func (p *AccountPool) Summary() map[string]int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := map[string]int{
		"total":        len(p.accounts),
		"active":       0,
		"expired":      0,
		"rate_limited": 0,
	}
	for _, entry := range p.accounts {
		result[entry.Status]++
	}
	return result
}
