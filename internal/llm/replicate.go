package llm

import (
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type ReplicateProvider struct {
	*OpenAICompatibleProvider
}

type replicateCompatibleAdapter struct {
	defaultOpenAICompatibleAdapter
}

func NewReplicateProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (Provider, error) {
	provider, err := newOpenAICompatibleProviderWithAdapter(name, cfg, replicateCompatibleAdapter{}, decider)
	if err != nil {
		return nil, err
	}
	return &ReplicateProvider{OpenAICompatibleProvider: provider}, nil
}
