package provider

import "encoding/json"

// --- Codex API Types ---

// CodexResponsesRequest is the request body for POST /codex/responses
type CodexResponsesRequest struct {
	Model        string            `json:"model"`
	Instructions string            `json:"instructions,omitempty"`
	Input        []CodexInputItem  `json:"input"`
	Stream       bool              `json:"stream"`
	Store        bool              `json:"store"`
	Reasoning    *CodexReasoning   `json:"reasoning,omitempty"`
	Tools        []json.RawMessage `json:"tools,omitempty"`
	ToolChoice   json.RawMessage   `json:"tool_choice,omitempty"`
	Text         *CodexTextConfig  `json:"text,omitempty"`
}

// CodexReasoning configures reasoning effort
type CodexReasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// CodexTextConfig configures text output format (JSON mode / structured outputs)
type CodexTextConfig struct {
	Format CodexTextFormat `json:"format"`
}

// CodexTextFormat defines the text format type
type CodexTextFormat struct {
	Type   string          `json:"type"`
	Name   string          `json:"name,omitempty"`
	Schema json.RawMessage `json:"schema,omitempty"`
	Strict *bool           `json:"strict,omitempty"`
}

// CodexInputItem represents an item in the input array.
// It can be a message (user/assistant) or a function_call/function_call_output.
type CodexInputItem struct {
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"` // string or []CodexContentPart
	Type    string          `json:"type,omitempty"`    // "function_call" or "function_call_output"
	CallID  string          `json:"call_id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Arguments string        `json:"arguments,omitempty"`
	Output  string          `json:"output,omitempty"`
}

// CodexContentPart for multimodal input
type CodexContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// CodexEventData represents a parsed SSE event from the Codex stream.
// The type field inside the JSON data identifies the event type.
type CodexEventData struct {
	Type     string             `json:"type"`
	Delta    string             `json:"delta,omitempty"`
	Item     json.RawMessage    `json:"item,omitempty"`
	Response json.RawMessage    `json:"response,omitempty"`
	Error    *CodexStreamError  `json:"error,omitempty"`
	CallID   string             `json:"call_id,omitempty"`
	Name     string             `json:"name,omitempty"`
	ItemID   string             `json:"item_id,omitempty"`
}

// CodexStreamError represents an error from the Codex SSE stream
type CodexStreamError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CodexCompletedResponse is the response object from response.completed event
type CodexCompletedResponse struct {
	ID     string        `json:"id"`
	Status string        `json:"status"`
	Usage  *CodexUsage   `json:"usage,omitempty"`
	Output []interface{} `json:"output,omitempty"`
}

// CodexUsage represents token usage from Codex API
type CodexUsage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	TotalTokens     int `json:"total_tokens"`
	CachedTokens    int `json:"cached_tokens,omitempty"`
	ReasoningTokens int `json:"reasoning_tokens,omitempty"`
}

// --- OpenAI Chat Completion Types (extended for codex translation) ---

// ChatCompletionRequest is the full OpenAI Chat Completions request
type ChatCompletionRequest struct {
	Model           string              `json:"model"`
	Messages        []ChatCompletionMsg `json:"messages"`
	Stream          bool                `json:"stream,omitempty"`
	Tools           []ChatTool          `json:"tools,omitempty"`
	ToolChoice      json.RawMessage     `json:"tool_choice,omitempty"`
	ReasoningEffort string              `json:"reasoning_effort,omitempty"`
	ResponseFormat  *ResponseFormat     `json:"response_format,omitempty"`
	Temperature     *float64            `json:"temperature,omitempty"`
	MaxTokens       *int                `json:"max_tokens,omitempty"`
	TopP            *float64            `json:"top_p,omitempty"`
	Functions       json.RawMessage     `json:"functions,omitempty"`
}

// ChatCompletionMsg represents a message in the chat completion request
type ChatCompletionMsg struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCalls  []ToolCall      `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
}

// ChatTool represents a tool definition in OpenAI format
type ChatTool struct {
	Type     string       `json:"type"`
	Function ChatFunction `json:"function"`
}

// ChatFunction defines a function tool
type ChatFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall represents a tool call from the assistant
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall contains the function name and arguments
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ResponseFormat specifies the output format
type ResponseFormat struct {
	Type       string         `json:"type"`
	JSONSchema *JSONSchemaDef `json:"json_schema,omitempty"`
}

// JSONSchemaDef defines a JSON schema for structured output
type JSONSchemaDef struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
}

// --- Chat Completion Response Types ---

// ChatCompletionChunk is a streaming chunk in Chat Completions format
type ChatCompletionChunk struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []ChatChunkChoice `json:"choices"`
	Usage   *ChatUsage        `json:"usage,omitempty"`
}

// ChatChunkChoice is a choice in a streaming chunk
type ChatChunkChoice struct {
	Index        int            `json:"index"`
	Delta        ChatChunkDelta `json:"delta"`
	FinishReason *string        `json:"finish_reason"`
}

// ChatChunkDelta is the delta in a streaming chunk
type ChatChunkDelta struct {
	Role      string          `json:"role,omitempty"`
	Content   *string         `json:"content,omitempty"`
	ToolCalls []ChunkToolCall `json:"tool_calls,omitempty"`
}

// ChunkToolCall is a tool call delta in a streaming chunk
type ChunkToolCall struct {
	Index    int               `json:"index"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function ChunkFunctionCall `json:"function"`
}

// ChunkFunctionCall is the function call delta
type ChunkFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ChatCompletionResponse is a non-streaming response in Chat Completions format
type ChatCompletionResponse struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []ChatResponseChoice `json:"choices"`
	Usage   ChatUsage           `json:"usage"`
}

// ChatResponseChoice is a choice in a non-streaming response
type ChatResponseChoice struct {
	Index        int             `json:"index"`
	Message      ChatResponseMsg `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

// ChatResponseMsg is the message in a non-streaming response
type ChatResponseMsg struct {
	Role      string     `json:"role"`
	Content   *string    `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ChatUsage represents token usage in Chat Completions format
type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
