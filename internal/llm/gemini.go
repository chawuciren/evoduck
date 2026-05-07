package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/genai"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type GeminiProvider struct {
	name           string
	client         *genai.Client
	model          string
	decider        *proxy.Decider
	defaultOptions ChatOptions
}

func NewGeminiProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (*GeminiProvider, error) {
	ctx := context.Background()
	clientCfg := &genai.ClientConfig{
		APIKey:  cfg.APIKey,
		Backend: genai.BackendGeminiAPI,
	}
	if cfg.BaseURL != "" {
		clientCfg.HTTPOptions = genai.HTTPOptions{BaseURL: cfg.BaseURL}
	}
	client, err := genai.NewClient(ctx, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("init gemini client: %w", err)
	}
	return &GeminiProvider{name: name, client: client, model: cfg.DefaultModel, decider: decider}, nil
}

func (p *GeminiProvider) Name() string {
	return p.name
}

func (p *GeminiProvider) SetDefaultOptions(opts ChatOptions) {
	p.defaultOptions = opts
}

func (p *GeminiProvider) Chat(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error) {
	return p.chatWithMergedOptions(ctx, messages, tools, p.defaultOptions)
}

func (p *GeminiProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (*models.Response, error) {
	return p.chatWithMergedOptions(ctx, messages, tools, p.mergeOptions(p.defaultOptions, opts))
}

func (p *GeminiProvider) chatWithMergedOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (*models.Response, error) {
	log.Debug("Gemini chat request started", logger.Fields{
		"provider":       p.name,
		"model":          p.resolveModel(opts),
		"messages_count": len(messages),
		"tools_count":    len(tools),
	})

	contents, cfg, err := p.buildRequest(messages, tools, opts)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Models.GenerateContent(ctx, p.resolveModel(opts), contents, cfg)
	if err != nil {
		log.Error("Gemini chat failed", logger.Fields{"error": err.Error()})
		return nil, fmt.Errorf("gemini generate content: %w", err)
	}

	return p.responseFromGenerateContent(resp)
}

func (p *GeminiProvider) ChatStream(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	log.Debug("Gemini stream request started", logger.Fields{
		"provider":       p.name,
		"model":          p.resolveModel(p.defaultOptions),
		"messages_count": len(messages),
		"tools_count":    len(tools),
	})

	contents, cfg, err := p.buildRequest(messages, tools, p.defaultOptions)
	if err != nil {
		return nil, err
	}

	stream := p.client.Models.GenerateContentStream(ctx, p.resolveModel(p.defaultOptions), contents, cfg)
	ch := make(chan models.StreamEvent, 100)

	go func() {
		defer close(ch)

		toolCallBuilders := map[string]*models.ToolCall{}
		for resp, err := range stream {
			// Check for context cancellation
			select {
			case <-ctx.Done():
				ch <- models.StreamEvent{Type: "cancelled", Done: true}
				return
			default:
			}

			if err != nil {
				if ctx.Err() != nil {
					ch <- models.StreamEvent{Type: "cancelled", Done: true}
				} else {
					log.Error("Gemini stream failed", logger.Fields{"error": err.Error()})
					ch <- models.StreamEvent{Type: "error", Done: true, Error: err}
				}
				return
			}
			if resp == nil {
				continue
			}
			if len(resp.Candidates) == 0 || resp.Candidates[0] == nil || resp.Candidates[0].Content == nil {
				continue
			}

			for _, part := range resp.Candidates[0].Content.Parts {
				if part == nil {
					continue
				}
				if part.Text != "" {
					eventType := "content"
					if part.Thought {
						eventType = "thinking"
					}
					if eventType == "thinking" {
						ch <- models.StreamEvent{Type: eventType, ThinkingContent: part.Text, ReasoningMetadata: newGeminiReasoningMetadata("gemini", base64EncodeBytes(part.ThoughtSignature))}
					} else {
						ch <- models.StreamEvent{Type: eventType, Content: part.Text}
					}
				}
				if part.FunctionCall != nil {
					toolCall, err := p.toolCallFromFunctionCall(part.FunctionCall)
					if err != nil {
						ch <- models.StreamEvent{Type: "error", Done: true, Error: err}
						return
					}
					key := toolCall.ID
					if key == "" {
						key = toolCall.Function.Name
					}
					toolCallBuilders[key] = &toolCall
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
	}()

	return ch, nil
}

func (p *GeminiProvider) resolveModel(opts ChatOptions) string {
	if opts.Model != "" {
		return opts.Model
	}
	if p.defaultOptions.Model != "" {
		return p.defaultOptions.Model
	}
	return p.model
}

func (p *GeminiProvider) buildRequest(messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) ([]*genai.Content, *genai.GenerateContentConfig, error) {
	system, contents, err := p.convertMessages(messages)
	if err != nil {
		return nil, nil, err
	}
	cfg := &genai.GenerateContentConfig{}
	if system != "" {
		cfg.SystemInstruction = genai.NewContentFromText(system, genai.RoleUser)
	}
	if len(tools) > 0 {
		cfg.Tools = []*genai.Tool{{FunctionDeclarations: p.convertTools(tools)}}
		cfg.ToolConfig = &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{Mode: genai.FunctionCallingConfigModeAuto}}
	}
	p.applyOptions(cfg, opts)
	return contents, cfg, nil
}

func (p *GeminiProvider) convertMessages(messages []models.Message) (string, []*genai.Content, error) {
	var systemParts []string
	var result []*genai.Content

	appendContent := func(role genai.Role, parts ...*genai.Part) {
		if len(parts) == 0 {
			return
		}
		if len(result) > 0 && result[len(result)-1].Role == string(role) {
			result[len(result)-1].Parts = append(result[len(result)-1].Parts, parts...)
			return
		}
		result = append(result, genai.NewContentFromParts(parts, role))
	}

	for _, m := range messages {
		switch m.Role {
		case "system":
			if strings.TrimSpace(m.Content) != "" {
				systemParts = append(systemParts, m.Content)
			}
		case "user":
			parts, err := p.messagePartsFromMessage(m)
			if err != nil {
				return "", nil, err
			}
			appendContent(genai.RoleUser, parts...)
		case "assistant":
			var parts []*genai.Part
			if strings.TrimSpace(m.Content) != "" {
				parts = append(parts, p.messagePartsFromText(m.Content)...)
			}
			if metadata := m.ReasoningMetadata; reasoningProvider(metadata) == "gemini" {
				thinking := strings.TrimSpace(m.ThinkingContent)
				signature := ""
				if metadata.Gemini != nil {
					signature = metadata.Gemini.ThoughtSignature
				}
				if thinking != "" {
					part := &genai.Part{Text: thinking, Thought: true}
					if decoded, err := base64DecodeBytes(signature); err == nil && len(decoded) > 0 {
						part.ThoughtSignature = decoded
					}
					parts = append(parts, part)
				}
			}
			for _, tc := range m.ToolCalls {
				args := map[string]any{}
				if strings.TrimSpace(tc.Function.Arguments) != "" {
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
						return "", nil, fmt.Errorf("decode gemini tool call args: %w", err)
					}
				}
				part := genai.NewPartFromFunctionCall(tc.Function.Name, args)
				if part.FunctionCall != nil {
					part.FunctionCall.ID = tc.ID
				}
				parts = append(parts, part)
			}
			appendContent(genai.RoleModel, parts...)
		case "tool":
			response := map[string]any{"output": m.Content}
			if strings.HasPrefix(m.Content, "Error:") {
				response = map[string]any{"error": strings.TrimSpace(strings.TrimPrefix(m.Content, "Error:"))}
			}
			part := genai.NewPartFromFunctionResponse(p.toolNameForToolResponse(result, m.ToolCallID), response)
			if part.FunctionResponse != nil {
				part.FunctionResponse.ID = m.ToolCallID
			}
			appendContent(genai.RoleUser, part)
		default:
			appendContent(genai.RoleUser, p.messagePartsFromText(m.Content)...)
		}
	}

	if len(result) == 0 {
		result = append(result, genai.NewContentFromText(" ", genai.RoleUser))
	}
	return strings.Join(systemParts, "\n\n"), result, nil
}

func (p *GeminiProvider) toolNameForToolResponse(contents []*genai.Content, toolCallID string) string {
	for i := len(contents) - 1; i >= 0; i-- {
		for _, part := range contents[i].Parts {
			if part != nil && part.FunctionCall != nil && part.FunctionCall.ID == toolCallID {
				return part.FunctionCall.Name
			}
		}
	}
	return "tool"
}

func (p *GeminiProvider) convertTools(tools []models.ToolDefinition) []*genai.FunctionDeclaration {
	result := make([]*genai.FunctionDeclaration, 0, len(tools))
	for _, t := range tools {
		result = append(result, &genai.FunctionDeclaration{
			Name:                 t.Function.Name,
			Description:          t.Function.Description,
			ParametersJsonSchema: t.Function.Parameters,
		})
	}
	return result
}

func (p *GeminiProvider) messagePartsFromText(content string) []*genai.Part {
	return []*genai.Part{genai.NewPartFromText(coalescePromptText(content))}
}

func (p *GeminiProvider) messagePartsFromMessage(message models.Message) ([]*genai.Part, error) {
	images, err := collectProviderImageInputs(message)
	if err != nil {
		return nil, err
	}
	parts := make([]*genai.Part, 0, len(images)+1)
	if strings.TrimSpace(message.Content) != "" || len(images) == 0 {
		parts = append(parts, genai.NewPartFromText(coalescePromptText(message.Content)))
	}
	for _, image := range images {
		parts = append(parts, genai.NewPartFromBytes(image.Data, image.MimeType))
	}
	return parts, nil
}

func (p *GeminiProvider) responseFromGenerateContent(resp *genai.GenerateContentResponse) (*models.Response, error) {
	result := &models.Response{}
	if resp == nil || len(resp.Candidates) == 0 || resp.Candidates[0] == nil || resp.Candidates[0].Content == nil {
		return result, nil
	}
	for _, part := range resp.Candidates[0].Content.Parts {
		if part == nil {
			continue
		}
		if part.Text != "" {
			if part.Thought {
				if result.ReasoningContent != "" {
					result.ReasoningContent += "\n"
				}
				result.ReasoningContent += part.Text
				result.ReasoningMetadata = newGeminiReasoningMetadata("gemini", base64EncodeBytes(part.ThoughtSignature))
			} else {
				result.Content += part.Text
			}
		}
		if part.FunctionCall != nil {
			toolCall, err := p.toolCallFromFunctionCall(part.FunctionCall)
			if err != nil {
				return nil, err
			}
			result.ToolCalls = append(result.ToolCalls, toolCall)
		}
	}
	return result, nil
}

func (p *GeminiProvider) toolCallFromFunctionCall(fc *genai.FunctionCall) (models.ToolCall, error) {
	args, err := json.Marshal(fc.Args)
	if err != nil {
		return models.ToolCall{}, fmt.Errorf("encode gemini tool call args: %w", err)
	}
	return models.ToolCall{
		ID:   fc.ID,
		Type: "function",
		Function: models.ToolCallFunction{
			Name:      fc.Name,
			Arguments: strings.TrimSpace(string(args)),
		},
	}, nil
}

func (p *GeminiProvider) applyOptions(cfg *genai.GenerateContentConfig, opts ChatOptions) {
	if opts.Temperature != nil {
		cfg.Temperature = genai.Ptr(float32(*opts.Temperature))
		log.Debug("Gemini option applied", logger.Fields{"temperature": *opts.Temperature})
	}
	if opts.TopP != nil {
		cfg.TopP = genai.Ptr(float32(*opts.TopP))
		log.Debug("Gemini option applied", logger.Fields{"top_p": *opts.TopP})
	}
	if opts.MaxTokens > 0 {
		cfg.MaxOutputTokens = int32(opts.MaxTokens)
		log.Debug("Gemini option applied", logger.Fields{"max_tokens": opts.MaxTokens})
	}
}

func (p *GeminiProvider) mergeOptions(defaultOpts, overrideOpts ChatOptions) ChatOptions {
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

func (p *GeminiProvider) GetMaxContextTokens() int {
	model := strings.ToLower(p.resolveModel(p.defaultOptions))
	if strings.Contains(model, "gemini-2.5-pro") || strings.Contains(model, "gemini-2.5-flash") {
		return 1000000
	}
	if strings.Contains(model, "gemini") {
		return 128000
	}
	return 128000
}

func (p *GeminiProvider) BuiltinModels() []ProviderModel {
	models := builtinModelsFromConfig(p.model, nil)
	for i := range models {
		models[i].SupportsTools = true
		models[i].SupportsStreaming = true
		models[i].ContextWindow = p.GetMaxContextTokens()
	}
	return models
}

func (p *GeminiProvider) FetchModels(ctx context.Context) ([]ProviderModel, error) {
	_ = ctx
	return nil, nil
}

func (p *GeminiProvider) ListModels(ctx context.Context) ([]ProviderModel, error) {
	return listProviderModels(ctx, p)
}
