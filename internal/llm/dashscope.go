package llm

import (
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type DashScopeProvider struct {
	*OpenAICompatibleProvider
}

type dashScopeCompatibleAdapter struct {
	defaultOpenAICompatibleAdapter
}

func NewDashScopeProvider(name string, cfg config.ProviderConfig, decider *proxy.Decider) (Provider, error) {
	provider, err := newOpenAICompatibleProviderWithAdapter(name, cfg, dashScopeCompatibleAdapter{}, decider)
	if err != nil {
		return nil, err
	}
	return &DashScopeProvider{OpenAICompatibleProvider: provider}, nil
}

func isDashScopeProviderType(providerType string) bool {
	switch providerType {
	case "dashscope", "dashscope-cn", "dashscope-coding", "dashscope-coding-cn":
		return true
	default:
		return false
	}
}
