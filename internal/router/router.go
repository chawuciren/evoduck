package router

import (
	"fmt"
	"sync"

	"github.com/chawuciren/evoduck/internal/agent"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
)

type Router struct {
	mu             sync.RWMutex
	agentMgr       *agent.Manager
	channels       config.ChannelsConfig
	defaultAgentID string
}

func New(agentMgr *agent.Manager, channels config.ChannelsConfig, defaultAgentID string) *Router {
	r := &Router{
		agentMgr:       agentMgr,
		channels:       channels,
		defaultAgentID: defaultAgentID,
	}

	r.validateBindings()

	return r
}

func (r *Router) validateBindings() {
	for channelID, chCfg := range r.channels {
		if chCfg.Agent == "" {
			logger.Warn("Channel has no agent binding", logger.Fields{
				"channel_id": channelID,
			})
			continue
		}

		ag, err := r.agentMgr.Get(chCfg.Agent)
		if err != nil {
			logger.Warn("Channel bound to non-existent agent", logger.Fields{
				"channel_id": channelID,
				"agent_id":   chCfg.Agent,
				"error":      err.Error(),
			})
			continue
		}

		logger.Info("Channel-Agent binding validated", logger.Fields{
			"channel_id": channelID,
			"agent_id":   chCfg.Agent,
			"agent_role": ag.Role,
		})
	}
}

func (r *Router) RouteByAgentID(agentID string) (*agent.Agent, error) {
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is empty")
	}

	ag, err := r.agentMgr.Get(agentID)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %s", agentID)
	}

	return ag, nil
}

func (r *Router) RouteByChannel(channelID string) (*agent.Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	chCfg, ok := r.channels[channelID]
	if !ok {
		return nil, fmt.Errorf("channel not found: %s", channelID)
	}

	if chCfg.Agent == "" {
		agents := r.agentMgr.List()
		if len(agents) == 0 {
			return nil, fmt.Errorf("no agent available for channel %s", channelID)
		}
		logger.Warn("Channel has no agent binding, using first available", logger.Fields{
			"channel_id":     channelID,
			"fallback_agent": agents[0].ID,
		})
		return agents[0], nil
	}

	ag, err := r.agentMgr.Get(chCfg.Agent)
	if err != nil {
		return nil, fmt.Errorf("agent %s bound to channel %s not found: %w", chCfg.Agent, channelID, err)
	}

	logger.Info("Routing by channel", logger.Fields{
		"channel_id": channelID,
		"agent_id":   ag.ID,
	})

	return ag, nil
}

func (r *Router) RouteByRole(role models.Role) ([]*agent.Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matched []*agent.Agent
	for _, ag := range r.agentMgr.List() {
		if ag.Role == role {
			matched = append(matched, ag)
		}
	}

	if len(matched) == 0 {
		return nil, fmt.Errorf("no agent with role %s available", role)
	}

	logger.Info("Routing by role", logger.Fields{
		"role":        role,
		"agent_count": len(matched),
	})

	return matched, nil
}

func (r *Router) RouteDefault() (*agent.Agent, error) {
	// 优先使用配置的默认 agent
	if r.defaultAgentID != "" {
		ag, err := r.agentMgr.Get(r.defaultAgentID)
		if err == nil {
			logger.Info("Using configured default agent", logger.Fields{
				"agent_id": ag.ID,
				"role":     ag.Role,
			})
			return ag, nil
		}
		logger.Warn("Configured default agent not found, falling back to first agent", logger.Fields{
			"default_agent_id": r.defaultAgentID,
		})
	}

	// 否则使用第一个注册的 agent
	agents := r.agentMgr.List()
	if len(agents) == 0 {
		return nil, fmt.Errorf("no agent available")
	}

	ag := agents[0]
	logger.Warn("Using default routing (first agent)", logger.Fields{
		"agent_id": ag.ID,
		"role":     ag.Role,
	})

	return ag, nil
}

func (r *Router) GetChannelRole(channelID string) (models.Role, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	chCfg, ok := r.channels[channelID]
	if !ok {
		return models.RoleAdmin, fmt.Errorf("channel not found: %s", channelID)
	}

	return models.Role(chCfg.Role), nil
}

func (r *Router) ListChannels() map[string]config.ChannelConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]config.ChannelConfig)
	for k, v := range r.channels {
		result[k] = v
	}
	return result
}
