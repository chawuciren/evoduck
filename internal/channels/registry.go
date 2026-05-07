package channels

import (
	"fmt"
	"sync"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type Factory func(channelID string, cfg config.ChannelConfig, decider *proxy.Decider) (Bridge, error)

var (
	factoriesMu sync.RWMutex
	factories   = make(map[string]Factory)
)

func RegisterFactory(channelType string, factory Factory) {
	factoriesMu.Lock()
	defer factoriesMu.Unlock()
	factories[channelType] = factory
}

func NewBridge(channelID string, cfg config.ChannelConfig, decider *proxy.Decider) (Bridge, error) {
	factoriesMu.RLock()
	factory, ok := factories[cfg.Type]
	factoriesMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown channel type: %s", cfg.Type)
	}

	return factory(channelID, cfg, decider)
}