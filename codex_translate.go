package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ChatToResponses converts an OpenAI Chat Completions request to a Codex Responses API request.
func ChatToResponses(chatReq *ChatCompletionRequest) (*CodexResponsesRequest, error) {
	req := &CodexResponsesRequest{
		Model:  chatReq.Model,
		Input:  []CodexInputItem{},
		Stream: true,  // Always use streaming for Codex
		Store:  false,
	}

	// Extract system/developer messages as instructions
	var instructions []string
	for _, msg := range chatReq.Messages {
		if msg.Role == "system" || msg.Role == "developer" {
			text := extractTextContent(msg.Content)
			if text != "" {
				instructions = append(instructions, text)
			}
		}
	}
	if len(instructions) > 0 {
		req.Instructions = strings.Join(instructions, "\n\n")
	} else {
		req.Instructions = "You are a helpful assistant."
	}

	// Convert messages to input items
	for _, msg := range chatReq.Messages {
		if msg.Role == "system" || msg.Role == "developer" {
			continue
		}

		switch msg.Role {
		case "assistant":
			// Push text content first
			text := extractTextContent(msg.Content)
			if text != "" || len(msg.ToolCalls) == 0 {
				textJSON, _ := json.Marshal(text)
				req.Input = append(req.Input, CodexInputItem{
					Role:    "assistant",
					Content: textJSON,
				})
			}
			// Push tool calls as function_call items
			for _, tc := range msg.ToolCalls {
				req.Input = append(req.Input, CodexInputItem{
					Type:      "function_call",
					CallID:    tc.ID,
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				})
			}

		case "tool":
			output := extractTextContent(msg.Content)
			req.Input = append(req.Input, CodexInputItem{
				Type:   "function_call_output",
				CallID: msg.ToolCallID,
				Output: output,
			})

		case "function":
			// Legacy function role
			output := extractTextContent(msg.Content)
			callID := msg.Name
			if callID == "" {
				callID = "fc_unknown"
			}
			req.Input = append(req.Input, CodexInputItem{
				Type:   "function_call_output",
				CallID: "fc_" + callID,
				Output: output,
			})

		default: // "user"
			content := convertUserContent(msg.Content)
			req.Input = append(req.Input, CodexInputItem{
				Role:    "user",
				Content: content,
			})
		}
	}

	// Ensure at least one input message
	if len(req.Input) == 0 {
		emptyContent, _ := json.Marshal("")
		req.Input = append(req.Input, CodexInputItem{
			Role:    "user",
			Content: emptyContent,
		})
	}

	// Convert tools to Codex format
	if len(chatReq.Tools) > 0 {
		req.Tools = make([]json.RawMessage, 0, len(chatReq.Tools))
		for _, tool := range chatReq.Tools {
			if tool.Type == "function" {
				codexTool := map[string]interface{}{
					"type":        "function",
					"name":        tool.Function.Name,
					"description": tool.Function.Description,
				}
				if tool.Function.Parameters != nil {
					var params interface{}
					if json.Unmarshal(tool.Function.Parameters, &params) == nil {
						codexTool["parameters"] = params
					}
				}
				toolJSON, _ := json.Marshal(codexTool)
				req.Tools = append(req.Tools, toolJSON)
			}
		}
	}

	// Convert tool_choice
	if chatReq.ToolChoice != nil {
		req.ToolChoice = convertToolChoice(chatReq.ToolChoice)
	}

	// Reasoning effort
	if chatReq.ReasoningEffort != "" {
		req.Reasoning = &CodexReasoning{
			Effort:  chatReq.ReasoningEffort,
			Summary: "auto",
		}
	}

	// Response format → text.format
	if chatReq.ResponseFormat != nil && chatReq.ResponseFormat.Type != "text" {
		switch chatReq.ResponseFormat.Type {
		case "json_object":
			req.Text = &CodexTextConfig{
				Format: CodexTextFormat{Type: "json_object"},
			}
		case "json_schema":
			if chatReq.ResponseFormat.JSONSchema != nil {
				js := chatReq.ResponseFormat.JSONSchema
				req.Text = &CodexTextConfig{
					Format: CodexTextFormat{
						Type:   "json_schema",
						Name:   js.Name,
						Schema: js.Schema,
						Strict: &js.Strict,
					},
				}
			}
		}
	}

	return req, nil
}

// extractTextContent extracts plain text from a message content field.
// Content can be a string, array of parts, null, or undefined.
func extractTextContent(raw json.RawMessage) string {
	if raw == nil || string(raw) == "null" {
		return ""
	}

	// Try as string
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}

	// Try as array of content parts
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var builder strings.Builder
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				if builder.Len() > 0 {
					builder.WriteString("\n")
				}
				builder.WriteString(p.Text)
			}
		}
		return builder.String()
	}

	return ""
}

// convertUserContent converts message content to Codex format.
// Returns JSON-serialized string or array of CodexContentPart.
func convertUserContent(raw json.RawMessage) json.RawMessage {
	if raw == nil || string(raw) == "null" {
		content, _ := json.Marshal("")
		return content
	}

	// Check if it's an array (multimodal content)
	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL interface{} `json:"image_url"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		// Check for images
		hasImages := false
		for _, p := range parts {
			if p.Type == "image_url" {
				hasImages = true
				break
			}
		}

		if !hasImages {
			// Text-only array → extract text as plain string
			text := extractTextContent(raw)
			content, _ := json.Marshal(text)
			return content
		}

		// Multimodal → convert to Codex content parts
		var codexParts []CodexContentPart
		for _, p := range parts {
			if p.Type == "text" && p.Text != "" {
				codexParts = append(codexParts, CodexContentPart{
					Type: "input_text",
					Text: p.Text,
				})
			} else if p.Type == "image_url" && p.ImageURL != nil {
				var urlStr string
				switch v := p.ImageURL.(type) {
				case string:
					urlStr = v
				case map[string]interface{}:
					if u, ok := v["url"].(string); ok {
						urlStr = u
					}
				}
				if urlStr != "" {
					codexParts = append(codexParts, CodexContentPart{
						Type:     "input_image",
						ImageURL: urlStr,
					})
				}
			}
		}
		if len(codexParts) == 0 {
			content, _ := json.Marshal("")
			return content
		}
		content, _ := json.Marshal(codexParts)
		return content
	}

	// Already a string or other format, pass through
	return raw
}

// convertToolChoice converts OpenAI tool_choice to Codex format
func convertToolChoice(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}

	// String values: "auto", "none", "required" → pass through
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return raw
	}

	// Object: {"type": "function", "function": {"name": "xxx"}}
	var tc struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(raw, &tc) == nil && tc.Type == "function" {
		codexTC := map[string]string{
			"type": "function",
			"name": tc.Function.Name,
		}
		result, _ := json.Marshal(codexTC)
		return result
	}

	return raw
}

// generateChatID generates a unique chat completion ID
func generateChatID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "chatcmpl-" + hex.EncodeToString(b)
}

// formatSSEChunk formats a ChatCompletionChunk as an SSE data line
func formatSSEChunk(chunk *ChatCompletionChunk) string {
	data, _ := json.Marshal(chunk)
	return "data: " + string(data) + "\n\n"
}

// StreamResponsesToChat converts a Codex SSE stream to OpenAI Chat Completions SSE format.
// It writes directly to the http.ResponseWriter with flushing.
// Returns usage info and any error encountered.
func StreamResponsesToChat(
	src io.Reader,
	dst http.ResponseWriter,
	model string,
) (usage *CodexUsage, err error) {
	flusher, canFlush := dst.(http.Flusher)
	chunkID := generateChatID()
	created := time.Now().Unix()
	hasToolCalls := false
	hasContent := false

	// Track tool call indices by call_id
	toolCallIndexMap := make(map[string]int)
	nextToolCallIndex := 0
	callIdsWithDeltas := make(map[string]bool)

	// Send initial role chunk
	role := "assistant"
	initChunk := &ChatCompletionChunk{
		ID:      chunkID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   model,
		Choices: []ChatChunkChoice{{
			Index:        0,
			Delta:        ChatChunkDelta{Role: role},
			FinishReason: nil,
		}},
	}
	dst.Write([]byte(formatSSEChunk(initChunk)))
	if canFlush {
		flusher.Flush()
	}

	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line

	var currentData string
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data: ") {
			currentData = strings.TrimPrefix(line, "data: ")
			continue
		}

		// Empty line = end of SSE event
		if line == "" && currentData != "" {
			var evt CodexEventData
			if err := json.Unmarshal([]byte(currentData), &evt); err != nil {
				currentData = ""
				continue
			}
			currentData = ""

			// Handle error events
			if evt.Error != nil {
				errContent := fmt.Sprintf("[Error] %s: %s", evt.Error.Code, evt.Error.Message)
				errChunk := &ChatCompletionChunk{
					ID:      chunkID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   model,
					Choices: []ChatChunkChoice{{
						Index: 0,
						Delta: ChatChunkDelta{Content: &errContent},
					}},
				}
				dst.Write([]byte(formatSSEChunk(errChunk)))

				stopReason := "stop"
				stopChunk := &ChatCompletionChunk{
					ID:      chunkID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   model,
					Choices: []ChatChunkChoice{{
						Index:        0,
						Delta:        ChatChunkDelta{},
						FinishReason: &stopReason,
					}},
				}
				dst.Write([]byte(formatSSEChunk(stopChunk)))
				dst.Write([]byte("data: [DONE]\n\n"))
				if canFlush {
					flusher.Flush()
				}
				return nil, fmt.Errorf("codex stream error: %s: %s", evt.Error.Code, evt.Error.Message)
			}

			// Handle function_call start (from response.output_item.added with function_call type)
			if evt.Type == "response.output_item.added" && evt.Item != nil {
				var item struct {
					Type   string `json:"type"`
					CallID string `json:"call_id"`
					Name   string `json:"name"`
				}
				if json.Unmarshal(evt.Item, &item) == nil && item.Type == "function_call" {
					hasToolCalls = true
					hasContent = true
					idx := nextToolCallIndex
					nextToolCallIndex++
					toolCallIndexMap[item.CallID] = idx
					toolChunk := &ChatCompletionChunk{
						ID:      chunkID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   model,
						Choices: []ChatChunkChoice{{
							Index: 0,
							Delta: ChatChunkDelta{
								ToolCalls: []ChunkToolCall{{
									Index: idx,
									ID:    item.CallID,
									Type:  "function",
									Function: ChunkFunctionCall{
										Name:      item.Name,
										Arguments: "",
									},
								}},
							},
						}},
					}
					dst.Write([]byte(formatSSEChunk(toolChunk)))
					if canFlush {
						flusher.Flush()
					}
				}
				continue
			}

			// Handle function call arguments delta
			if evt.Type == "response.function_call_arguments.delta" && evt.Delta != "" {
				callIdsWithDeltas[evt.CallID] = true
				idx := toolCallIndexMap[evt.CallID]
				argChunk := &ChatCompletionChunk{
					ID:      chunkID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   model,
					Choices: []ChatChunkChoice{{
						Index: 0,
						Delta: ChatChunkDelta{
							ToolCalls: []ChunkToolCall{{
								Index: idx,
								Function: ChunkFunctionCall{
									Arguments: evt.Delta,
								},
							}},
						},
					}},
				}
				dst.Write([]byte(formatSSEChunk(argChunk)))
				if canFlush {
					flusher.Flush()
				}
				continue
			}

			// Handle function call arguments done
			if evt.Type == "response.function_call_arguments.done" {
				// If no deltas were streamed, emit full arguments
				if !callIdsWithDeltas[evt.CallID] {
					idx := toolCallIndexMap[evt.CallID]
					doneChunk := &ChatCompletionChunk{
						ID:      chunkID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   model,
						Choices: []ChatChunkChoice{{
							Index: 0,
							Delta: ChatChunkDelta{
								ToolCalls: []ChunkToolCall{{
									Index: idx,
									Function: ChunkFunctionCall{
										Arguments: evt.Delta,
									},
								}},
							},
						}},
					}
					dst.Write([]byte(formatSSEChunk(doneChunk)))
					if canFlush {
						flusher.Flush()
					}
				}
				continue
			}

			// Handle text delta
			if evt.Type == "response.output_text.delta" && evt.Delta != "" {
				hasContent = true
				textChunk := &ChatCompletionChunk{
					ID:      chunkID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   model,
					Choices: []ChatChunkChoice{{
						Index: 0,
						Delta: ChatChunkDelta{Content: &evt.Delta},
					}},
				}
				dst.Write([]byte(formatSSEChunk(textChunk)))
				if canFlush {
					flusher.Flush()
				}
				continue
			}

			// Handle response.completed
			if evt.Type == "response.completed" && evt.Response != nil {
				var resp CodexCompletedResponse
				if json.Unmarshal(evt.Response, &resp) == nil && resp.Usage != nil {
					usage = resp.Usage
				}

				// Inject error if no content was produced
				if !hasContent {
					errText := "[Error] Codex returned an empty response. Please retry."
					errChunk := &ChatCompletionChunk{
						ID:      chunkID,
						Object:  "chat.completion.chunk",
						Created: created,
						Model:   model,
						Choices: []ChatChunkChoice{{
							Index: 0,
							Delta: ChatChunkDelta{Content: &errText},
						}},
					}
					dst.Write([]byte(formatSSEChunk(errChunk)))
				}

				// Build final chunk with usage and finish reason
				finishReason := "stop"
				if hasToolCalls {
					finishReason = "tool_calls"
				}

				var chatUsage *ChatUsage
				if usage != nil {
					chatUsage = &ChatUsage{
						PromptTokens:     usage.InputTokens,
						CompletionTokens: usage.OutputTokens,
						TotalTokens:      usage.InputTokens + usage.OutputTokens,
					}
				}

				finalChunk := &ChatCompletionChunk{
					ID:      chunkID,
					Object:  "chat.completion.chunk",
					Created: created,
					Model:   model,
					Choices: []ChatChunkChoice{{
						Index:        0,
						Delta:        ChatChunkDelta{},
						FinishReason: &finishReason,
					}},
					Usage: chatUsage,
				}
				dst.Write([]byte(formatSSEChunk(finalChunk)))

				// Send [DONE] marker
				dst.Write([]byte("data: [DONE]\n\n"))
				if canFlush {
					flusher.Flush()
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return usage, fmt.Errorf("SSE read error: %w", err)
	}

	return usage, nil
}

// CollectResponsesToChat consumes a Codex SSE stream and builds a complete ChatCompletionResponse.
// Used for non-streaming requests.
func CollectResponsesToChat(
	src io.Reader,
	model string,
) (*ChatCompletionResponse, *CodexUsage, error) {
	id := generateChatID()
	created := time.Now().Unix()
	var fullText strings.Builder
	var usage *CodexUsage
	var toolCalls []ToolCall

	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var currentData string
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "data: ") {
			currentData = strings.TrimPrefix(line, "data: ")
			continue
		}

		if line == "" && currentData != "" {
			var evt CodexEventData
			if err := json.Unmarshal([]byte(currentData), &evt); err != nil {
				currentData = ""
				continue
			}
			currentData = ""

			// Handle error events
			if evt.Error != nil {
				return nil, nil, fmt.Errorf("codex stream error: %s: %s", evt.Error.Code, evt.Error.Message)
			}

			// Collect text deltas
			if evt.Type == "response.output_text.delta" && evt.Delta != "" {
				fullText.WriteString(evt.Delta)
			}

			// Collect function call completion
			if evt.Type == "response.function_call_arguments.done" {
				toolCalls = append(toolCalls, ToolCall{
					ID:   evt.CallID,
					Type: "function",
					Function: FunctionCall{
						Name:      evt.Name,
						Arguments: evt.Delta,
					},
				})
			}

			// Extract usage from response.completed
			if evt.Type == "response.completed" && evt.Response != nil {
				var resp CodexCompletedResponse
				if json.Unmarshal(evt.Response, &resp) == nil {
					usage = resp.Usage
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("SSE read error: %w", err)
	}

	// Build the response
	hasToolCalls := len(toolCalls) > 0
	finishReason := "stop"
	if hasToolCalls {
		finishReason = "tool_calls"
	}

	contentStr := fullText.String()
	message := ChatResponseMsg{
		Role:    "assistant",
		Content: &contentStr,
	}
	if hasToolCalls {
		message.ToolCalls = toolCalls
	}

	response := &ChatCompletionResponse{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: []ChatResponseChoice{{
			Index:        0,
			Message:      message,
			FinishReason: finishReason,
		}},
	}

	if usage != nil {
		response.Usage = ChatUsage{
			PromptTokens:     usage.InputTokens,
			CompletionTokens: usage.OutputTokens,
			TotalTokens:      usage.InputTokens + usage.OutputTokens,
		}
	} else {
		response.Usage = ChatUsage{}
	}

	return response, usage, nil
}
