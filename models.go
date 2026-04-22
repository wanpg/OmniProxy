package main

import "time"

// RequestLog 请求日志
type RequestLog struct {
	ID               int64
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
	Summary Summary    `json:"summary"`
	ByKey  []KeyStats `json:"by_key"`
	ByModel []ModelStats `json:"by_model"`
}

type Summary struct {
	TotalRequests        int `json:"total_requests"`
	TotalPromptTokens    int `json:"total_prompt_tokens"`
	TotalCompletionTokens int `json:"total_completion_tokens"`
	TotalTokens          int `json:"total_tokens"`
}

type KeyStats struct {
	KeyAlias         string `json:"key_alias"`
	Provider         string `json:"provider"`
	Requests         int    `json:"requests"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
}

type ModelStats struct {
	Model       string `json:"model"`
	Provider    string `json:"provider"`
	Requests    int    `json:"requests"`
	TotalTokens int    `json:"total_tokens"`
}

// OpenAI Chat Completions Request/Response
type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIChatRequest struct {
	Model    string          `json:"model"`
	Messages []OpenAIMessage `json:"messages"`
	Stream   bool            `json:"stream,omitempty"`
}

type OpenAIChatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Usage   struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// Anthropic Messages Request/Response
type AnthropicMessageRequest struct {
	Model       string          `json:"model"`
	Messages    []AnthropicMsg  `json:"messages"`
	MaxTokens   int             `json:"max_tokens"`
	SystemPrompt string         `json:"system,omitempty"`
}

type AnthropicMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AnthropicResponse struct {
	ID     string `json:"id"`
	Model  string `json:"model"`
	Usage  struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}
