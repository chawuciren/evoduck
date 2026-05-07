package llm

import (
	"context"
	"strings"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type DeepSeekProvider struct {
	*OpenAICompatibleProvider
}

type deepSeekCompatibleAdapter struct{}

func NewDeepSeekProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (Provider, error) {
	provider, err := newOpenAICompatibleProviderWithAdapter(name, cfg, deepSeekCompatibleAdapter{}, decider)
	if err != nil {
		return nil, err
	}
	return &DeepSeekProvider{OpenAICompatibleProvider: provider}, nil
}

func (p *DeepSeekProvider) ListModels(ctx context.Context) ([]ProviderModel, error) {
	return listProviderModels(ctx, p)
}

func (deepSeekCompatibleAdapter) ConvertMessages(p *OpenAICompatibleProvider, messages []models.Message) ([]openAIChatMessage, error) {
	return p.convertMessagesWithReasoningPolicy(messages, deepSeekReasoningReplayPolicy(p.config.ReasoningReplay))
}

func (deepSeekCompatibleAdapter) ApplyConfig(req *openAICompatibleRequest, cfg config.ProviderConfig) {
	applyDeepSeekProviderConfig(req, cfg)
}

func applyDeepSeekProviderConfig(req *openAICompatibleRequest, cfg config.ProviderConfig) {
	if len(cfg.Stop) > 0 {
		req.Stop = append([]string(nil), cfg.Stop...)
	}
	if cfg.MaxCompletionTokens > 0 {
		req.MaxCompletionTokens = cfg.MaxCompletionTokens
	}
	if cfg.ReasoningEffort != "" {
		req.ReasoningEffort = normalizeDeepSeekReasoningEffort(cfg.ReasoningEffort)
	}
	if cfg.Thinking != nil && strings.TrimSpace(cfg.Thinking.Type) != "" {
		req.Extra = ensureOpenAICompatibleExtra(req.Extra)
		req.Extra["thinking"] = map[string]string{"type": strings.TrimSpace(strings.ToLower(cfg.Thinking.Type))}
	}
	if cfg.Verbosity != "" {
		req.Verbosity = cfg.Verbosity
	}
	if cfg.User != "" {
		req.Extra = ensureOpenAICompatibleExtra(req.Extra)
		req.Extra["user_id"] = cfg.User
	}
	if cfg.UserID != "" {
		req.Extra = ensureOpenAICompatibleExtra(req.Extra)
		req.Extra["user_id"] = cfg.UserID
	}
	if cfg.SafetyIdentifier != "" {
		req.SafetyIdentifier = cfg.SafetyIdentifier
	}
	if cfg.ServiceTier != "" {
		req.ServiceTier = cfg.ServiceTier
	}
	if cfg.N > 0 {
		req.N = cfg.N
	}
	if cfg.Seed != nil {
		req.Seed = cfg.Seed
	}
	if cfg.LogProbs {
		req.LogProbs = true
	}
	if cfg.TopLogProbs > 0 {
		req.TopLogProbs = cfg.TopLogProbs
		req.LogProbs = true
	}
	if cfg.Store != nil {
		req.Store = cfg.Store
	}
	if len(cfg.Metadata) > 0 {
		req.Metadata = cloneStringMap(cfg.Metadata)
	}
	if len(cfg.ChatTemplateKwargs) > 0 {
		req.ChatTemplateKwargs = cloneAnyMap(cfg.ChatTemplateKwargs)
	}
	if len(cfg.ResponseFormat) > 0 {
		req.ResponseFormat = cloneAnyMap(cfg.ResponseFormat)
	}
	if cfg.ToolChoice != "" {
		switch cfg.ToolChoice {
		case "auto", "none", "required":
			req.ToolChoice = cfg.ToolChoice
		default:
			req.ToolChoice = map[string]any{
				"type":     "function",
				"function": map[string]any{"name": cfg.ToolChoice},
			}
		}
	}
}

func ensureOpenAICompatibleExtra(extra map[string]any) map[string]any {
	if extra == nil {
		return make(map[string]any)
	}
	return extra
}

func deepSeekReasoningReplayPolicy(value string) reasoningReplayPolicy {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "none":
		return reasoningReplayNone
	case "all":
		return reasoningReplayAll
	case "tool_calls_only", "":
		return reasoningReplayToolCallsOnly
	default:
		return reasoningReplayToolCallsOnly
	}
}

func normalizeDeepSeekReasoningEffort(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "low", "medium":
		return "high"
	case "xhigh":
		return "max"
	default:
		return strings.TrimSpace(strings.ToLower(value))
	}
}
