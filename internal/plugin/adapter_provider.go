package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/google/uuid"
)

type ProviderAdapter struct {
	pluginID     string
	capabilityID string
	providerName string
	models       []llm.ProviderModel
	defaultOpts  llm.ChatOptions
	manager      *Manager
}

func NewProviderAdapter(manager *Manager, pluginID string, capability Capability) *ProviderAdapter {
	models := make([]llm.ProviderModel, 0, len(capability.Models))
	for _, model := range capability.Models {
		models = append(models, llm.ProviderModel{
			ID:                model.ID,
			Name:              model.Name,
			ContextWindow:     model.ContextWindow,
			MaxTokens:         model.MaxTokens,
			SupportsTools:     model.SupportsTools,
			SupportsStreaming: model.SupportsStreaming,
			SupportsVision:    model.SupportsVision,
			Reasoning:         model.Reasoning,
		})
	}
	return &ProviderAdapter{
		pluginID:     pluginID,
		capabilityID: capability.CapabilityID,
		providerName: capability.ProviderName,
		models:       models,
		manager:      manager,
	}
}

func (p *ProviderAdapter) Name() string { return p.providerName }

func (p *ProviderAdapter) Chat(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error) {
	stream, err := p.ChatStream(ctx, messages, tools)
	if err != nil {
		return nil, err
	}
	response := &models.Response{}
	for event := range stream {
		switch event.Type {
		case "content":
			response.Content += event.Content
		case "tool_calls":
			response.ToolCalls = append(response.ToolCalls, event.ToolCalls...)
		case "error":
			return nil, event.Error
		}
	}
	return response, nil
}

func (p *ProviderAdapter) ChatStream(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (<-chan models.StreamEvent, error) {
	if p.manager == nil || p.manager.transport == nil {
		return nil, fmt.Errorf("plugin transport is not initialized")
	}
	var cancel context.CancelFunc
	if timeout := p.manager.requestTimeout(p.pluginID); timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	}

	requestID := uuid.NewString()
	payload, err := buildProviderPayload(messages, tools, p.defaultOpts)
	if err != nil {
		return nil, err
	}

	frames, err := p.manager.transport.SendStreamingRequest(ctx, p.pluginID, requestID, MethodProviderChat, p.capabilityID, payload)
	if err != nil {
		return nil, err
	}

	out := make(chan models.StreamEvent, 32)
	go func() {
		if cancel != nil {
			defer cancel()
		}
		defer close(out)
		for frame := range frames {
			eventType, _ := frame.Data["event_type"].(string)
			switch eventType {
			case "content":
				content, _ := frame.Data["content"].(string)
				out <- models.StreamEvent{Type: "content", Content: content}
			case "tool_calls":
				toolCalls, err := decodeToolCalls(frame.Data["tool_calls"])
				if err != nil {
					out <- models.StreamEvent{Type: "error", Done: true, Error: err}
					return
				}
				out <- models.StreamEvent{Type: "tool_calls", ToolCalls: toolCalls}
			case "stop":
				out <- models.StreamEvent{Type: "stop", Done: true}
				return
			case "error":
				message, _ := frame.Data["error"].(string)
				out <- models.StreamEvent{Type: "error", Done: true, Error: fmt.Errorf("%s", message)}
				return
			default:
				if frame.Type == FrameTypeResponse {
					if ok, _ := frame.Data["ok"].(bool); !ok {
						message, _ := frame.Data["error"].(string)
						if message == "" {
							message = "provider request failed"
						}
						out <- models.StreamEvent{Type: "error", Done: true, Error: fmt.Errorf("%s", message)}
						return
					}
				}
			}
		}
	}()
	return out, nil
}

func (p *ProviderAdapter) ChatWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, opts llm.ChatOptions) (*models.Response, error) {
	previous := p.defaultOpts
	p.defaultOpts = opts
	defer func() { p.defaultOpts = previous }()
	return p.Chat(ctx, messages, tools)
}

func (p *ProviderAdapter) SetDefaultOptions(opts llm.ChatOptions) { p.defaultOpts = opts }
func (p *ProviderAdapter) GetMaxContextTokens() int {
	maxTokens := 0
	for _, model := range p.models {
		if model.ContextWindow > maxTokens {
			maxTokens = model.ContextWindow
		}
	}
	if maxTokens == 0 {
		return 128000
	}
	return maxTokens
}
func (p *ProviderAdapter) BuiltinModels() []llm.ProviderModel {
	return append([]llm.ProviderModel(nil), p.models...)
}
func (p *ProviderAdapter) FetchModels(ctx context.Context) ([]llm.ProviderModel, error) {
	return p.BuiltinModels(), nil
}
func (p *ProviderAdapter) ListModels(ctx context.Context) ([]llm.ProviderModel, error) {
	return p.BuiltinModels(), nil
}

func buildProviderPayload(messages []models.Message, tools []models.ToolDefinition, opts llm.ChatOptions) (map[string]interface{}, error) {
	payload := map[string]interface{}{
		"messages": messages,
		"tools":    tools,
		"stream":   true,
		"options": map[string]interface{}{
			"model": opts.Model,
		},
	}
	if opts.Temperature != nil {
		payload["options"].(map[string]interface{})["temperature"] = *opts.Temperature
	}
	if opts.MaxTokens > 0 {
		payload["options"].(map[string]interface{})["max_tokens"] = opts.MaxTokens
	}
	if opts.TopP != nil {
		payload["options"].(map[string]interface{})["top_p"] = *opts.TopP
	}
	return payload, nil
}

func decodeToolCalls(raw interface{}) ([]models.ToolCall, error) {
	if raw == nil {
		return nil, nil
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var toolCalls []models.ToolCall
	if err := json.Unmarshal(buf, &toolCalls); err != nil {
		return nil, err
	}
	return toolCalls, nil
}

var _ llm.Provider = (*ProviderAdapter)(nil)
