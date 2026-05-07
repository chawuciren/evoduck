package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type AnthropicProvider struct {
	name           string
	client         anthropic.Client
	model          string
	decider        *proxy.Decider
	defaultOptions ChatOptions
}

func (p *AnthropicProvider) resolveModel(opts ChatOptions) string {
	if opts.Model != "" {
		return opts.Model
	}
	if p.defaultOptions.Model != "" {
		return p.defaultOptions.Model
	}
	return p.model
}

type anthropicBlockBuilder struct {
	kind      string
	id        string
	name      string
	signature string
	text      strings.Builder
	thinking  strings.Builder
	inputJSON strings.Builder
}

func NewAnthropicProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (*AnthropicProvider, error) {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if decider != nil {
		httpClient := decider.ForLLM(name).HTTPClient
		opts = append(opts, option.WithHTTPClient(httpClient))
	}

	return &AnthropicProvider{
		name:    name,
		client:  anthropic.NewClient(opts...),
		model:   cfg.DefaultModel,
		decider: decider,
	}, nil
}

func (p *AnthropicProvider) Name() string {
	return p.name
}

func (p *AnthropicProvider) SetDefaultOptions(opts ChatOptions) {
	p.defaultOptions = opts
}

func (p *AnthropicProvider) Chat(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error) {
	return p.chatWithMergedOptions(ctx, messages, tools, p.defaultOptions)
}

func (p *AnthropicProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (*models.Response, error) {
	return p.chatWithMergedOptions(ctx, messages, tools, p.mergeOptions(p.defaultOptions, opts))
}

func (p *AnthropicProvider) chatWithMergedOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (*models.Response, error) {
	log.Debug("Anthropic chat request started", logger.Fields{
		"provider":       p.name,
		"model":          p.model,
		"messages_count": len(messages),
		"tools_count":    len(tools),
	})

	req, err := p.buildRequest(messages, tools, opts)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Messages.New(ctx, req)
	if err != nil {
		log.Error("Anthropic chat failed", logger.Fields{"error": err.Error()})
		return nil, fmt.Errorf("anthropic messages: %w", err)
	}

	result := p.responseFromMessage(resp)
	return result, nil
}

func (p *AnthropicProvider) ChatStream(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	log.Debug("Anthropic stream request started", logger.Fields{
		"provider":       p.name,
		"model":          p.model,
		"messages_count": len(messages),
		"tools_count":    len(tools),
	})

	req, err := p.buildRequest(messages, tools, p.defaultOptions)
	if err != nil {
		return nil, err
	}

	stream := p.client.Messages.NewStreaming(ctx, req)
	ch := make(chan models.StreamEvent, 100)

	go func() {
		defer close(ch)

		builders := map[int64]*anthropicBlockBuilder{}
		messageStopSent := false

		for stream.Next() {
			// Check for context cancellation
			select {
			case <-ctx.Done():
				ch <- models.StreamEvent{Type: "cancelled", Done: true}
				return
			default:
			}

			event := stream.Current()

			switch v := event.AsAny().(type) {
			case anthropic.ContentBlockStartEvent:
				builders[v.Index] = p.newBlockBuilder(v)
			case anthropic.ContentBlockDeltaEvent:
				builder := builders[v.Index]
				if builder == nil {
					builder = &anthropicBlockBuilder{}
					builders[v.Index] = builder
				}
				p.applyDelta(builder, v, ch)
			case anthropic.ContentBlockStopEvent:
				builder := builders[v.Index]
				if builder == nil {
					continue
				}
				if builder.kind == "tool_use" {
					toolCall := models.ToolCall{
						ID:   builder.id,
						Type: "tool_use",
						Function: models.ToolCallFunction{
							Name:      builder.name,
							Arguments: strings.TrimSpace(builder.inputJSON.String()),
						},
					}
					ch <- models.StreamEvent{Type: "tool_calls", ToolCalls: []models.ToolCall{toolCall}}
				}
			case anthropic.MessageDeltaEvent:
				if v.Delta.StopReason != "" && v.Delta.StopReason != anthropic.StopReasonToolUse {
					messageStopSent = true
					ch <- models.StreamEvent{Type: "stop", Done: true}
					return
				}
			case anthropic.MessageStopEvent:
				if !messageStopSent {
					ch <- models.StreamEvent{Type: "stop", Done: true}
				}
				return
			}
		}

		if err := stream.Err(); err != nil {
			if ctx.Err() != nil {
				ch <- models.StreamEvent{Type: "cancelled", Done: true}
			} else {
				log.Error("Anthropic stream failed", logger.Fields{"error": err.Error()})
				ch <- models.StreamEvent{Type: "error", Done: true, Error: err}
			}
			return
		}

		if !messageStopSent {
			ch <- models.StreamEvent{Type: "stop", Done: true}
		}
	}()

	return ch, nil
}

func (p *AnthropicProvider) buildRequest(messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (anthropic.MessageNewParams, error) {
	system, anthropicMessages, err := p.convertMessages(messages)
	if err != nil {
		return anthropic.MessageNewParams{}, err
	}

	resolvedModel := p.resolveModel(opts)
	req := anthropic.MessageNewParams{
		Model:     anthropic.Model(resolvedModel),
		Messages:  anthropicMessages,
		MaxTokens: int64(p.resolveMaxTokens(opts)),
	}

	if len(system) > 0 {
		req.System = []anthropic.TextBlockParam{{Text: system}}
	}
	if len(tools) > 0 {
		req.Tools = p.convertTools(tools)
	}
	p.applyOptions(&req, opts)
	return req, nil
}

func (p *AnthropicProvider) convertMessages(messages []models.Message) (string, []anthropic.MessageParam, error) {
	var systemParts []string
	var result []anthropic.MessageParam

	appendRole := func(role anthropic.MessageParamRole, blocks ...anthropic.ContentBlockParamUnion) {
		if len(blocks) == 0 {
			return
		}
		if len(result) > 0 && result[len(result)-1].Role == role {
			result[len(result)-1].Content = append(result[len(result)-1].Content, blocks...)
			return
		}
		if role == anthropic.MessageParamRoleAssistant {
			result = append(result, anthropic.NewAssistantMessage(blocks...))
			return
		}
		result = append(result, anthropic.NewUserMessage(blocks...))
	}

	for _, m := range messages {
		switch m.Role {
		case "system":
			if strings.TrimSpace(m.Content) != "" {
				systemParts = append(systemParts, m.Content)
			}
		case "user":
			blocks, err := p.messageBlocksFromMessage(m)
			if err != nil {
				return "", nil, err
			}
			appendRole(anthropic.MessageParamRoleUser, blocks...)
		case "assistant":
			var blocks []anthropic.ContentBlockParamUnion
			if strings.TrimSpace(m.Content) != "" {
				blocks = append(blocks, p.messageBlocksFromText(m.Content)...)
			}
			if metadata := m.ReasoningMetadata; reasoningProvider(metadata) == "anthropic" {
				thinking := strings.TrimSpace(m.ThinkingContent)
				signature := ""
				if metadata.Anthropic != nil {
					signature = metadata.Anthropic.Signature
				}
				if thinking != "" && strings.TrimSpace(signature) != "" {
					blocks = append(blocks, anthropic.NewThinkingBlock(signature, thinking))
				}
			}
			for _, tc := range m.ToolCalls {
				input := map[string]any{}
				if strings.TrimSpace(tc.Function.Arguments) != "" {
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
						return "", nil, fmt.Errorf("decode anthropic tool call args: %w", err)
					}
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, tc.Function.Name))
			}
			appendRole(anthropic.MessageParamRoleAssistant, blocks...)
		case "tool":
			blocks, err := p.toolResultBlocksFromMessage(m)
			if err != nil {
				return "", nil, err
			}
			appendRole(anthropic.MessageParamRoleUser, blocks...)
		default:
			blocks := p.messageBlocksFromText(m.Content)
			appendRole(anthropic.MessageParamRoleUser, blocks...)
		}
	}

	if len(result) == 0 {
		result = append(result, anthropic.NewUserMessage(anthropic.NewTextBlock(" ")))
	}

	return strings.Join(systemParts, "\n\n"), result, nil
}

func (p *AnthropicProvider) convertTools(tools []models.ToolDefinition) []anthropic.ToolUnionParam {
	result := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		schema := anthropic.ToolInputSchemaParam{Type: "object"}
		if props, ok := t.Function.Parameters["properties"]; ok {
			schema.Properties = props
		}
		if required, ok := t.Function.Parameters["required"].([]interface{}); ok {
			for _, item := range required {
				if s, ok := item.(string); ok {
					schema.Required = append(schema.Required, s)
				}
			}
		}
		if required, ok := t.Function.Parameters["required"].([]string); ok {
			schema.Required = append(schema.Required, required...)
		}
		if len(t.Function.Parameters) > 0 {
			schema.ExtraFields = map[string]any{}
			for k, v := range t.Function.Parameters {
				if k == "type" || k == "properties" || k == "required" {
					continue
				}
				schema.ExtraFields[k] = v
			}
		}

		tool := anthropic.ToolParam{
			Name:        t.Function.Name,
			Description: anthropic.String(t.Function.Description),
			InputSchema: schema,
			Type:        anthropic.ToolTypeCustom,
		}
		result = append(result, anthropic.ToolUnionParam{OfTool: &tool})
	}
	return result
}

func (p *AnthropicProvider) responseFromMessage(resp *anthropic.Message) *models.Response {
	result := &models.Response{}
	for _, block := range resp.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			result.Content += b.Text
		case anthropic.ThinkingBlock:
			if result.ReasoningContent != "" {
				result.ReasoningContent += "\n"
			}
			result.ReasoningContent += b.Thinking
			result.ReasoningMetadata = newAnthropicReasoningMetadata("anthropic", b.Signature)
		case anthropic.ToolUseBlock:
			result.ToolCalls = append(result.ToolCalls, models.ToolCall{
				ID:   b.ID,
				Type: "tool_use",
				Function: models.ToolCallFunction{
					Name:      b.Name,
					Arguments: strings.TrimSpace(string(b.Input)),
				},
			})
		}
	}
	return result
}

func (p *AnthropicProvider) applyOptions(req *anthropic.MessageNewParams, opts ChatOptions) {
	if opts.Temperature != nil {
		req.Temperature = anthropic.Float(*opts.Temperature)
		log.Debug("Anthropic option applied", logger.Fields{"temperature": *opts.Temperature})
	}
	if opts.TopP != nil {
		req.TopP = anthropic.Float(*opts.TopP)
		log.Debug("Anthropic option applied", logger.Fields{"top_p": *opts.TopP})
	}
	if strings.Contains(strings.ToLower(p.resolveModel(opts)), "claude") {
		adaptive := anthropic.ThinkingConfigAdaptiveParam{Type: "adaptive"}
		req.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive}
	}
}

func (p *AnthropicProvider) mergeOptions(defaultOpts, overrideOpts ChatOptions) ChatOptions {
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

func (p *AnthropicProvider) resolveMaxTokens(opts ChatOptions) int {
	if opts.MaxTokens > 0 {
		return opts.MaxTokens
	}
	return 16000
}

func (p *AnthropicProvider) GetMaxContextTokens() int {
	model := strings.ToLower(p.resolveModel(p.defaultOptions))
	if strings.Contains(model, "claude-opus-4-7") || strings.Contains(model, "claude-opus-4-6") || strings.Contains(model, "claude-sonnet-4-6") {
		return 1000000
	}
	if strings.Contains(model, "claude") {
		return 200000
	}
	return 128000
}

func (p *AnthropicProvider) BuiltinModels() []ProviderModel {
	models := builtinModelsFromConfig(p.model, nil)
	for i := range models {
		models[i].SupportsTools = true
		models[i].SupportsStreaming = true
		models[i].ContextWindow = p.GetMaxContextTokens()
	}
	return models
}

func (p *AnthropicProvider) FetchModels(ctx context.Context) ([]ProviderModel, error) {
	pager := p.client.Models.ListAutoPaging(ctx, anthropic.ModelListParams{})
	result := make([]ProviderModel, 0)
	for pager.Next() {
		result = append(result, anthropicProviderModelFromSDK(pager.Current()))
	}
	if err := pager.Err(); err != nil {
		return nil, fmt.Errorf("anthropic list models: %w", err)
	}
	return result, nil
}

func (p *AnthropicProvider) ListModels(ctx context.Context) ([]ProviderModel, error) {
	return listProviderModels(ctx, p)
}

func anthropicProviderModelFromSDK(model anthropic.ModelInfo) ProviderModel {
	id := strings.TrimSpace(model.ID)
	lowerID := strings.ToLower(id)
	result := ProviderModel{
		ID:                id,
		Name:              strings.TrimSpace(model.DisplayName),
		ContextWindow:     int(model.MaxInputTokens),
		MaxTokens:         int(model.MaxTokens),
		SupportsTools:     strings.Contains(lowerID, "claude"),
		SupportsStreaming: true,
		SupportsVision:    model.Capabilities.ImageInput.Supported || model.Capabilities.PDFInput.Supported,
		Reasoning:         model.Capabilities.Thinking.Supported || model.Capabilities.Effort.Supported,
	}
	if result.Name == "" {
		result.Name = id
	}
	if result.ContextWindow == 0 {
		if strings.Contains(lowerID, "claude-opus-4-7") || strings.Contains(lowerID, "claude-opus-4-6") || strings.Contains(lowerID, "claude-sonnet-4-6") {
			result.ContextWindow = 1000000
		} else if strings.Contains(lowerID, "claude") {
			result.ContextWindow = 200000
		} else {
			result.ContextWindow = 128000
		}
	}
	return result
}

func (p *AnthropicProvider) messageBlocksFromText(content string) []anthropic.ContentBlockParamUnion {
	return []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(coalescePromptText(content))}
}

func (p *AnthropicProvider) messageBlocksFromMessage(message models.Message) ([]anthropic.ContentBlockParamUnion, error) {
	images, err := collectProviderImageInputs(message)
	if err != nil {
		return nil, err
	}
	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(images)+1)
	if strings.TrimSpace(message.Content) != "" || len(images) == 0 {
		blocks = append(blocks, anthropic.NewTextBlock(coalescePromptText(message.Content)))
	}
	for _, image := range images {
		blocks = append(blocks, anthropic.NewImageBlockBase64(image.MimeType, base64EncodeBytes(image.Data)))
	}
	return blocks, nil
}

func (p *AnthropicProvider) toolResultBlocksFromMessage(message models.Message) ([]anthropic.ContentBlockParamUnion, error) {
	images, err := collectProviderImageInputs(message)
	if err != nil {
		return nil, err
	}
	content := make([]anthropic.ToolResultBlockParamContentUnion, 0, len(images)+1)
	if strings.TrimSpace(message.Content) != "" || len(images) == 0 {
		content = append(content, anthropic.ToolResultBlockParamContentUnion{
			OfText: &anthropic.TextBlockParam{Text: coalescePromptText(message.Content)},
		})
	}
	for _, image := range images {
		content = append(content, anthropic.ToolResultBlockParamContentUnion{
			OfImage: &anthropic.ImageBlockParam{
				Source: anthropic.ImageBlockParamSourceUnion{
					OfBase64: &anthropic.Base64ImageSourceParam{
						Data:      base64EncodeBytes(image.Data),
						MediaType: anthropic.Base64ImageSourceMediaType(image.MimeType),
					},
				},
			},
		})
	}
	toolResult := anthropic.ToolResultBlockParam{
		ToolUseID: message.ToolCallID,
		Content:   content,
		IsError:   anthropic.Bool(strings.HasPrefix(message.Content, "Error:")),
	}
	return []anthropic.ContentBlockParamUnion{{OfToolResult: &toolResult}}, nil
}

func (p *AnthropicProvider) newBlockBuilder(event anthropic.ContentBlockStartEvent) *anthropicBlockBuilder {
	builder := &anthropicBlockBuilder{}
	switch b := event.ContentBlock.AsAny().(type) {
	case anthropic.TextBlock:
		builder.kind = "text"
		if b.Text != "" {
			builder.text.WriteString(b.Text)
		}
	case anthropic.ThinkingBlock:
		builder.kind = "thinking"
		builder.signature = b.Signature
		if b.Thinking != "" {
			builder.thinking.WriteString(b.Thinking)
		}
	case anthropic.ToolUseBlock:
		builder.kind = "tool_use"
		builder.id = b.ID
		builder.name = b.Name
		if len(b.Input) > 0 {
			builder.inputJSON.Write(b.Input)
		}
	}
	return builder
}

func (p *AnthropicProvider) applyDelta(builder *anthropicBlockBuilder, event anthropic.ContentBlockDeltaEvent, ch chan<- models.StreamEvent) {
	switch delta := event.Delta.AsAny().(type) {
	case anthropic.TextDelta:
		builder.text.WriteString(delta.Text)
		if delta.Text != "" {
			ch <- models.StreamEvent{Type: "content", Content: delta.Text}
		}
	case anthropic.ThinkingDelta:
		builder.thinking.WriteString(delta.Thinking)
		if delta.Thinking != "" {
			ch <- models.StreamEvent{Type: "thinking", ThinkingContent: delta.Thinking, ReasoningMetadata: newAnthropicReasoningMetadata("anthropic", builder.signature)}
		}
	case anthropic.InputJSONDelta:
		builder.inputJSON.WriteString(delta.PartialJSON)
	}
}
