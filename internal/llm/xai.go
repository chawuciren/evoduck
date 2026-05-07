package llm

import (
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type XAIProvider struct {
	*OpenAICompatibleProvider
}

type xaiCompatibleAdapter struct {
	defaultOpenAICompatibleAdapter
}

func NewXAIProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (Provider, error) {
	provider, err := newOpenAICompatibleProviderWithAdapter(name, cfg, xaiCompatibleAdapter{}, decider)
	if err != nil {
		return nil, err
	}
	return &XAIProvider{OpenAICompatibleProvider: provider}, nil
}