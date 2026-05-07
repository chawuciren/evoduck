package llm

import (
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type PerplexityProvider struct {
	*OpenAICompatibleProvider
}

type perplexityCompatibleAdapter struct {
	defaultOpenAICompatibleAdapter
}

func NewPerplexityProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (Provider, error) {
	provider, err := newOpenAICompatibleProviderWithAdapter(name, cfg, perplexityCompatibleAdapter{}, decider)
	if err != nil {
		return nil, err
	}
	return &PerplexityProvider{OpenAICompatibleProvider: provider}, nil
}