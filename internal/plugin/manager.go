package plugin

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type Manager struct {
	config         config.PluginConfig
	registry       *Registry
	authenticator  Authenticator
	transport      *Transport
	processes      *ProcessManager
	mu             sync.RWMutex
	bridgeCache    map[string]*ChannelBridge
	channelBridges map[string]*ChannelBridge
	hookObservers  map[string][]hookSubscriber
	hookMutators   map[string][]hookSubscriber
	decider        *proxy.Decider
}

type hookSubscriber struct {
	pluginID     string
	capabilityID string
	priority     int
}

type HookDecision struct {
	Block   bool
	Message string
	Patch   map[string]interface{}
}

func NewManager(cfg config.PluginConfig, decider *proxy.Decider) *Manager {
	enabled := enabledPlugins(cfg.Plugins)
	addr := fmt.Sprintf("%s:%d", cfg.WSServer.Host, cfg.WSServer.Port)
	wsURL := fmt.Sprintf("ws://%s/plugin", normalizeLoopbackHost(cfg.WSServer.Host, cfg.WSServer.Port))

	processes := NewProcessManager(wsURL, decider)
	staticTokens := make(map[string]string)
	for pluginID, def := range enabled {
		if def.Type == "remote" && def.Token != "" {
			staticTokens[pluginID] = def.Token
		}
	}

	registry := NewRegistry()
	transport := NewTransport(addr, NewStaticTokenAuthenticator(staticTokens), registry, localPluginIDs(enabled))

	mgr := &Manager{
		config:         cfg,
		registry:       registry,
		authenticator:  transport.authenticator,
		transport:      transport,
		processes:      processes,
		bridgeCache:    make(map[string]*ChannelBridge),
		channelBridges: make(map[string]*ChannelBridge),
		hookObservers:  make(map[string][]hookSubscriber),
		hookMutators:   make(map[string][]hookSubscriber),
		decider:        decider,
	}
	transport.SetManager(mgr)
	return mgr
}

func (m *Manager) Start(ctx context.Context) error {
	enabled := enabledPlugins(m.config.Plugins)
	if len(enabled) == 0 {
		return nil
	}

	startDefs, tokens, err := preparePluginDefs(enabled)
	if err != nil {
		return err
	}
	m.authenticator = NewStaticTokenAuthenticator(tokens)
	m.transport.authenticator = m.authenticator

	if err := m.transport.Start(); err != nil {
		return err
	}
	if err := m.processes.StartAll(ctx, startDefs); err != nil {
		return err
	}

	logger.Info("Plugin manager started", logger.Fields{
		"address": addrFromConfig(m.config.WSServer),
		"plugins": len(enabled),
	})
	return nil
}

func (m *Manager) WaitReady(ctx context.Context, timeout time.Duration) error {
	if len(localPluginIDs(enabledPlugins(m.config.Plugins))) == 0 {
		return nil
	}
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return m.transport.WaitReady(readyCtx)
}

func (m *Manager) Shutdown(ctx context.Context) error {
	if m.processes != nil {
		m.processes.Shutdown(ctx)
	}
	if m.transport != nil {
		return m.transport.Shutdown(ctx)
	}
	return nil
}

func (m *Manager) Registry() *Registry {
	return m.registry
}

func (m *Manager) Statuses() []*Plugin {
	if m.registry == nil {
		return nil
	}
	return m.registry.List()
}

func (m *Manager) ListToolAdapters() []*ToolAdapter {
	plugins := m.registry.List()
	adapters := make([]*ToolAdapter, 0)
	for _, pl := range plugins {
		for _, capability := range pl.Capabilities {
			if capability.Type != CapabilityTypeTool {
				continue
			}
			adapters = append(adapters, NewToolAdapter(m, pl.PluginID, capability))
		}
	}
	return adapters
}

func (m *Manager) ListProviderAdapters() []*ProviderAdapter {
	plugins := m.registry.List()
	adapters := make([]*ProviderAdapter, 0)
	for _, pl := range plugins {
		for _, capability := range pl.Capabilities {
			if capability.Type != CapabilityTypeProvider {
				continue
			}
			adapters = append(adapters, NewProviderAdapter(m, pl.PluginID, capability))
		}
	}
	return adapters
}

func (m *Manager) ListChannelBridges() []*ChannelBridge {
	m.reindexCapabilities()

	m.mu.RLock()
	defer m.mu.RUnlock()
	bridges := make([]*ChannelBridge, 0, len(m.channelBridges))
	for _, bridge := range m.channelBridges {
		bridges = append(bridges, bridge)
	}
	return bridges
}

func (m *Manager) ListHookObservers() map[string][]string {
	m.reindexCapabilities()

	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string][]string)
	for event, subscribers := range m.hookObservers {
		for _, subscriber := range subscribers {
			result[event] = append(result[event], subscriber.capabilityID)
		}
	}
	for event, subscribers := range m.hookMutators {
		for _, subscriber := range subscribers {
			result[event] = append(result[event], subscriber.capabilityID)
		}
	}
	return result
}

func (m *Manager) reindexCapabilities() {
	plugins := m.registry.List()
	indexed := make(map[string]*ChannelBridge)
	cache := make(map[string]*ChannelBridge)
	m.mu.RLock()
	for key, bridge := range m.bridgeCache {
		cache[key] = bridge
	}
	m.mu.RUnlock()

	observers := make(map[string][]hookSubscriber)
	mutators := make(map[string][]hookSubscriber)
	for _, pl := range plugins {
		for _, capability := range pl.Capabilities {
			switch capability.Type {
			case CapabilityTypeChannel:
				bridge := cache[capability.CapabilityID]
				if bridge == nil {
					bridge = NewChannelBridge(m, pl.PluginID, capability)
				}
				cache[capability.CapabilityID] = bridge
				indexed[capability.CapabilityID] = bridge
			case CapabilityTypeHook:
				for _, event := range capability.Events {
					subscriber := hookSubscriber{pluginID: pl.PluginID, capabilityID: capability.CapabilityID, priority: capability.Priority}
					if isMutatingHookEvent(event) {
						mutators[event] = append(mutators[event], subscriber)
					} else {
						observers[event] = append(observers[event], subscriber)
					}
				}
			default:
				continue
			}
		}
	}
	for event := range mutators {
		sort.Slice(mutators[event], func(i, j int) bool {
			return mutators[event][i].priority > mutators[event][j].priority
		})
	}

	m.mu.Lock()
	m.bridgeCache = cache
	m.channelBridges = indexed
	m.hookObservers = observers
	m.hookMutators = mutators
	m.mu.Unlock()
}

func (m *Manager) TriggerObserverHook(ctx context.Context, event string, payload map[string]interface{}) {
	if m == nil || m.transport == nil || strings.TrimSpace(event) == "" {
		return
	}

	m.mu.RLock()
	subscribers := append([]hookSubscriber(nil), m.hookObservers[event]...)
	m.mu.RUnlock()
	if len(subscribers) == 0 {
		return
	}

	for _, subscriber := range subscribers {
		_, err := m.transport.SendRequest(ctx, subscriber.pluginID, MethodHookTrigger, subscriber.capabilityID, map[string]interface{}{
			"event":   event,
			"payload": payload,
		})
		if err != nil {
			logger.Warn("Observer hook dispatch failed", logger.Fields{
				"event":         event,
				"plugin_id":     subscriber.pluginID,
				"capability_id": subscriber.capabilityID,
				"error":         err.Error(),
			})
		}
	}
}

func (m *Manager) TriggerMutatingHook(ctx context.Context, event string, payload map[string]interface{}) HookDecision {
	if m == nil || m.transport == nil || strings.TrimSpace(event) == "" {
		return HookDecision{}
	}

	m.mu.RLock()
	subscribers := append([]hookSubscriber(nil), m.hookMutators[event]...)
	m.mu.RUnlock()
	if len(subscribers) == 0 {
		return HookDecision{}
	}

	for _, subscriber := range subscribers {
		response, err := m.transport.SendRequest(ctx, subscriber.pluginID, MethodHookTrigger, subscriber.capabilityID, map[string]interface{}{
			"event":   event,
			"payload": payload,
		})
		if err != nil {
			logger.Warn("Mutating hook dispatch failed", logger.Fields{
				"event":         event,
				"plugin_id":     subscriber.pluginID,
				"capability_id": subscriber.capabilityID,
				"error":         err.Error(),
			})
			continue
		}

		block, _ := response.Data["block"].(bool)
		patch := map[string]interface{}{}
		if rawPatch, ok := response.Data["patch"].(map[string]interface{}); ok {
			for key, value := range rawPatch {
				patch[key] = value
			}
		}
		if !block {
			if len(patch) > 0 {
				return HookDecision{Patch: patch}
			}
			continue
		}
		message, _ := response.Data["message"].(string)
		return HookDecision{Block: true, Message: message, Patch: patch}
	}

	return HookDecision{}
}

func isMutatingHookEvent(event string) bool {
	switch event {
	case "before_tool_call", "before_llm_call", "before_agent_start", "before_message_send":
		return true
	default:
		return false
	}
}

func (m *Manager) deliverChannelMessage(capabilityID string, msg *models.NormalizedMessage) {
	m.mu.RLock()
	bridge := m.channelBridges[capabilityID]
	m.mu.RUnlock()
	if bridge == nil {
		m.reindexCapabilities()
		m.mu.RLock()
		bridge = m.channelBridges[capabilityID]
		m.mu.RUnlock()
	}
	if bridge == nil {
		logger.Warn("Plugin channel message dropped because bridge is not registered", logger.Fields{"capability_id": capabilityID})
		return
	}
	bridge.deliver(msg)
}

func (m *Manager) ExecuteTool(ctx context.Context, pluginID, capabilityID, toolName string, args map[string]interface{}, requestKey string) (string, error) {
	if m.transport == nil {
		return "", fmt.Errorf("plugin transport is not initialized")
	}

	if timeout := m.requestTimeout(pluginID); timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	response, err := m.transport.SendRequest(ctx, pluginID, MethodToolExecute, capabilityID, map[string]interface{}{
		"tool_name":   toolName,
		"arguments":   args,
		"request_key": requestKey,
	})
	if err != nil {
		return "", err
	}

	if response.Type == FrameTypeError {
		return "", fmt.Errorf("plugin tool error")
	}

	ok, _ := response.Data["ok"].(bool)
	if !ok {
		if message, _ := response.Data["error"].(string); message != "" {
			return "", fmt.Errorf("%s", message)
		}
		return "", fmt.Errorf("plugin tool execution failed: %s", toolName)
	}

	content, _ := response.Data["content"].(string)
	return content, nil
}

func enabledPlugins(defs map[string]config.PluginDef) map[string]config.PluginDef {
	enabled := make(map[string]config.PluginDef)
	for pluginID, def := range defs {
		if def.Enabled {
			enabled[pluginID] = def
		}
	}
	return enabled
}

func localPluginIDs(defs map[string]config.PluginDef) []string {
	ids := make([]string, 0, len(defs))
	for pluginID, def := range defs {
		if def.Type == "local" {
			ids = append(ids, pluginID)
		}
	}
	return ids
}

func normalizeLoopbackHost(host string, port int) string {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" || trimmed == "0.0.0.0" {
		trimmed = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%d", trimmed, port)
}

func addrFromConfig(cfg config.WSServerConfig) string {
	return fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
}

func (m *Manager) requestTimeout(pluginID string) time.Duration {
	def, ok := m.config.Plugins[pluginID]
	if !ok || def.RequestTimeout <= 0 {
		return 0
	}
	return time.Duration(def.RequestTimeout) * time.Millisecond
}

func preparePluginDefs(defs map[string]config.PluginDef) (map[string]config.PluginDef, map[string]string, error) {
	prepared := make(map[string]config.PluginDef, len(defs))
	tokens := make(map[string]string, len(defs))

	for pluginID, def := range defs {
		cloned := def
		token := cloned.Token
		if token == "" && cloned.Type == "local" {
			generated, err := randomToken()
			if err != nil {
				return nil, nil, fmt.Errorf("generate token for plugin %s: %w", pluginID, err)
			}
			token = generated
			cloned.Token = token
		}
		if token != "" {
			tokens[pluginID] = token
		}
		prepared[pluginID] = cloned
	}

	return prepared, tokens, nil
}
