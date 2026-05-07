package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	bedrockdocument "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

const defaultBedrockRegion = "us-east-1"

type BedrockProvider struct {
	name           string
	client         *bedrockruntime.Client
	model          string
	config         config.ProviderConfig
	decider        *proxy.Decider
	defaultOptions ChatOptions
}

func NewBedrockProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (*BedrockProvider, error) {
	region := strings.TrimSpace(cfg.Metadata["region"])
	if region == "" {
		region = defaultBedrockRegion
	}

	loadOpts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if profile := strings.TrimSpace(cfg.Metadata["profile"]); profile != "" {
		loadOpts = append(loadOpts, awsconfig.WithSharedConfigProfile(profile))
	}
	if accessKey := strings.TrimSpace(cfg.Metadata["access_key_id"]); accessKey != "" {
		secretKey := strings.TrimSpace(cfg.Metadata["secret_access_key"])
		if secretKey == "" {
			return nil, fmt.Errorf("bedrock metadata.secret_access_key is required when metadata.access_key_id is set")
		}
		sessionToken := strings.TrimSpace(cfg.Metadata["session_token"])
		loadOpts = append(loadOpts, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, sessionToken)))
	}
	if decider != nil {
		httpClient := decider.ForLLM(name).HTTPClient
		loadOpts = append(loadOpts, awsconfig.WithHTTPClient(httpClient))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	if cfg.DefaultModel == "" && len(cfg.Models) > 0 {
		cfg.DefaultModel = strings.TrimSpace(cfg.Models[0].ID)
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "anthropic.claude-3-5-sonnet-20240620-v1:0"
	}
	if len(cfg.Models) == 0 && cfg.DefaultModel != "" {
		cfg.Models = []config.ProviderModelConfig{{ID: cfg.DefaultModel, Name: cfg.DefaultModel, Type: config.ProviderModelTypeChat}}
	}

	return &BedrockProvider{
		name:    name,
		client:  bedrockruntime.NewFromConfig(awsCfg),
		model:   cfg.DefaultModel,
		config:  cfg,
		decider: decider,
	}, nil
}

func (p *BedrockProvider) Name() string { return p.name }

func (p *BedrockProvider) SetDefaultOptions(opts ChatOptions) { p.defaultOptions = opts }

func (p *BedrockProvider) resolveModel(opts ChatOptions) string {
	if opts.Model != "" {
		return opts.Model
	}
	if p.defaultOptions.Model != "" {
		return p.defaultOptions.Model
	}
	return p.model
}

func (p *BedrockProvider) mergeOptions(defaultOpts, overrideOpts ChatOptions) ChatOptions {
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

func (p *BedrockProvider) Chat(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error) {
	return p.ChatWithOptions(ctx, messages, tools, p.defaultOptions)
}

func (p *BedrockProvider) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (*models.Response, error) {
	input, err := p.buildConverseInput(messages, tools, p.mergeOptions(p.defaultOptions, opts))
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Converse(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("bedrock converse: %w", err)
	}
	return p.responseFromConverse(resp)
}

func (p *BedrockProvider) ChatStream(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	resp, err := p.Chat(ctx, messages, tools)
	if err != nil {
		if ctx.Err() != nil {
			ch := make(chan models.StreamEvent, 1)
			go func() {
				defer close(ch)
				ch <- models.StreamEvent{Type: "cancelled", Done: true}
			}()
			return ch, nil
		}
		return nil, err
	}
	ch := make(chan models.StreamEvent, 4)
	go func() {
		defer close(ch)
		// Check for context cancellation
		select {
		case <-ctx.Done():
			ch <- models.StreamEvent{Type: "cancelled", Done: true}
			return
		default:
		}
		if resp.ReasoningContent != "" {
			ch <- models.StreamEvent{Type: "thinking", ThinkingContent: resp.ReasoningContent}
		}
		if resp.Content != "" {
			ch <- models.StreamEvent{Type: "content", Content: resp.Content}
		}
		if len(resp.ToolCalls) > 0 {
			ch <- models.StreamEvent{Type: "tool_calls", ToolCalls: resp.ToolCalls}
		}
		ch <- models.StreamEvent{Type: "stop", Done: true}
	}()
	return ch, nil
}

func (p *BedrockProvider) buildConverseInput(messages []models.Message, tools []models.ToolDefinition, opts ChatOptions) (*bedrockruntime.ConverseInput, error) {
	system, bedrockMessages, err := p.convertMessages(messages)
	if err != nil {
		return nil, err
	}
	input := &bedrockruntime.ConverseInput{
		ModelId:  stringPtr(p.resolveModel(opts)),
		Messages: bedrockMessages,
	}
	if len(system) > 0 {
		input.System = make([]bedrocktypes.SystemContentBlock, 0, len(system))
		for _, part := range system {
			input.System = append(input.System, &bedrocktypes.SystemContentBlockMemberText{Value: part})
		}
	}
	if len(tools) > 0 {
		toolConfig, err := p.convertTools(tools)
		if err != nil {
			return nil, err
		}
		input.ToolConfig = toolConfig
	}
	if inference := p.inferenceConfigFromOptions(opts); inference != nil {
		input.InferenceConfig = inference
	}
	if len(p.config.Metadata) > 0 {
		metadata := cloneStringMap(p.config.Metadata)
		delete(metadata, "region")
		delete(metadata, "profile")
		delete(metadata, "access_key_id")
		delete(metadata, "secret_access_key")
		delete(metadata, "session_token")
		if len(metadata) > 0 {
			input.RequestMetadata = metadata
		}
	}
	return input, nil
}

func (p *BedrockProvider) convertMessages(messages []models.Message) ([]string, []bedrocktypes.Message, error) {
	var system []string
	result := make([]bedrocktypes.Message, 0, len(messages))

	appendMessage := func(role bedrocktypes.ConversationRole, blocks ...bedrocktypes.ContentBlock) {
		if len(blocks) == 0 {
			return
		}
		if len(result) > 0 && result[len(result)-1].Role == role {
			result[len(result)-1].Content = append(result[len(result)-1].Content, blocks...)
			return
		}
		result = append(result, bedrocktypes.Message{Role: role, Content: blocks})
	}

	for _, m := range messages {
		switch m.Role {
		case "system":
			if strings.TrimSpace(m.Content) != "" {
				system = append(system, m.Content)
			}
		case "assistant":
			var blocks []bedrocktypes.ContentBlock
			if strings.TrimSpace(m.Content) != "" {
				blocks = append(blocks, &bedrocktypes.ContentBlockMemberText{Value: m.Content})
			}
			if metadata := m.ReasoningMetadata; reasoningProvider(metadata) == "bedrock" {
				thinking := strings.TrimSpace(m.ThinkingContent)
				signature := ""
				if metadata.Bedrock != nil {
					signature = metadata.Bedrock.Signature
				}
				if thinking != "" {
					reasoning := bedrocktypes.ReasoningTextBlock{Text: stringPtr(thinking)}
					if strings.TrimSpace(signature) != "" {
						reasoning.Signature = stringPtr(signature)
					}
					blocks = append(blocks, &bedrocktypes.ContentBlockMemberReasoningContent{Value: &bedrocktypes.ReasoningContentBlockMemberReasoningText{Value: reasoning}})
				}
			}
			for _, tc := range m.ToolCalls {
				var input any = map[string]any{}
				if strings.TrimSpace(tc.Function.Arguments) != "" {
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
						return nil, nil, fmt.Errorf("decode bedrock tool args: %w", err)
					}
				}
				blocks = append(blocks, &bedrocktypes.ContentBlockMemberToolUse{Value: bedrocktypes.ToolUseBlock{
					ToolUseId: stringPtr(tc.ID),
					Name:      stringPtr(tc.Function.Name),
					Input:     bedrockdocument.NewLazyDocument(input),
				}})
			}
			appendMessage(bedrocktypes.ConversationRoleAssistant, blocks...)
		case "tool":
			appendMessage(bedrocktypes.ConversationRoleUser, &bedrocktypes.ContentBlockMemberToolResult{Value: bedrocktypes.ToolResultBlock{
				ToolUseId: stringPtr(m.ToolCallID),
				Content:   []bedrocktypes.ToolResultContentBlock{&bedrocktypes.ToolResultContentBlockMemberText{Value: m.Content}},
			}})
		default:
			text := strings.TrimSpace(m.Content)
			if text == "" {
				text = " "
			}
			appendMessage(bedrocktypes.ConversationRoleUser, &bedrocktypes.ContentBlockMemberText{Value: text})
		}
	}

	if len(result) == 0 {
		result = append(result, bedrocktypes.Message{Role: bedrocktypes.ConversationRoleUser, Content: []bedrocktypes.ContentBlock{&bedrocktypes.ContentBlockMemberText{Value: " "}}})
	}
	return system, result, nil
}

func (p *BedrockProvider) convertTools(tools []models.ToolDefinition) (*bedrocktypes.ToolConfiguration, error) {
	result := &bedrocktypes.ToolConfiguration{Tools: make([]bedrocktypes.Tool, 0, len(tools))}
	for _, tool := range tools {
		result.Tools = append(result.Tools, &bedrocktypes.ToolMemberToolSpec{Value: bedrocktypes.ToolSpecification{
			Name:        stringPtr(tool.Function.Name),
			Description: stringPtr(tool.Function.Description),
			InputSchema: &bedrocktypes.ToolInputSchemaMemberJson{Value: bedrockdocument.NewLazyDocument(tool.Function.Parameters)},
		}})
	}
	return result, nil
}

func (p *BedrockProvider) inferenceConfigFromOptions(opts ChatOptions) *bedrocktypes.InferenceConfiguration {
	cfg := &bedrocktypes.InferenceConfiguration{}
	if opts.MaxTokens > 0 {
		cfg.MaxTokens = int32Ptr(int32(opts.MaxTokens))
	}
	if opts.Temperature != nil {
		cfg.Temperature = float32Ptr(float32(*opts.Temperature))
	}
	if opts.TopP != nil {
		cfg.TopP = float32Ptr(float32(*opts.TopP))
	}
	if len(p.config.Stop) > 0 {
		cfg.StopSequences = append([]string(nil), p.config.Stop...)
	}
	if cfg.MaxTokens == nil && cfg.Temperature == nil && cfg.TopP == nil && len(cfg.StopSequences) == 0 {
		return nil
	}
	return cfg
}

func (p *BedrockProvider) responseFromConverse(resp *bedrockruntime.ConverseOutput) (*models.Response, error) {
	if resp == nil {
		return &models.Response{}, nil
	}
	msgOut, ok := resp.Output.(*bedrocktypes.ConverseOutputMemberMessage)
	if !ok || msgOut == nil {
		return &models.Response{}, nil
	}
	result := &models.Response{}
	for _, block := range msgOut.Value.Content {
		switch v := block.(type) {
		case *bedrocktypes.ContentBlockMemberText:
			result.Content += v.Value
		case *bedrocktypes.ContentBlockMemberReasoningContent:
			if reasoning, ok := v.Value.(*bedrocktypes.ReasoningContentBlockMemberReasoningText); ok && reasoning.Value.Text != nil {
				if result.ReasoningContent != "" {
					result.ReasoningContent += "\n"
				}
				result.ReasoningContent += *reasoning.Value.Text
				result.ReasoningMetadata = newBedrockReasoningMetadata(valueOrEmpty(reasoning.Value.Signature))
			}
		case *bedrocktypes.ContentBlockMemberToolUse:
			payload, err := json.Marshal(v.Value.Input)
			if err != nil {
				return nil, fmt.Errorf("marshal bedrock tool input: %w", err)
			}
			result.ToolCalls = append(result.ToolCalls, models.ToolCall{
				ID:   valueOrEmpty(v.Value.ToolUseId),
				Type: "function",
				Function: models.ToolCallFunction{
					Name:      valueOrEmpty(v.Value.Name),
					Arguments: string(payload),
				},
			})
		}
	}
	return result, nil
}

func valueOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func (p *BedrockProvider) GetMaxContextTokens() int {
	return getOpenAICompatibleMaxContextTokens(strings.ToLower(p.resolveModel(p.defaultOptions)))
}

func (p *BedrockProvider) BuiltinModels() []ProviderModel {
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

func (p *BedrockProvider) FetchModels(ctx context.Context) ([]ProviderModel, error) {
	return nil, nil
}

func (p *BedrockProvider) ListModels(ctx context.Context) ([]ProviderModel, error) {
	return listProviderModels(ctx, p)
}

func stringPtr(v string) *string { return &v }

func int32Ptr(v int32) *int32 { return &v }

func float32Ptr(v float32) *float32 { return &v }

var _ Provider = (*BedrockProvider)(nil)
