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

type OpenAIResponsesCompatibleProvider struct {
	name           string
	baseURL        string
	apiKey         string
	headers        map[string]string
	model          string
	httpClient     *http.Client
	decider        *proxy.Decider
	defaultOptions ChatOptions
}

type responsesRequest struct {
	Model           string               `json:"model"`
	Input           []responsesInputItem `json:"input"`
	Tools           []responsesTool      `json:"tools,omitempty"`
	Temperature     *float64             `json:"temperature,omitempty"`
	TopP            *float64             `json:"top_p,omitempty"`
	MaxOutputTokens int                  `json:"max_output_tokens,omitempty"`
	Reasoning       map[string]any       `json:"reasoning,omitempty"`
	Stream          bool                 `json:"stream,omitempty"`
}

type responsesInputItem struct {
	Type      string         `json:"type,omitempty"`
	Role      string         `json:"role,omitempty"`
	Content   any            `json:"content,omitempty"`
	CallID    string         `json:"call_id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Arguments string         `json:"arguments,omitempty"`
	Output    any            `json:"output,omitempty"`
	Status    string         `json:"status,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type responsesTextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type responsesEnvelope struct {
	Output []responsesOutputItem `json:"output"`
}

type responsesOutputItem struct {
	ID               string                   `json:"id"`
	Type             string                   `json:"type"`
	Role             string                   `json:"role"`
	Status           string                   `json:"status"`
	Content          []responsesOutputContent `json:"content"`
	CallID           string                   `json:"call_id"`
	Name             string                   `json:"name"`
	Arguments        string                   `json:"arguments"`
	Output           any                      `json:"output"`
	Summary          []responsesOutputContent `json:"summary"`
	EncryptedContent string                   `json:"encrypted_content"`
}

type responsesOutputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesSSEPayload struct {
	Type         string               `json:"type"`
	Delta        string               `json:"delta"`
	ItemID       string               `json:"item_id"`
	OutputIndex  int                  `json:"output_index"`
	ContentIndex int                  `json:"content_index"`
	CallID       string               `json:"call_id"`
	Name         string               `json:"name"`
	Arguments    string               `json:"arguments"`
	Item         *responsesOutputItem `json:"item"`
	Error        *responsesError      `json:"error"`
}

type responsesError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

type responsesSSEEvent struct {
	Event string
	Data  string
}

type responsesToolCallBuilder struct {
	id        string
	type_     string
	name      string
	arguments strings.Builder
}

type responsesReasoningBuilder struct {
	itemID      string
	outputIndex int
	status      string
	delta       strings.Builder
	summary     strings.Builder
	content     strings.Builder
	encrypted   string
	flushed     bool
}

func NewOpenAIResponsesCompatibleProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (Provider, error) {
	normalized, err := normalizeOpenAICompatibleConfig(cfg)
	if err != nil {
		return nil, err
	}
	httpClient := &http.Client{Timeout: 5 * time.Minute}
	if decider != nil {
		httpClient = decider.ForLLM(name).HTTPClient
	}
	return &OpenAIResponsesCompatibleProvider{
		name:       name,
		baseURL:    normalized.BaseURL,
		apiKey:     strings.TrimSpace(normalized.APIKey),
		headers:    cloneStringMap(normalized.Headers),
		model:      normalized.DefaultModel,
		httpClient: httpClient,
		decider:    decider,
	}, nil
}

func (p *OpenAIResponsesCompatibleProvider) Name() string {
	return p.name
}

func (p *OpenAIResponsesCompatibleProvider) SetDefaultOptions(opts ChatOptions) {
	p.defaultOptions = opts
}

func (p *OpenAIResponsesCompatibleProvider) resolveModel(opts ChatOptions) string {
	if opts.Model != "" {
		return opts.Model
	}
	if p.defaultOptions.Model != "" {
		return p.defaultOptions.Model
	}
	return p.model
}

func (p *OpenAIResponsesCompatibleProvider) mergeOptions(defaultOpts, overrideOpts ChatOptions) ChatOptions {
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

func (p *OpenAIResponsesCompatibleProvider) Chat(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error) {
	return p.ChatWithOptions(ctx, messages, tools, p.defaultOptions)
}

func (p *OpenAIResponsesCompatibleProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (*models.Response, error) {
	mergedOpts := p.mergeOptions(p.defaultOptions, opts)
	requestBody, err := p.buildRequest(messages, tools, mergedOpts)
	if err != nil {
		return nil, err
	}

	resp, err := p.doResponsesRequest(ctx, requestBody, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read responses body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai responses request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var envelope responsesEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode responses body: %w", err)
	}

	return convertResponsesOutput(envelope.Output), nil
}

func (p *OpenAIResponsesCompatibleProvider) ChatStream(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	requestBody, err := p.buildRequest(messages, tools, p.defaultOptions)
	if err != nil {
		return nil, err
	}

	resp, err := p.doResponsesRequest(ctx, requestBody, true)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("openai responses stream failed: status=%d", resp.StatusCode)
		}
		return nil, fmt.Errorf("openai responses stream failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	ch := make(chan models.StreamEvent, 100)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		if err := p.streamResponsesSSE(ctx, resp.Body, ch); err != nil {
			if ctx.Err() != nil {
				ch <- models.StreamEvent{Type: "cancelled", Done: true}
			} else {
				ch <- models.StreamEvent{Type: "error", Done: true, Error: err}
			}
		}
	}()
	return ch, nil
}

func (p *OpenAIResponsesCompatibleProvider) GetMaxContextTokens() int {
	return getOpenAICompatibleMaxContextTokens(strings.ToLower(p.resolveModel(p.defaultOptions)))
}

func (p *OpenAIResponsesCompatibleProvider) BuiltinModels() []ProviderModel {
	models := builtinModelsFromConfig(p.model, nil)
	for i := range models {
		models[i].SupportsTools = true
		models[i].SupportsStreaming = true
	}
	return models
}

func (p *OpenAIResponsesCompatibleProvider) FetchModels(ctx context.Context) ([]ProviderModel, error) {
	_ = ctx
	return nil, nil
}

func (p *OpenAIResponsesCompatibleProvider) ListModels(ctx context.Context) ([]ProviderModel, error) {
	return listProviderModels(ctx, p)
}

func (p *OpenAIResponsesCompatibleProvider) buildRequest(messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (responsesRequest, error) {
	input, err := buildResponsesInput(messages)
	if err != nil {
		return responsesRequest{}, err
	}

	request := responsesRequest{
		Model: p.resolveModel(opts),
		Input: input,
	}
	if len(tools) > 0 {
		request.Tools = buildResponsesTools(tools)
	}
	if opts.Temperature != nil {
		request.Temperature = opts.Temperature
	}
	if opts.TopP != nil {
		request.TopP = opts.TopP
	}
	if opts.MaxTokens > 0 {
		request.MaxOutputTokens = opts.MaxTokens
	}
	return request, nil
}

func (p *OpenAIResponsesCompatibleProvider) doResponsesRequest(ctx context.Context, requestBody responsesRequest, stream bool) (*http.Response, error) {
	requestBody.Stream = stream
	payload, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal responses request: %w", err)
	}

	url := p.baseURL + "/responses"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create responses request: %w", err)
	}
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	for key, value := range p.headers {
		if strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		if stream {
			return nil, fmt.Errorf("openai responses stream: %w", err)
		}
		return nil, fmt.Errorf("openai responses request: %w", err)
	}
	return resp, nil
}

func (p *OpenAIResponsesCompatibleProvider) streamResponsesSSE(ctx context.Context, body io.Reader, ch chan<- models.StreamEvent) error {
	reader := bufio.NewReader(body)
	toolBuilders := make(map[string]*responsesToolCallBuilder)
	toolOrder := make([]string, 0)
	reasoningBuilders := make(map[string]*responsesReasoningBuilder)
	reasoningOrder := make([]string, 0)

	for {
		// Check for context cancellation before blocking read
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		event, err := readResponsesSSEEvent(reader)
		if err != nil {
			if err == io.EOF {
				flushResponsesReasoning(ch, reasoningBuilders, reasoningOrder)
				if toolCalls := buildResponsesToolCalls(toolBuilders, toolOrder); len(toolCalls) > 0 {
					ch <- models.StreamEvent{Type: "tool_calls", ToolCalls: toolCalls}
				}
				return nil
			}
			return fmt.Errorf("read responses sse: %w", err)
		}
		if event.Data == "" || event.Data == "[DONE]" {
			continue
		}

		var payload responsesSSEPayload
		if err := json.Unmarshal([]byte(event.Data), &payload); err != nil {
			return fmt.Errorf("decode responses sse event: %w", err)
		}

		eventType := event.Event
		if eventType == "" {
			eventType = payload.Type
		}

		switch eventType {
		case "response.output_text.delta":
			if payload.Delta != "" {
				ch <- models.StreamEvent{Type: "content", Content: payload.Delta}
			}
		case "response.function_call_arguments.delta":
			builder, _ := upsertResponsesToolCallBuilder(toolBuilders, &toolOrder, payload)
			if payload.Delta != "" {
				builder.arguments.WriteString(payload.Delta)
			}
		case "response.function_call_arguments.done":
			builder, _ := upsertResponsesToolCallBuilder(toolBuilders, &toolOrder, payload)
			if payload.Arguments != "" {
				builder.arguments.Reset()
				builder.arguments.WriteString(payload.Arguments)
			}
		case "response.output_item.added", "response.output_item_added", "response.output_item.done":
			if payload.Item != nil {
				switch payload.Item.Type {
				case "function_call":
					builder, _ := upsertResponsesToolCallBuilder(toolBuilders, &toolOrder, payload)
					if payload.Item.CallID != "" {
						builder.id = payload.Item.CallID
					}
					if payload.Item.Name != "" {
						builder.name = payload.Item.Name
					}
					if payload.Item.Arguments != "" {
						builder.arguments.Reset()
						builder.arguments.WriteString(payload.Item.Arguments)
					}
				case "reasoning":
					builder, _ := upsertResponsesReasoningBuilder(reasoningBuilders, &reasoningOrder, payload)
					if payload.Item.Status != "" {
						builder.status = payload.Item.Status
					}
					if payload.Item.EncryptedContent != "" {
						builder.encrypted = payload.Item.EncryptedContent
					}
					appendResponsesReasoningSummary(builder, payload.Item.Summary)
					appendResponsesReasoningContent(builder, payload.Item.Content)
					if eventType == "response.output_item.done" {
						flushSingleResponsesReasoning(ch, builder)
					}
				}
			}
		case "response.completed":
			flushResponsesReasoning(ch, reasoningBuilders, reasoningOrder)
			toolCalls := buildResponsesToolCalls(toolBuilders, toolOrder)
			if len(toolCalls) > 0 {
				ch <- models.StreamEvent{Type: "tool_calls", ToolCalls: toolCalls}
				return nil
			}
			ch <- models.StreamEvent{Type: "stop", Done: true}
			return nil
		case "response.failed", "error":
			return buildResponsesStreamError(eventType, payload)
		default:
			if strings.Contains(eventType, "reasoning") && strings.HasSuffix(eventType, ".delta") && payload.Delta != "" {
				builder, _ := upsertResponsesReasoningBuilder(reasoningBuilders, &reasoningOrder, payload)
				builder.delta.WriteString(payload.Delta)
				ch <- models.StreamEvent{Type: "thinking", ThinkingContent: payload.Delta, ReasoningMetadata: buildResponsesReasoningMetadata(builder)}
			}
		}
	}
}

func readResponsesSSEEvent(reader *bufio.Reader) (responsesSSEEvent, error) {
	var event responsesSSEEvent
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				line = strings.TrimRight(line, "\r\n")
				if line == "" {
					if event.Event == "" && event.Data == "" {
						return responsesSSEEvent{}, io.EOF
					}
					return event, nil
				}
			} else {
				return responsesSSEEvent{}, err
			}
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if event.Event == "" && event.Data == "" {
				if err == io.EOF {
					return responsesSSEEvent{}, io.EOF
				}
				continue
			}
			return event, nil
		}
		if strings.HasPrefix(line, ":") {
			if err == io.EOF {
				return event, nil
			}
			continue
		}
		if strings.HasPrefix(line, "event:") {
			event.Event = trimSSEFieldValue(line[len("event:"):])
		} else if strings.HasPrefix(line, "data:") {
			if event.Data != "" {
				event.Data += "\n"
			}
			event.Data += trimSSEFieldValue(line[len("data:"):])
		}

		if err == io.EOF {
			return event, nil
		}
	}
}

func upsertResponsesToolCallBuilder(builders map[string]*responsesToolCallBuilder, order *[]string, payload responsesSSEPayload) (*responsesToolCallBuilder, string) {
	key := responsesToolCallKey(payload)
	builder := builders[key]
	if builder == nil {
		builder = &responsesToolCallBuilder{type_: "function"}
		builders[key] = builder
		*order = append(*order, key)
	}
	if payload.CallID != "" {
		builder.id = payload.CallID
	}
	if payload.Name != "" {
		builder.name = payload.Name
	}
	if payload.Arguments != "" && builder.arguments.Len() == 0 {
		builder.arguments.WriteString(payload.Arguments)
	}
	if payload.Item != nil {
		if payload.Item.CallID != "" {
			builder.id = payload.Item.CallID
		}
		if payload.Item.Name != "" {
			builder.name = payload.Item.Name
		}
		if payload.Item.Arguments != "" && builder.arguments.Len() == 0 {
			builder.arguments.WriteString(payload.Item.Arguments)
		}
	}
	return builder, key
}

func responsesToolCallKey(payload responsesSSEPayload) string {
	if payload.ItemID != "" {
		return "item:" + payload.ItemID
	}
	if payload.Item != nil && payload.Item.ID != "" {
		return "item:" + payload.Item.ID
	}
	if payload.CallID != "" {
		return "call:" + payload.CallID
	}
	if payload.Item != nil && payload.Item.CallID != "" {
		return "call:" + payload.Item.CallID
	}
	return fmt.Sprintf("index:%d", payload.OutputIndex)
}

func upsertResponsesReasoningBuilder(builders map[string]*responsesReasoningBuilder, order *[]string, payload responsesSSEPayload) (*responsesReasoningBuilder, string) {
	key := responsesReasoningKey(payload)
	builder := builders[key]
	if builder == nil {
		builder = &responsesReasoningBuilder{itemID: payload.ItemID, outputIndex: payload.OutputIndex}
		builders[key] = builder
		*order = append(*order, key)
	}
	if builder.itemID == "" && payload.Item != nil {
		builder.itemID = payload.Item.ID
	}
	if payload.Item != nil && payload.Item.Status != "" {
		builder.status = payload.Item.Status
	}
	if payload.Item != nil && payload.Item.EncryptedContent != "" {
		builder.encrypted = payload.Item.EncryptedContent
	}
	return builder, key
}

func responsesReasoningKey(payload responsesSSEPayload) string {
	if payload.ItemID != "" {
		return "item:" + payload.ItemID
	}
	if payload.Item != nil && payload.Item.ID != "" {
		return "item:" + payload.Item.ID
	}
	return fmt.Sprintf("index:%d", payload.OutputIndex)
}

func appendResponsesReasoningSummary(builder *responsesReasoningBuilder, summary []responsesOutputContent) {
	for _, part := range summary {
		text := strings.TrimSpace(part.Text)
		if text == "" {
			continue
		}
		current := builder.summary.String()
		if current != "" && !strings.Contains(current, text) {
			builder.summary.WriteString("\n")
		}
		if !strings.Contains(current, text) {
			builder.summary.WriteString(text)
		}
	}
}

func appendResponsesReasoningContent(builder *responsesReasoningBuilder, content []responsesOutputContent) {
	for _, part := range content {
		text := strings.TrimSpace(part.Text)
		if text == "" {
			continue
		}
		currentContent := builder.content.String()
		if currentContent != "" && !strings.Contains(currentContent, text) {
			builder.content.WriteString("\n")
		}
		if !strings.Contains(currentContent, text) {
			builder.content.WriteString(text)
		}
		current := builder.summary.String()
		if current != "" && !strings.Contains(current, text) {
			builder.summary.WriteString("\n")
		}
		if !strings.Contains(current, text) {
			builder.summary.WriteString(text)
		}
	}
}

func flushSingleResponsesReasoning(ch chan<- models.StreamEvent, builder *responsesReasoningBuilder) {
	if builder == nil || builder.flushed {
		return
	}
	text := strings.TrimSpace(builder.summary.String())
	if text == "" {
		text = strings.TrimSpace(builder.delta.String())
	}
	if text == "" {
		return
	}
	builder.flushed = true
	ch <- models.StreamEvent{Type: "thinking", ThinkingContent: text, ReasoningMetadata: buildResponsesReasoningMetadata(builder)}
}

func flushResponsesReasoning(ch chan<- models.StreamEvent, builders map[string]*responsesReasoningBuilder, order []string) {
	for _, key := range order {
		flushSingleResponsesReasoning(ch, builders[key])
	}
}

func buildResponsesToolCalls(builders map[string]*responsesToolCallBuilder, order []string) []models.ToolCall {
	result := make([]models.ToolCall, 0, len(order))
	for _, key := range order {
		builder := builders[key]
		if builder == nil || builder.name == "" {
			continue
		}
		result = append(result, models.ToolCall{
			ID:   builder.id,
			Type: builder.type_,
			Function: models.ToolCallFunction{
				Name:      builder.name,
				Arguments: builder.arguments.String(),
			},
		})
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func buildResponsesStreamError(eventType string, payload responsesSSEPayload) error {
	if payload.Error != nil {
		if payload.Error.Message != "" {
			return fmt.Errorf("%s: %s", eventType, payload.Error.Message)
		}
		if payload.Error.Type != "" || payload.Error.Code != "" {
			return fmt.Errorf("%s: type=%s code=%s", eventType, payload.Error.Type, payload.Error.Code)
		}
	}
	return fmt.Errorf("%s", eventType)
}

func buildResponsesInput(messages []models.Message) ([]responsesInputItem, error) {
	items := make([]responsesInputItem, 0, len(messages))
	for _, m := range messages {
		if shouldSkipSyntheticToolMessage(m) {
			continue
		}
		role := strings.TrimSpace(strings.ToLower(m.Role))
		switch role {
		case "system", "user", "assistant", "developer":
			if reasoningItem := buildResponsesReasoningItem(m); reasoningItem != nil {
				items = append(items, *reasoningItem)
			}
			images, err := collectProviderImageInputs(m)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(m.Content) == "" && len(m.ToolCalls) == 0 && len(images) == 0 {
				continue
			}
			item := responsesInputItem{Role: role}
			if len(images) > 0 {
				item.Content = buildResponsesVisionContent(m.Content, images)
			} else {
				item.Content = []responsesTextContent{{
					Type: "input_text",
					Text: coalescePromptText(m.Content),
				}}
			}
			if role == "assistant" && len(m.ToolCalls) > 0 {
				if strings.TrimSpace(m.Content) != "" {
					items = append(items, item)
				}
				for _, tc := range m.ToolCalls {
					items = append(items, responsesInputItem{
						Type:      "function_call",
						CallID:    tc.ID,
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
						Status:    "completed",
					})
				}
				continue
			}
			items = append(items, item)
		case "tool":
			if m.ToolCallID == "" {
				return nil, fmt.Errorf("tool message missing tool_call_id")
			}
			images, err := collectProviderImageInputs(m)
			if err != nil {
				return nil, err
			}
			output := any(m.Content)
			if len(images) > 0 {
				output = buildResponsesVisionContent(m.Content, images)
			}
			items = append(items, responsesInputItem{
				Type:   "function_call_output",
				CallID: m.ToolCallID,
				Output: output,
			})
		default:
			if strings.TrimSpace(m.Content) == "" {
				continue
			}
			items = append(items, responsesInputItem{
				Role: role,
				Content: []responsesTextContent{{
					Type: "input_text",
					Text: m.Content,
				}},
			})
		}
	}
	return items, nil
}

func buildResponsesTools(tools []models.ToolDefinition) []responsesTool {
	result := make([]responsesTool, 0, len(tools))
	for _, tool := range tools {
		result = append(result, responsesTool{
			Type:        "function",
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
		})
	}
	return result
}

func convertResponsesOutput(items []responsesOutputItem) *models.Response {
	result := &models.Response{}
	for _, item := range items {
		switch item.Type {
		case "message":
			for _, content := range item.Content {
				if strings.TrimSpace(content.Text) == "" {
					continue
				}
				if result.Content != "" {
					result.Content += "\n"
				}
				result.Content += content.Text
			}
		case "reasoning":
			result.ReasoningMetadata = mergeResponsesReasoningMetadata(result.ReasoningMetadata, buildResponsesReasoningMetadataFromItem(item))
			for _, summary := range item.Summary {
				if strings.TrimSpace(summary.Text) == "" {
					continue
				}
				if result.ReasoningContent != "" {
					result.ReasoningContent += "\n"
				}
				result.ReasoningContent += summary.Text
			}
		case "function_call":
			result.ToolCalls = append(result.ToolCalls, models.ToolCall{
				ID:   item.CallID,
				Type: "function",
				Function: models.ToolCallFunction{
					Name:      item.Name,
					Arguments: item.Arguments,
				},
			})
		}
	}
	return result
}

func buildResponsesReasoningItem(msg models.Message) *responsesInputItem {
	metadata := msg.ReasoningMetadata
	if metadata == nil || metadata.OpenAIResponses == nil || reasoningProvider(metadata) != "openai_responses" {
		return nil
	}
	replay := metadata.OpenAIResponses
	id := strings.TrimSpace(replay.ItemID)
	if id == "" {
		return nil
	}
	item := &responsesInputItem{Type: "reasoning", Name: id}
	item.Status = replay.Status
	content := buildResponsesReasoningPayload(replay)
	if len(content) > 0 {
		item.Content = content
	}
	item.Metadata = map[string]any{"id": id}
	if encrypted := strings.TrimSpace(replay.EncryptedContent); encrypted != "" {
		item.Metadata["encrypted_content"] = encrypted
	}
	return item
}

func buildResponsesReasoningPayload(replay *models.OpenAIResponsesReasoningReplay) []responsesTextContent {
	if replay == nil {
		return nil
	}
	if len(replay.Summary) > 0 {
		summary := append([]string(nil), replay.Summary...)
		parts := make([]responsesTextContent, 0, len(summary))
		for _, text := range summary {
			parts = append(parts, responsesTextContent{Type: "summary_text", Text: text})
		}
		return parts
	}
	if len(replay.Content) > 0 {
		content := append([]string(nil), replay.Content...)
		parts := make([]responsesTextContent, 0, len(content))
		for _, text := range content {
			parts = append(parts, responsesTextContent{Type: "reasoning_text", Text: text})
		}
		return parts
	}
	if len(replay.Summary) == 1 && strings.TrimSpace(replay.Summary[0]) != "" {
		text := strings.TrimSpace(replay.Summary[0])
		return []responsesTextContent{{Type: "summary_text", Text: text}}
	}
	return nil
}

func buildResponsesReasoningMetadata(builder *responsesReasoningBuilder) *models.ReasoningReplay {
	metadata := &models.OpenAIResponsesReasoningReplay{ItemID: builder.itemID}
	if status := strings.TrimSpace(builder.status); status != "" {
		metadata.Status = status
	}
	summary := strings.TrimSpace(builder.summary.String())
	if summary != "" {
		metadata.Summary = strings.Split(summary, "\n")
	}
	content := strings.TrimSpace(builder.content.String())
	if content != "" {
		metadata.Content = strings.Split(content, "\n")
	}
	if encrypted := strings.TrimSpace(builder.encrypted); encrypted != "" {
		metadata.EncryptedContent = encrypted
	}
	return &models.ReasoningReplay{Provider: "openai_responses", OpenAIResponses: metadata}
}

func buildResponsesReasoningMetadataFromItem(item responsesOutputItem) *models.ReasoningReplay {
	builder := &responsesReasoningBuilder{itemID: item.ID, status: item.Status, encrypted: item.EncryptedContent}
	appendResponsesReasoningSummary(builder, item.Summary)
	appendResponsesReasoningContent(builder, item.Content)
	return buildResponsesReasoningMetadata(builder)
}

func mergeResponsesReasoningMetadata(base, incoming *models.ReasoningReplay) *models.ReasoningReplay {
	if incoming == nil || !incoming.HasData() {
		return base
	}
	return cloneReasoningMetadata(incoming)
}

var _ Provider = (*OpenAIResponsesCompatibleProvider)(nil)
