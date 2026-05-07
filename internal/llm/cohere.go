package llm

import (
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type CohereProvider struct {
	*OpenAICompatibleProvider
}

type cohereCompatibleAdapter struct {
	defaultOpenAICompatibleAdapter
}

func NewCohereProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (Provider, error) {
	provider, err := newOpenAICompatibleProviderWithAdapter(name, cfg, cohereCompatibleAdapter{}, decider)
	if err != nil {
		return nil, err
	}
	return &CohereProvider{OpenAICompatibleProvider: provider}, nil
}
