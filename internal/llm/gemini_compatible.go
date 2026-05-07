package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type GeminiCompatibleProvider struct {
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

type geminiCompatibleRequest struct {
	Model             string                     `json:"-"`
	SystemInstruction *geminiCompatibleContent   `json:"system_instruction,omitempty"`
	Contents          []geminiCompatibleContent  `json:"contents"`
	Tools             []geminiCompatibleToolWrap `json:"tools,omitempty"`
	ToolConfig        map[string]interface{}     `json:"tool_config,omitempty"`
	GenerationConfig  map[string]interface{}     `json:"generation_config,omitempty"`
}

type geminiCompatibleContent struct {
	Role  string                 `json:"role,omitempty"`
	Parts []geminiCompatiblePart `json:"parts"`
}

type geminiCompatiblePart struct {
	Text             string                            `json:"text,omitempty"`
	Thought          bool                              `json:"thought,omitempty"`
	ThoughtSignature string                            `json:"thoughtSignature,omitempty"`
	InlineData       *geminiCompatibleBlob             `json:"inlineData,omitempty"`
	FunctionCall     *geminiCompatibleFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiCompatibleFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiCompatibleBlob struct {
	Data     string `json:"data,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
}

type geminiCompatibleFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
	ID   string                 `json:"id,omitempty"`
}

type geminiCompatibleFunctionResponse struct {
	Name     string                                 `json:"name"`
	Response any                                    `json:"response,omitempty"`
	Parts    []geminiCompatibleFunctionResponsePart `json:"parts,omitempty"`
	ID       string                                 `json:"id,omitempty"`
}

type geminiCompatibleFunctionResponsePart struct {
	InlineData *geminiCompatibleBlob `json:"inlineData,omitempty"`
}

type geminiCompatibleToolWrap struct {
	FunctionDeclarations []geminiCompatibleFunctionDeclaration `json:"functionDeclarations,omitempty"`
}

type geminiCompatibleFunctionDeclaration struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type geminiCompatibleResponse struct {
	Candidates []geminiCompatibleCandidate `json:"candidates"`
}

type geminiCompatibleCandidate struct {
	Content *geminiCompatibleContent `json:"content"`
}

type geminiCompatibleModelListResponse struct {
	Models        []map[string]any `json:"models"`
	Data          []map[string]any `json:"data"`
	NextPageToken string           `json:"nextPageToken"`
}

func NewGeminiCompatibleProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (Provider, error) {
	normalized, err := normalizeGeminiCompatibleConfig(cfg)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Timeout: 5 * time.Minute}
	if decider != nil {
		httpClient = decider.ForLLM(name).HTTPClient
	}
	return &GeminiCompatibleProvider{
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

func (p *GeminiCompatibleProvider) Name() string {
	return p.name
}

func (p *GeminiCompatibleProvider) SetDefaultOptions(opts ChatOptions) {
	p.defaultOptions = opts
}

func (p *GeminiCompatibleProvider) resolveModel(opts ChatOptions) string {
	if opts.Model != "" {
		return opts.Model
	}
	if p.defaultOptions.Model != "" {
		return p.defaultOptions.Model
	}
	return p.model
}

func (p *GeminiCompatibleProvider) mergeOptions(defaultOpts, overrideOpts ChatOptions) ChatOptions {
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

func (p *GeminiCompatibleProvider) Chat(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error) {
	return p.ChatWithOptions(ctx, messages, tools, p.defaultOptions)
}

func (p *GeminiCompatibleProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (*models.Response, error) {
	requestBody, err := p.buildRequest(messages, tools, p.mergeOptions(p.defaultOptions, opts))
	if err != nil {
		return nil, err
	}

	resp, err := p.doRequest(ctx, requestBody, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read gemini-compatible body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gemini-compatible request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var envelope geminiCompatibleResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode gemini-compatible body: %w", err)
	}
	return p.responseFromGenerateContent(&envelope)
}

func (p *GeminiCompatibleProvider) ChatStream(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	requestBody, err := p.buildRequest(messages, tools, p.defaultOptions)
	if err != nil {
		return nil, err
	}

	resp, err := p.doRequest(ctx, requestBody, true)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("gemini-compatible stream failed: status=%d", resp.StatusCode)
		}
		return nil, fmt.Errorf("gemini-compatible stream failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
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

func (p *GeminiCompatibleProvider) GetMaxContextTokens() int {
	model := strings.ToLower(p.resolveModel(p.defaultOptions))
	if strings.Contains(model, "gemini-2.5-pro") || strings.Contains(model, "gemini-2.5-flash") {
		return 1000000
	}
	if strings.Contains(model, "gemini") {
		return 128000
	}
	return 128000
}

func (p *GeminiCompatibleProvider) BuiltinModels() []ProviderModel {
	models := builtinModelsFromConfig(p.config.DefaultModel, p.config.Models)
	for i := range models {
		models[i].SupportsTools = true
		models[i].SupportsStreaming = true
	}
	return models
}

func (p *GeminiCompatibleProvider) FetchModels(ctx context.Context) ([]ProviderModel, error) {
	result := make([]ProviderModel, 0)
	nextPageToken := ""
	for {
		resp, err := p.doModelsRequest(ctx, nextPageToken)
		if err != nil {
			return nil, err
		}

		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read gemini-compatible models body: %w", readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("gemini-compatible models request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var envelope geminiCompatibleModelListResponse
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, fmt.Errorf("decode gemini-compatible models body: %w", err)
		}

		items := envelope.Models
		if len(items) == 0 {
			items = envelope.Data
		}
		for _, item := range items {
			model := geminiCompatibleProviderModelFromMap(item)
			if model.ID == "" {
				continue
			}
			result = append(result, model)
		}
		if strings.TrimSpace(envelope.NextPageToken) == "" {
			break
		}
		nextPageToken = strings.TrimSpace(envelope.NextPageToken)
	}
	return result, nil
}

func (p *GeminiCompatibleProvider) ListModels(ctx context.Context) ([]ProviderModel, error) {
	return listProviderModels(ctx, p)
}

func (p *GeminiCompatibleProvider) buildRequest(messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (geminiCompatibleRequest, error) {
	system, contents, err := p.convertMessages(messages)
	if err != nil {
		return geminiCompatibleRequest{}, err
	}
	request := geminiCompatibleRequest{Model: p.resolveModel(opts), Contents: contents}
	if system != "" {
		request.SystemInstruction = &geminiCompatibleContent{Parts: []geminiCompatiblePart{{Text: system}}}
	}
	if len(tools) > 0 {
		request.Tools = []geminiCompatibleToolWrap{{FunctionDeclarations: convertGeminiCompatibleTools(tools)}}
		request.ToolConfig = map[string]interface{}{
			"functionCallingConfig": map[string]interface{}{"mode": "AUTO"},
		}
	}
	generationConfig := map[string]interface{}{}
	if opts.Temperature != nil {
		generationConfig["temperature"] = *opts.Temperature
	}
	if opts.TopP != nil {
		generationConfig["topP"] = *opts.TopP
	}
	if opts.MaxTokens > 0 {
		generationConfig["maxOutputTokens"] = opts.MaxTokens
	}
	if len(generationConfig) > 0 {
		request.GenerationConfig = generationConfig
	}
	return request, nil
}

func (p *GeminiCompatibleProvider) convertMessages(messages []models.Message) (string, []geminiCompatibleContent, error) {
	var systemParts []string
	var result []geminiCompatibleContent

	appendContent := func(role string, parts ...geminiCompatiblePart) {
		if len(parts) == 0 {
			return
		}
		if len(result) > 0 && result[len(result)-1].Role == role {
			result[len(result)-1].Parts = append(result[len(result)-1].Parts, parts...)
			return
		}
		result = append(result, geminiCompatibleContent{Role: role, Parts: parts})
	}

	for _, m := range messages {
		switch strings.TrimSpace(strings.ToLower(m.Role)) {
		case "system", "developer":
			if strings.TrimSpace(m.Content) != "" {
				systemParts = append(systemParts, m.Content)
			}
		case "user":
			parts, err := geminiCompatibleMessageParts(m)
			if err != nil {
				return "", nil, err
			}
			appendContent("user", parts...)
		case "assistant":
			var parts []geminiCompatiblePart
			if strings.TrimSpace(m.Content) != "" {
				parts = append(parts, geminiCompatibleTextParts(m.Content)...)
			}
			if metadata := m.ReasoningMetadata; reasoningProvider(metadata) == "gemini_compatible" {
				thinking := strings.TrimSpace(m.ThinkingContent)
				signature := ""
				if metadata.GeminiCompatible != nil {
					signature = metadata.GeminiCompatible.ThoughtSignature
				}
				if thinking != "" {
					parts = append(parts, geminiCompatiblePart{Text: thinking, Thought: true, ThoughtSignature: signature})
				}
			}
			for _, tc := range m.ToolCalls {
				args := map[string]interface{}{}
				if strings.TrimSpace(tc.Function.Arguments) != "" {
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
						return "", nil, fmt.Errorf("decode gemini-compatible tool args: %w", err)
					}
				}
				parts = append(parts, geminiCompatiblePart{FunctionCall: &geminiCompatibleFunctionCall{Name: tc.Function.Name, Args: args, ID: tc.ID}})
			}
			appendContent("model", parts...)
		case "tool":
			images, err := collectProviderImageInputs(m)
			if err != nil {
				return "", nil, err
			}
			response := map[string]any{"output": m.Content}
			if strings.HasPrefix(m.Content, "Error:") {
				response = map[string]any{"error": strings.TrimSpace(strings.TrimPrefix(m.Content, "Error:"))}
			}
			parts := make([]geminiCompatibleFunctionResponsePart, 0, len(images))
			for _, image := range images {
				parts = append(parts, geminiCompatibleFunctionResponsePart{
					InlineData: &geminiCompatibleBlob{Data: base64EncodeBytes(image.Data), MIMEType: image.MimeType},
				})
			}
			appendContent("user", geminiCompatiblePart{FunctionResponse: &geminiCompatibleFunctionResponse{
				Name:     p.toolNameForToolResponse(result, m.ToolCallID),
				Response: response,
				Parts:    parts,
				ID:       m.ToolCallID,
			}})
		default:
			appendContent("user", geminiCompatibleTextParts(m.Content)...)
		}
	}

	if len(result) == 0 {
		result = append(result, geminiCompatibleContent{Role: "user", Parts: geminiCompatibleTextParts(" ")})
	}
	return strings.Join(systemParts, "\n\n"), result, nil
}

func geminiCompatibleTextParts(content string) []geminiCompatiblePart {
	return []geminiCompatiblePart{{Text: coalescePromptText(content)}}
}

func geminiCompatibleMessageParts(message models.Message) ([]geminiCompatiblePart, error) {
	images, err := collectProviderImageInputs(message)
	if err != nil {
		return nil, err
	}
	parts := make([]geminiCompatiblePart, 0, len(images)+1)
	if strings.TrimSpace(message.Content) != "" || len(images) == 0 {
		parts = append(parts, geminiCompatiblePart{Text: coalescePromptText(message.Content)})
	}
	for _, image := range images {
		parts = append(parts, geminiCompatiblePart{InlineData: &geminiCompatibleBlob{Data: base64EncodeBytes(image.Data), MIMEType: image.MimeType}})
	}
	return parts, nil
}

func convertGeminiCompatibleTools(tools []models.ToolDefinition) []geminiCompatibleFunctionDeclaration {
	result := make([]geminiCompatibleFunctionDeclaration, 0, len(tools))
	for _, tool := range tools {
		result = append(result, geminiCompatibleFunctionDeclaration{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
		})
	}
	return result
}

func (p *GeminiCompatibleProvider) responseFromGenerateContent(resp *geminiCompatibleResponse) (*models.Response, error) {
	result := &models.Response{}
	if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return result, nil
	}
	for _, part := range resp.Candidates[0].Content.Parts {
		if part.Text != "" {
			if part.Thought {
				if result.ReasoningContent != "" {
					result.ReasoningContent += "\n"
				}
				result.ReasoningContent += part.Text
				result.ReasoningMetadata = newGeminiReasoningMetadata("gemini_compatible", part.ThoughtSignature)
			} else {
				result.Content += part.Text
			}
		}
		if part.FunctionCall != nil {
			args, err := json.Marshal(part.FunctionCall.Args)
			if err != nil {
				return nil, fmt.Errorf("encode gemini-compatible tool args: %w", err)
			}
			result.ToolCalls = append(result.ToolCalls, models.ToolCall{
				ID:   part.FunctionCall.ID,
				Type: "function",
				Function: models.ToolCallFunction{
					Name:      part.FunctionCall.Name,
					Arguments: strings.TrimSpace(string(args)),
				},
			})
		}
	}
	return result, nil
}

func (p *GeminiCompatibleProvider) toolNameForToolResponse(contents []geminiCompatibleContent, toolCallID string) string {
	for i := len(contents) - 1; i >= 0; i-- {
		for _, part := range contents[i].Parts {
			if part.FunctionCall != nil && part.FunctionCall.ID == toolCallID {
				return part.FunctionCall.Name
			}
		}
	}
	return "tool"
}

func (p *GeminiCompatibleProvider) doRequest(ctx context.Context, requestBody geminiCompatibleRequest, stream bool) (*http.Response, error) {
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal gemini-compatible request: %w", err)
	}

	model := url.PathEscape(requestBody.Model)
	method := ":generateContent"
	if stream {
		method = ":streamGenerateContent?alt=sse"
	}
	requestURL := strings.TrimRight(p.baseURL, "/") + "/models/" + model + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create gemini-compatible request: %w", err)
	}
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	req.Header.Set("Content-Type", "application/json")
	return p.doHTTPRequest(req, stream, false)
}

func (p *GeminiCompatibleProvider) doModelsRequest(ctx context.Context, pageToken string) (*http.Response, error) {
	requestURL := strings.TrimRight(p.baseURL, "/") + "/models"
	if strings.TrimSpace(pageToken) != "" {
		requestURL += "?pageToken=" + url.QueryEscape(strings.TrimSpace(pageToken))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create gemini-compatible models request: %w", err)
	}
	return p.doHTTPRequest(req, false, true)
}

func (p *GeminiCompatibleProvider) doHTTPRequest(req *http.Request, stream, models bool) (*http.Response, error) {
	if p.apiKey != "" {
		req.Header.Set("x-goog-api-key", p.apiKey)
		if req.Header.Get("Authorization") == "" {
			req.Header.Set("Authorization", "Bearer "+p.apiKey)
		}
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
			return nil, fmt.Errorf("gemini-compatible models request: %w", err)
		case stream:
			return nil, fmt.Errorf("gemini-compatible stream: %w", err)
		default:
			return nil, fmt.Errorf("gemini-compatible request: %w", err)
		}
	}
	return resp, nil
}

func (p *GeminiCompatibleProvider) streamSSE(ctx context.Context, body io.Reader, ch chan<- models.StreamEvent) error {
	reader := bufio.NewReader(body)
	toolCallBuilders := map[string]*models.ToolCall{}
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
				break
			}
			return fmt.Errorf("read gemini-compatible sse: %w", err)
		}
		if event.Data == "" || event.Data == "[DONE]" {
			continue
		}

		var envelope geminiCompatibleResponse
		if err := json.Unmarshal([]byte(event.Data), &envelope); err != nil {
			return fmt.Errorf("decode gemini-compatible sse: %w", err)
		}
		if len(envelope.Candidates) == 0 || envelope.Candidates[0].Content == nil {
			continue
		}
		for _, part := range envelope.Candidates[0].Content.Parts {
			if part.Text != "" {
				if part.Thought {
					ch <- models.StreamEvent{Type: "thinking", ThinkingContent: part.Text, ReasoningMetadata: newGeminiReasoningMetadata("gemini_compatible", part.ThoughtSignature)}
				} else {
					ch <- models.StreamEvent{Type: "content", Content: part.Text}
				}
			}
			if part.FunctionCall != nil {
				args, err := json.Marshal(part.FunctionCall.Args)
				if err != nil {
					return fmt.Errorf("encode gemini-compatible tool args: %w", err)
				}
				key := part.FunctionCall.ID
				if key == "" {
					key = part.FunctionCall.Name
				}
				toolCallBuilders[key] = &models.ToolCall{
					ID:   part.FunctionCall.ID,
					Type: "function",
					Function: models.ToolCallFunction{
						Name:      part.FunctionCall.Name,
						Arguments: strings.TrimSpace(string(args)),
					},
				}
			}
		}
	}

	if len(toolCallBuilders) > 0 {
		toolCalls := make([]models.ToolCall, 0, len(toolCallBuilders))
		for _, tc := range toolCallBuilders {
			toolCalls = append(toolCalls, *tc)
		}
		ch <- models.StreamEvent{Type: "tool_calls", ToolCalls: toolCalls}
	}
	ch <- models.StreamEvent{Type: "stop", Done: true}
	return nil
}

func geminiCompatibleProviderModelFromMap(item map[string]any) ProviderModel {
	id := firstNonEmptyString(
		strings.TrimPrefix(stringFromAny(item["name"]), "models/"),
		stringFromAny(item["id"]),
		stringFromAny(item["model"]),
	)
	name := firstNonEmptyString(stringFromAny(item["displayName"]), stringFromAny(item["display_name"]), id)
	lowerID := strings.ToLower(id)

	model := ProviderModel{
		ID:                strings.TrimSpace(id),
		Name:              strings.TrimSpace(name),
		ContextWindow:     firstPositiveInt(item["inputTokenLimit"], item["context_window"], item["contextWindow"], item["max_context_tokens"]),
		MaxTokens:         firstPositiveInt(item["outputTokenLimit"], item["maxOutputTokens"], item["max_tokens"], item["max_output_tokens"]),
		SupportsTools:     firstBool(item["supportsTools"], item["supports_tools"], item["functionCalling"]),
		SupportsStreaming: firstBool(item["supportsStreaming"], item["supports_streaming"], item["streaming"]),
		SupportsVision:    firstBool(item["supportsVision"], item["supports_vision"], item["vision"]),
		Reasoning:         firstBool(item["reasoning"], item["supportsReasoning"]),
	}
	if model.ContextWindow == 0 {
		if strings.Contains(lowerID, "gemini-2.5-pro") || strings.Contains(lowerID, "gemini-2.5-flash") {
			model.ContextWindow = 1000000
		} else {
			model.ContextWindow = 128000
		}
	}
	if !model.SupportsStreaming {
		model.SupportsStreaming = true
	}
	if strings.Contains(lowerID, "gemini") && !model.SupportsTools {
		model.SupportsTools = true
	}
	if strings.Contains(lowerID, "vision") || strings.Contains(lowerID, "gemini-1.5") || strings.Contains(lowerID, "gemini-2") {
		model.SupportsVision = true
	}
	if strings.Contains(lowerID, "thinking") || strings.Contains(lowerID, "gemini-2.5") {
		model.Reasoning = true
	}
	capabilities, _ := item["capabilities"].(map[string]any)
	if len(capabilities) > 0 {
		if !model.SupportsTools {
			model.SupportsTools = firstBool(capabilities["tools"], capabilities["functionCalling"], capabilities["function_calling"])
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

var _ Provider = (*GeminiCompatibleProvider)(nil)
