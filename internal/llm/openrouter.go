package llm

import (
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type OpenRouterProvider struct {
	*OpenAICompatibleProvider
}

type openRouterCompatibleAdapter struct {
	defaultOpenAICompatibleAdapter
}

func NewOpenRouterProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (Provider, error) {
	provider, err := newOpenAICompatibleProviderWithAdapter(name, cfg, openRouterCompatibleAdapter{}, decider)
	if err != nil {
		return nil, err
	}
	return &OpenRouterProvider{OpenAICompatibleProvider: provider}, nil
}
