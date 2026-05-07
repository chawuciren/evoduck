package llm

import (
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type MistralProvider struct {
	*OpenAICompatibleProvider
}

type mistralCompatibleAdapter struct {
	defaultOpenAICompatibleAdapter
}

func NewMistralProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (Provider, error) {
	provider, err := newOpenAICompatibleProviderWithAdapter(name, cfg, mistralCompatibleAdapter{}, decider)
	if err != nil {
		return nil, err
	}
	return &MistralProvider{OpenAICompatibleProvider: provider}, nil
}