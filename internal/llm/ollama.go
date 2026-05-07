package llm

import (
	"context"
	"strings"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type OllamaProvider struct {
	*OpenAICompatibleProvider
}

func NewOllamaProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (*OllamaProvider, error) {
	cfg.Type = "openai-compatible"
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:11434/v1"
	}
	if cfg.DefaultModel == "" {
		cfg.DefaultModel = "qwen2.5"
	}
	if len(cfg.Models) == 0 && cfg.DefaultModel != "" {
		cfg.Models = []config.ProviderModelConfig{{ID: cfg.DefaultModel, Name: cfg.DefaultModel, Type: config.ProviderModelTypeChat}}
	}

	provider, err := NewOpenAICompatibleProvider(name, cfg, decider)
	if err != nil {
		return nil, err
	}

	compatible, ok := provider.(*OpenAICompatibleProvider)
	if !ok {
		return nil, config.ValidationError{Field: name, Message: "ollama provider must wrap openai-compatible provider"}
	}
	return &OllamaProvider{OpenAICompatibleProvider: compatible}, nil
}

func (p *OllamaProvider) GetMaxContextTokens() int {
	if p == nil || p.OpenAICompatibleProvider == nil {
		return 32768
	}
	model := strings.ToLower(p.OpenAICompatibleProvider.resolveModel(p.OpenAICompatibleProvider.defaultOptions))
	if strings.Contains(model, "llama3") || strings.Contains(model, "qwen") || strings.Contains(model, "mistral") || strings.Contains(model, "gemma") {
		return 128000
	}
	return 32768
}

func (p *OllamaProvider) BuiltinModels() []ProviderModel {
	if p == nil || p.OpenAICompatibleProvider == nil {
		return nil
	}
	models := builtinModelsFromConfig(p.config.DefaultModel, p.config.Models)
	for i := range models {
		models[i].SupportsTools = true
		models[i].SupportsStreaming = true
		models[i].ContextWindow = ollamaMaxContextTokens(models[i].ID)
	}
	return models
}

func (p *OllamaProvider) ListModels(ctx context.Context) ([]ProviderModel, error) {
	return listProviderModels(ctx, p)
}

func ollamaMaxContextTokens(model string) int {
	model = strings.ToLower(model)
	if strings.Contains(model, "llama3") || strings.Contains(model, "qwen") || strings.Contains(model, "mistral") || strings.Contains(model, "gemma") {
		return 128000
	}
	return 32768
}

var _ Provider = (*OllamaProvider)(nil)
