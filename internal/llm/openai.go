package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

type OpenAIProvider struct {
	name           string
	client         *openai.Client
	model          string
	config         config.ProviderConfig
	defaultOptions ChatOptions
	decider        *proxy.Decider
}

type staticHeaderHTTPClient struct {
	base    http.Client
	headers map[string]string
}

func (c *staticHeaderHTTPClient) Do(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.Header = req.Header.Clone()
	for key, value := range c.headers {
		if strings.TrimSpace(value) == "" {
			continue
		}
		cloned.Header.Set(key, value)
	}
	return c.base.Do(cloned)
}

func NewOpenAIProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (*OpenAIProvider, error) {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if decider != nil {
		httpClient := decider.ForLLM(name).HTTPClient
		opts = append(opts, option.WithHTTPClient(httpClient))
	}
	if len(cfg.Headers) > 0 && decider == nil {
		httpClient := &staticHeaderHTTPClient{base: http.Client{}, headers: cfg.Headers}
		opts = append(opts, option.WithHTTPClient(httpClient))
	} else if len(cfg.Headers) > 0 {
		// If decider is set, wrap the proxy-configured client with headers
		baseClient := decider.ForLLM(name).HTTPClient
		httpClient := &staticHeaderHTTPClient{base: *baseClient, headers: cfg.Headers}
		opts[len(opts)-1] = option.WithHTTPClient(httpClient)
	}
	client := openai.NewClient(opts...)
	return &OpenAIProvider{name: name, client: &client, model: cfg.DefaultModel, config: cfg, decider: decider}, nil
}

func (p *OpenAIProvider) Name() string { return p.name }

func (p *OpenAIProvider) RequiresDeferredToolImageReplay() bool { return true }

func (p *OpenAIProvider) resolveModel(opts ChatOptions) string {
	if opts.Model != "" {
		return opts.Model
	}
	if p.defaultOptions.Model != "" {
		return p.defaultOptions.Model
	}
	return p.model
}

func (p *OpenAIProvider) Chat(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error) {
	log.Debug("Chat request started", logger.Fields{"provider": p.name, "model": p.model, "messages_count": len(messages), "tools_count": len(tools)})
	params, err := p.buildChatCompletionParams(messages, tools, p.defaultOptions)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		log.Error("Chat completion failed", logger.Fields{"error": err.Error()})
		return nil, fmt.Errorf("openai chat completion: %w", err)
	}
	return convertOpenAIChatResponse(resp)
}

func (p *OpenAIProvider) ChatStream(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	log.Debug("Stream request started", logger.Fields{"provider": p.name, "model": p.model, "messages_count": len(messages), "tools_count": len(tools)})
	toolNames := make([]string, len(tools))
	for i, t := range tools {
		toolNames[i] = t.Function.Name
	}
	if len(toolNames) > 0 {
		log.Debug("Tools available", logger.Fields{"tools": toolNames})
	}
	params, err := p.buildChatCompletionParams(messages, tools, p.defaultOptions)
	if err != nil {
		return nil, err
	}
	stream := p.client.Chat.Completions.NewStreaming(ctx, params)

	ch := make(chan models.StreamEvent, 100)
	go func() {
		defer close(ch)
		log.Debug("Stream processing started")
		acc := openai.ChatCompletionAccumulator{}
		for stream.Next() {
			// Check for context cancellation
			select {
			case <-ctx.Done():
				ch <- models.StreamEvent{Type: "cancelled", Done: true}
				return
			default:
			}

			chunk := stream.Current()
			acc.AddChunk(chunk)
			if _, ok := acc.JustFinishedContent(); ok {
				log.Debug("Content stream finished")
			}
			if tool, ok := acc.JustFinishedToolCall(); ok {
				log.Debug("Tool call stream finished", logger.Fields{"index": tool.Index, "name": tool.Name})
			}
			if len(chunk.Choices) > 0 {
				choice := chunk.Choices[0]
				if choice.Delta.Content != "" {
					ch <- models.StreamEvent{Type: "content", Content: choice.Delta.Content}
				}
				if choice.FinishReason != "" {
					toolCalls := buildToolCallsFromAccumulator(&acc)
					if len(toolCalls) > 0 {
						log.Info("Stream tool calls complete", logger.Fields{"count": len(toolCalls)})
					}
					ch <- models.StreamEvent{Type: string(choice.FinishReason), ToolCalls: toolCalls, Done: choice.FinishReason == "stop"}
					return
				}
			}
		}
		if err := stream.Err(); err != nil {
			if ctx.Err() != nil {
				ch <- models.StreamEvent{Type: "cancelled", Done: true}
			} else {
				log.Error("Stream error", logger.Fields{"error": err.Error()})
				ch <- models.StreamEvent{Type: "error", Done: true, Error: err}
			}
		}
	}()
	return ch, nil
}

func buildToolCallsFromAccumulator(acc *openai.ChatCompletionAccumulator) []models.ToolCall {
	if len(acc.Choices) == 0 {
		return nil
	}
	msg := acc.Choices[0].Message
	if len(msg.ToolCalls) == 0 {
		return nil
	}
	result := make([]models.ToolCall, len(msg.ToolCalls))
	for i, tc := range msg.ToolCalls {
		result[i] = models.ToolCall{
			ID:   tc.ID,
			Type: string(tc.Type),
			Function: models.ToolCallFunction{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		}
	}
	return result
}

func (p *OpenAIProvider) SetDefaultOptions(opts ChatOptions) { p.defaultOptions = opts }

func (p *OpenAIProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (*models.Response, error) {
	log.Debug("Chat with options started", logger.Fields{"provider": p.name, "model": p.model, "messages_count": len(messages), "tools_count": len(tools)})
	mergedOpts := p.mergeOptions(p.defaultOptions, opts)
	params, err := p.buildChatCompletionParams(messages, tools, mergedOpts)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Chat.Completions.New(ctx, params)
	if err != nil {
		log.Error("Chat completion failed", logger.Fields{"error": err.Error()})
		return nil, fmt.Errorf("openai chat completion: %w", err)
	}
	return convertOpenAIChatResponse(resp)
}

func (p *OpenAIProvider) buildChatCompletionParams(messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (openai.ChatCompletionNewParams, error) {
	openaiMsgs := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for _, m := range messages {
		if shouldSkipSyntheticToolMessage(m) {
			continue
		}
		role := normalizeNativeOpenAIRole(m.Role)
		switch role {
		case "system":
			openaiMsgs = append(openaiMsgs, openai.SystemMessage(m.Content))
		case "developer":
			openaiMsgs = append(openaiMsgs, openai.DeveloperMessage(m.Content))
		case "user":
			images, err := collectProviderImageInputs(m)
			if err != nil {
				return openai.ChatCompletionNewParams{}, err
			}
			if len(images) == 0 {
				openaiMsgs = append(openaiMsgs, openai.UserMessage(m.Content))
				break
			}
			parts := make([]openai.ChatCompletionContentPartUnionParam, 0, len(images)+1)
			if strings.TrimSpace(m.Content) != "" || len(images) == 0 {
				parts = append(parts, openai.ChatCompletionContentPartUnionParam{OfText: &openai.ChatCompletionContentPartTextParam{Text: coalescePromptText(m.Content)}})
			}
			for _, image := range images {
				parts = append(parts, openai.ChatCompletionContentPartUnionParam{OfImageURL: &openai.ChatCompletionContentPartImageParam{ImageURL: openai.ChatCompletionContentPartImageImageURLParam{URL: providerImageDataURL(image)}}})
			}
			openaiMsgs = append(openaiMsgs, openai.ChatCompletionMessageParamUnion{OfUser: &openai.ChatCompletionUserMessageParam{Content: openai.ChatCompletionUserMessageParamContentUnion{OfArrayOfContentParts: parts}}})
		case "assistant":
			if len(m.ToolCalls) > 0 {
				toolCallParams := make([]openai.ChatCompletionMessageToolCallUnionParam, len(m.ToolCalls))
				for i, tc := range m.ToolCalls {
					toolCallParams[i] = openai.ChatCompletionMessageToolCallUnionParam{
						OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
							ID: tc.ID,
							Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
								Name:      tc.Function.Name,
								Arguments: tc.Function.Arguments,
							},
						},
					}
				}
				assistant := &openai.ChatCompletionAssistantMessageParam{ToolCalls: toolCallParams}
				if strings.TrimSpace(m.Content) != "" {
					assistant.Content = openai.ChatCompletionAssistantMessageParamContentUnion{OfString: openai.String(m.Content)}
				} else {
					// Assistant messages require content or tool_calls per OpenAI API spec.
					assistant.Content = openai.ChatCompletionAssistantMessageParamContentUnion{OfString: openai.String(" ")}
				}
				openaiMsgs = append(openaiMsgs, openai.ChatCompletionMessageParamUnion{OfAssistant: assistant})
			} else {
				// Assistant messages require content or tool_calls per OpenAI API spec.
				// If content is empty, add placeholder to avoid 400 errors.
				content := m.Content
				if strings.TrimSpace(content) == "" {
					content = " "
				}
				openaiMsgs = append(openaiMsgs, openai.AssistantMessage(content))
			}
		case "tool":
			// Tool messages require content per OpenAI API spec.
			content := m.Content
			if strings.TrimSpace(content) == "" {
				content = " "
			}
			openaiMsgs = append(openaiMsgs, openai.ToolMessage(m.ToolCallID, content))
		default:
			openaiMsgs = append(openaiMsgs, openai.UserMessage(m.Content))
		}
	}
	openaiTools := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		paramsJSON, _ := json.Marshal(t.Function.Parameters)
		log.Debug("Tool definition", logger.Fields{"tool": t.Function.Name, "description": truncateDesc(t.Function.Description, 50), "params": string(paramsJSON)})
		openaiTools = append(openaiTools, openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        t.Function.Name,
			Description: openai.String(t.Function.Description),
			Parameters:  t.Function.Parameters,
		}))
	}
	resolvedModel := p.resolveModel(opts)
	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(resolvedModel),
		Messages: openaiMsgs,
		Tools:    openaiTools,
	}
	p.applyOptions(&params, opts)
	p.applyProviderConfig(&params)
	return params, nil
}

func normalizeNativeOpenAIRole(role string) string {
	normalized := strings.TrimSpace(strings.ToLower(role))
	switch normalized {
	case "system", "user", "assistant", "tool", "developer":
		return normalized
	default:
		if normalized == "" {
			return "user"
		}
		return normalized
	}
}

func (p *OpenAIProvider) applyOptions(params *openai.ChatCompletionNewParams, opts ChatOptions) {
	if opts.Temperature != nil {
		params.Temperature = openai.Float(*opts.Temperature)
		log.Debug("LLM option applied", logger.Fields{"temperature": *opts.Temperature})
	}
	if opts.MaxTokens > 0 {
		params.MaxTokens = openai.Int(int64(opts.MaxTokens))
		log.Debug("LLM option applied", logger.Fields{"max_tokens": opts.MaxTokens})
	}
	if opts.TopP != nil {
		params.TopP = openai.Float(*opts.TopP)
		log.Debug("LLM option applied", logger.Fields{"top_p": *opts.TopP})
	}
}

func (p *OpenAIProvider) applyProviderConfig(params *openai.ChatCompletionNewParams) {
	if len(p.config.Stop) > 0 {
		params.Stop = openai.ChatCompletionNewParamsStopUnion{OfStringArray: p.config.Stop}
	}
	if p.config.PresencePenalty != nil {
		params.PresencePenalty = openai.Float(*p.config.PresencePenalty)
	}
	if p.config.FrequencyPenalty != nil {
		params.FrequencyPenalty = openai.Float(*p.config.FrequencyPenalty)
	}
	if p.config.MaxCompletionTokens > 0 {
		params.MaxCompletionTokens = openai.Int(int64(p.config.MaxCompletionTokens))
	}
	if p.config.ReasoningEffort != "" {
		params.ReasoningEffort = shared.ReasoningEffort(p.config.ReasoningEffort)
	}
	if p.config.User != "" {
		params.User = openai.String(p.config.User)
	}
	if p.config.N > 0 {
		params.N = openai.Int(int64(p.config.N))
	}
	if p.config.Seed != nil {
		params.Seed = openai.Int(int64(*p.config.Seed))
	}
	if len(p.config.Metadata) > 0 {
		params.Metadata = cloneStringMap(p.config.Metadata)
	}
	if p.config.ToolChoice != "" {
		switch p.config.ToolChoice {
		case "auto":
			params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String("auto")}
		case "none":
			params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String("none")}
		case "required":
			params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String("required")}
		default:
			params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
				OfFunctionToolChoice: &openai.ChatCompletionNamedToolChoiceParam{
					Function: openai.ChatCompletionNamedToolChoiceFunctionParam{
						Name: p.config.ToolChoice,
					},
				},
			}
		}
	}
	if p.config.ParallelToolCalls != nil {
		params.ParallelToolCalls = openai.Bool(*p.config.ParallelToolCalls)
	}
	if len(p.config.ResponseFormat) > 0 {
		if responseFormat := buildChatCompletionResponseFormat(p.config.ResponseFormat); responseFormat != nil {
			params.ResponseFormat = *responseFormat
		}
	}
}

func buildChatCompletionResponseFormat(format map[string]any) *openai.ChatCompletionNewParamsResponseFormatUnion {
	typeValue, _ := format["type"].(string)
	typeValue = strings.TrimSpace(typeValue)
	if typeValue == "" {
		return nil
	}
	switch typeValue {
	case "text":
		result := openai.ChatCompletionNewParamsResponseFormatUnion{OfText: &shared.ResponseFormatTextParam{}}
		return &result
	case "json_object":
		result := openai.ChatCompletionNewParamsResponseFormatUnion{OfJSONObject: &shared.ResponseFormatJSONObjectParam{}}
		return &result
	case "json_schema":
		jsonSchema, ok := format["json_schema"]
		if !ok {
			return nil
		}
		payload, err := json.Marshal(jsonSchema)
		if err != nil {
			return nil
		}
		var schema map[string]any
		if err := json.Unmarshal(payload, &schema); err != nil {
			return nil
		}
		name, _ := schema["name"].(string)
		if name == "" {
			name = "response"
		}
		result := openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   name,
					Schema: schema,
					Strict: openai.Bool(true),
				},
			},
		}
		return &result
	default:
		return nil
	}
}

func convertOpenAIChatResponse(resp *openai.ChatCompletion) (*models.Response, error) {
	if len(resp.Choices) == 0 {
		log.Error("Empty response from LLM")
		return nil, fmt.Errorf("empty response from LLM")
	}
	choice := resp.Choices[0]
	log.Debug("Chat response received", logger.Fields{"finish_reason": choice.FinishReason, "content_length": len(choice.Message.Content)})
	result := &models.Response{Content: choice.Message.Content}
	if len(choice.Message.ToolCalls) > 0 {
		log.Info("Tool calls decided", logger.Fields{"count": len(choice.Message.ToolCalls)})
		for i, tc := range choice.Message.ToolCalls {
			log.Debug("Tool call detail", logger.Fields{"index": i, "name": tc.Function.Name, "id": tc.ID, "arguments": tc.Function.Arguments})
		}
	}
	for _, tc := range choice.Message.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, models.ToolCall{
			ID:   tc.ID,
			Type: string(tc.Type),
			Function: models.ToolCallFunction{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	return result, nil
}

func (p *OpenAIProvider) mergeOptions(defaultOpts, overrideOpts ChatOptions) ChatOptions {
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

func (p *OpenAIProvider) GetMaxContextTokens() int {
	model := strings.ToLower(p.resolveModel(p.defaultOptions))
	if strings.Contains(model, "gpt-4o") || strings.Contains(model, "gpt-4-turbo") {
		return 128000
	}
	if strings.Contains(model, "gpt-4") {
		if strings.Contains(model, "turbo") || strings.Contains(model, "1106") || strings.Contains(model, "0125") {
			return 128000
		}
		return 8192
	}
	if strings.Contains(model, "gpt-3.5") || strings.Contains(model, "gpt-35") {
		return 16385
	}
	if strings.Contains(model, "claude") {
		return 200000
	}
	return 128000
}

func (p *OpenAIProvider) BuiltinModels() []ProviderModel {
	models := builtinModelsFromConfig(p.config.DefaultModel, p.config.Models)
	for i := range models {
		models[i].SupportsTools = true
		models[i].SupportsStreaming = true
		if models[i].ContextWindow == 0 {
			models[i].ContextWindow = getOpenAICompatibleMaxContextTokens(strings.ToLower(models[i].ID))
		}
	}
	return models
}

func (p *OpenAIProvider) FetchModels(ctx context.Context) ([]ProviderModel, error) {
	pager := p.client.Models.ListAutoPaging(ctx)
	result := make([]ProviderModel, 0)
	for pager.Next() {
		result = append(result, openAIProviderModelFromSDK(pager.Current()))
	}
	if err := pager.Err(); err != nil {
		return nil, fmt.Errorf("openai list models: %w", err)
	}
	return result, nil
}

func (p *OpenAIProvider) ListModels(ctx context.Context) ([]ProviderModel, error) {
	return listProviderModels(ctx, p)
}

func openAIProviderModelFromSDK(model openai.Model) ProviderModel {
	id := strings.TrimSpace(model.ID)
	lowerID := strings.ToLower(id)
	result := ProviderModel{
		ID:                id,
		Name:              id,
		ContextWindow:     getOpenAICompatibleMaxContextTokens(lowerID),
		SupportsStreaming: true,
	}
	if strings.Contains(lowerID, "gpt") || strings.Contains(lowerID, "o1") || strings.Contains(lowerID, "o3") || strings.Contains(lowerID, "o4") {
		result.SupportsTools = true
	}
	if strings.Contains(lowerID, "vision") || strings.Contains(lowerID, "gpt-4o") {
		result.SupportsVision = true
	}
	if strings.HasPrefix(lowerID, "o") {
		result.Reasoning = true
	}
	return result
}

var _ Provider = (*OpenAIProvider)(nil)
