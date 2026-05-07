package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type AnthropicCompatibleProvider struct {
	name           string
	baseURL        string
	apiKey         string
	headers        map[string]string
	model          string
	config         config.ProviderConfig
	httpClient     *http.Client
	decider        *proxy.Decider
	defaultOptions ChatOptions
}

type anthropicCompatibleRequest struct {
	Model       string                       `json:"model"`
	MaxTokens   int                          `json:"max_tokens"`
	System      string                       `json:"system,omitempty"`
	Messages    []anthropicCompatibleMessage `json:"messages"`
	Tools       []anthropicCompatibleTool    `json:"tools,omitempty"`
	Temperature *float64                     `json:"temperature,omitempty"`
	TopP        *float64                     `json:"top_p,omitempty"`
	Stream      bool                         `json:"stream,omitempty"`
	Thinking    map[string]interface{}       `json:"thinking,omitempty"`
	Metadata    map[string]string            `json:"metadata,omitempty"`
	StopSeqs    []string                     `json:"stop_sequences,omitempty"`
}

type anthropicCompatibleMessage struct {
	Role    string                            `json:"role"`
	Content []anthropicCompatibleContentBlock `json:"content"`
}

type anthropicCompatibleContentBlock struct {
	Type      string                          `json:"type"`
	Text      string                          `json:"text,omitempty"`
	Thinking  string                          `json:"thinking,omitempty"`
	Signature string                          `json:"signature,omitempty"`
	ID        string                          `json:"id,omitempty"`
	Name      string                          `json:"name,omitempty"`
	Input     map[string]interface{}          `json:"input,omitempty"`
	ToolUseID string                          `json:"tool_use_id,omitempty"`
	Content   any                             `json:"content,omitempty"`
	IsError   bool                            `json:"is_error,omitempty"`
	Source    *anthropicCompatibleImageSource `json:"source,omitempty"`
}

type anthropicCompatibleImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

type anthropicCompatibleTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
}

type anthropicCompatibleResponse struct {
	Content []anthropicCompatibleContentBlock `json:"content"`
}

type anthropicCompatibleModelListResponse struct {
	Data   []map[string]any `json:"data"`
	Models []map[string]any `json:"models"`
}

type anthropicCompatibleSSEPayload struct {
	Type         string                          `json:"type"`
	Index        int64                           `json:"index"`
	Delta        anthropicCompatibleSSEDelta     `json:"delta"`
	ContentBlock anthropicCompatibleContentBlock `json:"content_block"`
	Error        *anthropicCompatibleSSEError    `json:"error"`
}

type anthropicCompatibleSSEDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Thinking    string `json:"thinking"`
	PartialJSON string `json:"partial_json"`
	StopReason  string `json:"stop_reason"`
}

type anthropicCompatibleSSEError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func NewAnthropicCompatibleProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (Provider, error) {
	normalized, err := normalizeAnthropicCompatibleConfig(cfg)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Timeout: 5 * time.Minute}
	if decider != nil {
		httpClient = decider.ForLLM(name).HTTPClient
	}
	return &AnthropicCompatibleProvider{
		name:       name,
		baseURL:    normalized.BaseURL,
		apiKey:     strings.TrimSpace(normalized.APIKey),
		headers:    cloneStringMap(normalized.Headers),
		model:      normalized.DefaultModel,
		config:     normalized,
		httpClient: httpClient,
		decider:    decider,
	}, nil
}

func (p *AnthropicCompatibleProvider) Name() string {
	return p.name
}

func (p *AnthropicCompatibleProvider) SetDefaultOptions(opts ChatOptions) {
	p.defaultOptions = opts
}

func (p *AnthropicCompatibleProvider) resolveModel(opts ChatOptions) string {
	if opts.Model != "" {
		return opts.Model
	}
	if p.defaultOptions.Model != "" {
		return p.defaultOptions.Model
	}
	return p.model
}

func (p *AnthropicCompatibleProvider) mergeOptions(defaultOpts, overrideOpts ChatOptions) ChatOptions {
	result := defaultOpts
	if overrideOpts.Model != "" {
		result.Model = overrideOpts.Model
	}
	if overrideOpts.Temperature != nil {
		result.Temperature = overrideOpts.Temperature
	}
	if overrideOpts.MaxTokens > 0 {
		result.MaxTokens = overrideOpts.MaxTokens
	}
	if overrideOpts.TopP != nil {
		result.TopP = overrideOpts.TopP
	}
	return result
}

func (p *AnthropicCompatibleProvider) Chat(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error) {
	return p.ChatWithOptions(ctx, messages, tools, p.defaultOptions)
}

func (p *AnthropicCompatibleProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (*models.Response, error) {
	requestBody, err := p.buildRequest(messages, tools, p.mergeOptions(p.defaultOptions, opts), false)
	if err != nil {
		return nil, err
	}

	resp, err := p.doRequest(ctx, requestBody)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read anthropic-compatible body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("anthropic-compatible request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var envelope anthropicCompatibleResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode anthropic-compatible body: %w", err)
	}
	return convertAnthropicCompatibleResponse(envelope.Content), nil
}

func (p *AnthropicCompatibleProvider) ChatStream(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	requestBody, err := p.buildRequest(messages, tools, p.defaultOptions, true)
	if err != nil {
		return nil, err
	}

	resp, err := p.doRequest(ctx, requestBody)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("anthropic-compatible stream failed: status=%d", resp.StatusCode)
		}
		return nil, fmt.Errorf("anthropic-compatible stream failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	ch := make(chan models.StreamEvent, 100)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		if err := p.streamSSE(ctx, resp.Body, ch); err != nil {
			if ctx.Err() != nil {
				ch <- models.StreamEvent{Type: "cancelled", Done: true}
			} else {
				ch <- models.StreamEvent{Type: "error", Done: true, Error: err}
			}
		}
	}()
	return ch, nil
}

func (p *AnthropicCompatibleProvider) GetMaxContextTokens() int {
	model := strings.ToLower(p.resolveModel(p.defaultOptions))
	if strings.Contains(model, "claude-opus-4-7") || strings.Contains(model, "claude-opus-4-6") || strings.Contains(model, "claude-sonnet-4-6") {
		return 1000000
	}
	if strings.Contains(model, "claude") {
		return 200000
	}
	return 128000
}

func (p *AnthropicCompatibleProvider) BuiltinModels() []ProviderModel {
	models := builtinModelsFromConfig(p.config.DefaultModel, p.config.Models)
	for i := range models {
		models[i].SupportsTools = true
		models[i].SupportsStreaming = true
	}
	return models
}

func (p *AnthropicCompatibleProvider) FetchModels(ctx context.Context) ([]ProviderModel, error) {
	resp, err := p.doModelsRequest(ctx)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read anthropic-compatible models body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("anthropic-compatible models request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var envelope anthropicCompatibleModelListResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode anthropic-compatible models body: %w", err)
	}

	items := envelope.Data
	if len(items) == 0 {
		items = envelope.Models
	}
	result := make([]ProviderModel, 0, len(items))
	for _, item := range items {
		model := anthropicCompatibleProviderModelFromMap(item)
		if model.ID == "" {
			continue
		}
		result = append(result, model)
	}
	return result, nil
}

func (p *AnthropicCompatibleProvider) ListModels(ctx context.Context) ([]ProviderModel, error) {
	return listProviderModels(ctx, p)
}

func (p *AnthropicCompatibleProvider) buildRequest(messages []models.Message, tools []models.ToolDefinition, opts ChatOptions, stream bool) (anthropicCompatibleRequest, error) {
	system, convertedMessages, err := p.convertMessages(messages)
	if err != nil {
		return anthropicCompatibleRequest{}, err
	}
	request := anthropicCompatibleRequest{
		Model:     p.resolveModel(opts),
		MaxTokens: p.resolveMaxTokens(opts),
		System:    system,
		Messages:  convertedMessages,
		Stream:    stream,
	}
	if len(tools) > 0 {
		request.Tools = convertAnthropicCompatibleTools(tools)
	}
	if opts.Temperature != nil {
		request.Temperature = opts.Temperature
	}
	if opts.TopP != nil {
		request.TopP = opts.TopP
	}
	if len(p.config.Metadata) > 0 {
		request.Metadata = cloneStringMap(p.config.Metadata)
	}
	if len(p.config.Stop) > 0 {
		request.StopSeqs = append([]string(nil), p.config.Stop...)
	}
	if strings.Contains(strings.ToLower(request.Model), "claude") {
		request.Thinking = map[string]interface{}{"type": "enabled", "budget_tokens": 1024}
	}
	return request, nil
}

func (p *AnthropicCompatibleProvider) convertMessages(messages []models.Message) (string, []anthropicCompatibleMessage, error) {
	var systemParts []string
	var result []anthropicCompatibleMessage

	appendMessage := func(role string, blocks ...anthropicCompatibleContentBlock) {
		if len(blocks) == 0 {
			return
		}
		if len(result) > 0 && result[len(result)-1].Role == role {
			result[len(result)-1].Content = append(result[len(result)-1].Content, blocks...)
			return
		}
		result = append(result, anthropicCompatibleMessage{Role: role, Content: blocks})
	}

	for _, m := range messages {
		switch strings.TrimSpace(strings.ToLower(m.Role)) {
		case "system", "developer":
			if strings.TrimSpace(m.Content) != "" {
				systemParts = append(systemParts, m.Content)
			}
		case "user":
			blocks, err := anthropicCompatibleMessageBlocks(m)
			if err != nil {
				return "", nil, err
			}
			appendMessage("user", blocks...)
		case "assistant":
			var blocks []anthropicCompatibleContentBlock
			if strings.TrimSpace(m.Content) != "" {
				blocks = append(blocks, anthropicCompatibleTextBlocks(m.Content)...)
			}
			if metadata := m.ReasoningMetadata; reasoningProvider(metadata) == "anthropic_compatible" {
				thinking := strings.TrimSpace(m.ThinkingContent)
				signature := ""
				if metadata.AnthropicCompatible != nil {
					signature = metadata.AnthropicCompatible.Signature
				}
				if thinking != "" && strings.TrimSpace(signature) != "" {
					blocks = append(blocks, anthropicCompatibleContentBlock{Type: "thinking", Thinking: thinking, Signature: signature})
				}
			}
			for _, tc := range m.ToolCalls {
				input := map[string]interface{}{}
				if strings.TrimSpace(tc.Function.Arguments) != "" {
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
						return "", nil, fmt.Errorf("decode anthropic-compatible tool args: %w", err)
					}
				}
				blocks = append(blocks, anthropicCompatibleContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: input,
				})
			}
			appendMessage("assistant", blocks...)
		case "tool":
			images, err := collectProviderImageInputs(m)
			if err != nil {
				return "", nil, err
			}
			content := any(m.Content)
			if len(images) > 0 {
				content = buildToolResultTextParts(m.Content, images)
			}
			appendMessage("user", anthropicCompatibleContentBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   content,
				IsError:   strings.HasPrefix(m.Content, "Error:"),
			})
		default:
			appendMessage("user", anthropicCompatibleTextBlocks(m.Content)...)
		}
	}

	if len(result) == 0 {
		result = append(result, anthropicCompatibleMessage{Role: "user", Content: anthropicCompatibleTextBlocks(" ")})
	}
	return strings.Join(systemParts, "\n\n"), result, nil
}

func anthropicCompatibleTextBlocks(content string) []anthropicCompatibleContentBlock {
	return []anthropicCompatibleContentBlock{{Type: "text", Text: coalescePromptText(content)}}
}

func anthropicCompatibleMessageBlocks(message models.Message) ([]anthropicCompatibleContentBlock, error) {
	images, err := collectProviderImageInputs(message)
	if err != nil {
		return nil, err
	}
	blocks := make([]anthropicCompatibleContentBlock, 0, len(images)+1)
	if strings.TrimSpace(message.Content) != "" || len(images) == 0 {
		blocks = append(blocks, anthropicCompatibleContentBlock{Type: "text", Text: coalescePromptText(message.Content)})
	}
	for _, image := range images {
		blocks = append(blocks, anthropicCompatibleContentBlock{
			Type: "image",
			Source: &anthropicCompatibleImageSource{
				Type:      "base64",
				MediaType: image.MimeType,
				Data:      base64EncodeBytes(image.Data),
			},
		})
	}
	return blocks, nil
}

func convertAnthropicCompatibleTools(tools []models.ToolDefinition) []anthropicCompatibleTool {
	result := make([]anthropicCompatibleTool, 0, len(tools))
	for _, tool := range tools {
		schema := map[string]interface{}{"type": "object"}
		for key, value := range tool.Function.Parameters {
			schema[key] = value
		}
		result = append(result, anthropicCompatibleTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: schema,
		})
	}
	return result
}

func convertAnthropicCompatibleResponse(blocks []anthropicCompatibleContentBlock) *models.Response {
	result := &models.Response{}
	for _, block := range blocks {
		switch block.Type {
		case "text":
			result.Content += block.Text
		case "thinking":
			if block.Thinking != "" {
				if result.ReasoningContent != "" {
					result.ReasoningContent += "\n"
				}
				result.ReasoningContent += block.Thinking
				result.ReasoningMetadata = newAnthropicReasoningMetadata("anthropic_compatible", block.Signature)
			}
		case "tool_use":
			args, _ := json.Marshal(block.Input)
			result.ToolCalls = append(result.ToolCalls, models.ToolCall{
				ID:   block.ID,
				Type: "tool_use",
				Function: models.ToolCallFunction{
					Name:      block.Name,
					Arguments: strings.TrimSpace(string(args)),
				},
			})
		}
	}
	return result
}

func (p *AnthropicCompatibleProvider) doRequest(ctx context.Context, requestBody anthropicCompatibleRequest) (*http.Response, error) {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic-compatible request: %w", err)
	}

	url := p.baseURL + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create anthropic-compatible request: %w", err)
	}
	if requestBody.Stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	req.Header.Set("Content-Type", "application/json")
	return p.doHTTPRequest(req, requestBody.Stream, false)
}

func (p *AnthropicCompatibleProvider) doModelsRequest(ctx context.Context) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("create anthropic-compatible models request: %w", err)
	}
	return p.doHTTPRequest(req, false, true)
}

func (p *AnthropicCompatibleProvider) doHTTPRequest(req *http.Request, stream, models bool) (*http.Response, error) {
	if p.apiKey != "" {
		req.Header.Set("x-api-key", p.apiKey)
		if req.Header.Get("Authorization") == "" {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
	}
	if req.Header.Get("anthropic-version") == "" {
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	for key, value := range p.headers {
		if strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		switch {
		case models:
			return nil, fmt.Errorf("anthropic-compatible models request: %w", err)
		case stream:
			return nil, fmt.Errorf("anthropic-compatible stream: %w", err)
		default:
			return nil, fmt.Errorf("anthropic-compatible request: %w", err)
		}
	}
	return resp, nil
}

func (p *AnthropicCompatibleProvider) streamSSE(ctx context.Context, body io.Reader, ch chan<- models.StreamEvent) error {
	reader := bufio.NewReader(body)
	builders := map[int64]*anthropicBlockBuilder{}
	messageStopSent := false

	for {
		// Check for context cancellation before blocking read
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		event, err := readOpenAICompatibleSSEEvent(reader)
		if err != nil {
			if err == io.EOF {
				if !messageStopSent {
					ch <- models.StreamEvent{Type: "stop", Done: true}
				}
				return nil
			}
			return fmt.Errorf("read anthropic-compatible sse: %w", err)
		}
		if event.Data == "" || event.Data == "[DONE]" {
			continue
		}

		var payload anthropicCompatibleSSEPayload
		if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
			return fmt.Errorf("decode anthropic-compatible sse: %w", err)
		}
		eventType := event.Event
		if eventType == "" {
			eventType = payload.Type
		}

		switch eventType {
		case "content_block_start":
			builder := &anthropicBlockBuilder{}
			switch payload.ContentBlock.Type {
			case "text":
				builder.kind = "text"
			case "thinking":
				builder.kind = "thinking"
				builder.signature = payload.ContentBlock.Signature
			case "tool_use":
				builder.kind = "tool_use"
				builder.id = payload.ContentBlock.ID
				builder.name = payload.ContentBlock.Name
				if len(payload.ContentBlock.Input) > 0 {
					encoded, _ := json.Marshal(payload.ContentBlock.Input)
					builder.inputJSON.Write(encoded)
				}
			}
			builders[payload.Index] = builder
		case "content_block_delta":
			builder := builders[payload.Index]
			if builder == nil {
				builder = &anthropicBlockBuilder{}
				builders[payload.Index] = builder
			}
			switch payload.Delta.Type {
			case "text_delta":
				builder.text.WriteString(payload.Delta.Text)
				if payload.Delta.Text != "" {
					ch <- models.StreamEvent{Type: "content", Content: payload.Delta.Text}
				}
			case "thinking_delta":
				builder.thinking.WriteString(payload.Delta.Thinking)
				if payload.Delta.Thinking != "" {
					ch <- models.StreamEvent{Type: "thinking", ThinkingContent: payload.Delta.Thinking, ReasoningMetadata: newAnthropicReasoningMetadata("anthropic_compatible", builder.signature)}
				}
			case "input_json_delta":
				builder.inputJSON.WriteString(payload.Delta.PartialJSON)
			}
		case "content_block_stop":
			builder := builders[payload.Index]
			if builder != nil && builder.kind == "tool_use" {
				ch <- models.StreamEvent{Type: "tool_calls", ToolCalls: []models.ToolCall{{
					ID:   builder.id,
					Type: "tool_use",
					Function: models.ToolCallFunction{
						Name:      builder.name,
						Arguments: strings.TrimSpace(builder.inputJSON.String()),
					},
				}}}
			}
		case "message_delta":
			if payload.Delta.StopReason != "" && payload.Delta.StopReason != "tool_use" {
				messageStopSent = true
				ch <- models.StreamEvent{Type: "stop", Done: true}
				return nil
			}
		case "message_stop":
			messageStopSent = true
			ch <- models.StreamEvent{Type: "stop", Done: true}
			return nil
		case "error":
			if payload.Error != nil && payload.Error.Message != "" {
				return fmt.Errorf("anthropic-compatible stream error: %s", payload.Error.Message)
			}
			return fmt.Errorf("anthropic-compatible stream error")
		}
	}
}

func (p *AnthropicCompatibleProvider) resolveMaxTokens(opts ChatOptions) int {
	if opts.MaxTokens > 0 {
		return opts.MaxTokens
	}
	if p.config.MaxCompletionTokens > 0 {
		return p.config.MaxCompletionTokens
	}
	return 16000
}

func anthropicCompatibleProviderModelFromMap(item map[string]any) ProviderModel {
	id := firstNonEmptyString(
		stringFromAny(item["id"]),
		stringFromAny(item["name"]),
		stringFromAny(item["model"]),
	)
	name := firstNonEmptyString(stringFromAny(item["display_name"]), stringFromAny(item["name"]), id)
	lowerID := strings.ToLower(id)

	model := ProviderModel{
		ID:                strings.TrimSpace(id),
		Name:              strings.TrimSpace(name),
		ContextWindow:     firstPositiveInt(item["context_window"], item["contextWindow"], item["max_context_tokens"], item["input_token_limit"]),
		MaxTokens:         firstPositiveInt(item["max_tokens"], item["max_output_tokens"], item["output_token_limit"]),
		SupportsTools:     firstBool(item["supports_tools"], item["tool_use"], item["tool_calling"]),
		SupportsStreaming: firstBool(item["supports_streaming"], item["streaming"]),
		SupportsVision:    firstBool(item["supports_vision"], item["vision"]),
		Reasoning:         firstBool(item["reasoning"], item["supports_reasoning"]),
	}
	if model.ContextWindow == 0 {
		if strings.Contains(lowerID, "claude-opus-4-7") || strings.Contains(lowerID, "claude-opus-4-6") || strings.Contains(lowerID, "claude-sonnet-4-6") {
			model.ContextWindow = 1000000
		} else if strings.Contains(lowerID, "claude") {
			model.ContextWindow = 200000
		} else {
			model.ContextWindow = 128000
		}
	}
	if !model.SupportsStreaming {
		model.SupportsStreaming = true
	}
	if strings.Contains(lowerID, "claude") && !model.SupportsTools {
		model.SupportsTools = true
	}
	if strings.Contains(lowerID, "claude") && !model.Reasoning {
		model.Reasoning = true
	}
	capabilities, _ := item["capabilities"].(map[string]any)
	if len(capabilities) > 0 {
		if !model.SupportsTools {
			model.SupportsTools = firstBool(capabilities["tools"], capabilities["tool_use"], capabilities["tool_calling"])
		}
		if !model.SupportsStreaming {
			model.SupportsStreaming = firstBool(capabilities["streaming"])
		}
		if !model.SupportsVision {
			model.SupportsVision = firstBool(capabilities["vision"])
		}
		if !model.Reasoning {
			model.Reasoning = firstBool(capabilities["reasoning"])
		}
	}
	return model
}

var _ Provider = (*AnthropicCompatibleProvider)(nil)
