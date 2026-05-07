package plugin

import (
	"fmt"
	"sync"
	"time"
)

type Registry struct {
	mu            sync.RWMutex
	plugins       map[string]*Plugin
	capabilities  map[string]Capability
	toolNames     map[string]string
	providerNames map[string]string
	bridgeNames   map[string]string
}

func NewRegistry() *Registry {
	return &Registry{
		plugins:       make(map[string]*Plugin),
		capabilities:  make(map[string]Capability),
		toolNames:     make(map[string]string),
		providerNames: make(map[string]string),
		bridgeNames:   make(map[string]string),
	}
}

func (r *Registry) Register(reg Registration) (*Plugin, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if reg.PluginID == "" {
		return nil, fmt.Errorf("plugin_id is required")
	}
	if reg.ProtocolVersion != int(CurrentProtocolVersion) {
		return nil, fmt.Errorf("unsupported protocol version: %d", reg.ProtocolVersion)
	}
	if len(reg.Capabilities) == 0 {
		return nil, fmt.Errorf("plugin %s has no capabilities", reg.PluginID)
	}

	for _, capability := range reg.Capabilities {
		if err := r.validateCapabilityLocked(reg.PluginID, capability); err != nil {
			return nil, err
		}
	}

	now := time.Now()
	pl := &Plugin{
		PluginID:     reg.PluginID,
		Name:         reg.Name,
		Version:      reg.PluginVersion,
		Protocol:     ProtocolVersion(reg.ProtocolVersion),
		Capabilities: append([]Capability(nil), reg.Capabilities...),
		Status:       StatusReady,
		ConnectedAt:  now,
		LastSeenAt:   now,
	}

	r.plugins[reg.PluginID] = pl
	for _, capability := range reg.Capabilities {
		r.capabilities[capability.CapabilityID] = capability
		switch capability.Type {
		case CapabilityTypeTool:
			r.toolNames[capability.ToolName] = reg.PluginID
		case CapabilityTypeProvider:
			r.providerNames[capability.ProviderName] = reg.PluginID
		case CapabilityTypeChannel:
			r.bridgeNames[capability.BridgeName] = reg.PluginID
		}
	}

	return pl, nil
}

func (r *Registry) Get(pluginID string) (*Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pl, ok := r.plugins[pluginID]
	if !ok {
		return nil, false
	}
	clone := *pl
	clone.Capabilities = append([]Capability(nil), pl.Capabilities...)
	return &clone, true
}

func (r *Registry) List() []*Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	plugins := make([]*Plugin, 0, len(r.plugins))
	for _, pl := range r.plugins {
		clone := *pl
		clone.Capabilities = append([]Capability(nil), pl.Capabilities...)
		plugins = append(plugins, &clone)
	}
	return plugins
}

func (r *Registry) HasToolName(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.toolNames[name]
	return ok
}

func (r *Registry) SetStatus(pluginID string, status Status) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	pl, ok := r.plugins[pluginID]
	if !ok {
		return false
	}
	pl.Status = status
	pl.LastSeenAt = time.Now()
	return true
}

func (r *Registry) Touch(pluginID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	pl, ok := r.plugins[pluginID]
	if !ok {
		return false
	}
	pl.LastSeenAt = time.Now()
	return true
}

func (r *Registry) validateCapabilityLocked(pluginID string, capability Capability) error {
	if capability.CapabilityID == "" {
		return fmt.Errorf("plugin %s has empty capability_id", pluginID)
	}
	if _, exists := r.capabilities[capability.CapabilityID]; exists {
		return fmt.Errorf("capability already registered: %s", capability.CapabilityID)
	}

	switch capability.Type {
	case CapabilityTypeTool:
		if capability.ToolName == "" {
			return fmt.Errorf("tool capability %s requires tool_name", capability.CapabilityID)
		}
		if owner, exists := r.toolNames[capability.ToolName]; exists {
			return fmt.Errorf("tool name conflict: %s already registered by %s", capability.ToolName, owner)
		}
	case CapabilityTypeProvider:
		if capability.ProviderName == "" {
			return fmt.Errorf("provider capability %s requires provider_name", capability.CapabilityID)
		}
		if owner, exists := r.providerNames[capability.ProviderName]; exists {
			return fmt.Errorf("provider name conflict: %s already registered by %s", capability.ProviderName, owner)
		}
	case CapabilityTypeChannel:
		if capability.BridgeName == "" {
			return fmt.Errorf("channel capability %s requires bridge_name", capability.CapabilityID)
		}
		if owner, exists := r.bridgeNames[capability.BridgeName]; exists {
			return fmt.Errorf("bridge name conflict: %s already registered by %s", capability.BridgeName, owner)
		}
	case CapabilityTypeHook:
		return nil
	default:
		return fmt.Errorf("unsupported capability type: %s", capability.Type)
	}

	return nil
}
