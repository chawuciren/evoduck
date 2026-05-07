package gateway

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/internal/agent"
	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/plugin"
)

type CapabilityAudit struct {
	Status    string              `json:"status"`
	Summary   CapabilitySummary   `json:"summary"`
	Gateway   CapabilityGateway   `json:"gateway"`
	LLM       CapabilityLLM       `json:"llm"`
	Agents    []CapabilityAgent   `json:"agents"`
	Channels  []CapabilityChannel `json:"channels"`
	Memory    CapabilityMemory    `json:"memory"`
	Scheduler CapabilityScheduler `json:"scheduler"`
	Plugins   CapabilityPlugins   `json:"plugins"`
	MCP       CapabilityMCP       `json:"mcp"`
	Warnings  []string            `json:"warnings"`
	Errors    []string            `json:"errors"`
}

type CapabilitySummary struct {
	AgentCount             int `json:"agent_count"`
	ProviderCount          int `json:"provider_count"`
	ConfiguredChannelCount int `json:"configured_channel_count"`
	RegisteredBridgeCount  int `json:"registered_bridge_count"`
	PluginChannelCount     int `json:"plugin_channel_count"`
	WSConnectionCount      int `json:"ws_connection_count"`
}

type CapabilityGateway struct {
	DefaultAgent      string `json:"default_agent"`
	ResolvedAgent     string `json:"resolved_agent"`
	ResolvedAgentOK   bool   `json:"resolved_agent_ok"`
	ConfigPath        string `json:"config_path"`
	DataDir           string `json:"data_dir"`
	Uptime            string `json:"uptime"`
	WebchatGateway    bool   `json:"webchat_gateway"`
	SlashCommands     bool   `json:"slash_commands"`
	ChannelsStarted   bool   `json:"channels_started"`
	MediaStoreEnabled bool   `json:"media_store_enabled"`
}

type CapabilityLLM struct {
	DefaultProvider string                  `json:"default_provider"`
	DefaultModel    string                  `json:"default_model"`
	Providers       []CapabilityLLMProvider `json:"providers"`
}

type CapabilityLLMProvider struct {
	Name       string `json:"name"`
	IsDefault  bool   `json:"is_default"`
	Status     string `json:"status"`
	ModelCount int    `json:"model_count"`
	Error      string `json:"error,omitempty"`
}

type CapabilityAgent struct {
	ID                   string                `json:"id"`
	Role                 string                `json:"role"`
	Workspace            string                `json:"workspace"`
	Provider             string                `json:"provider"`
	Model                string                `json:"model"`
	Status               string                `json:"status"`
	RuntimeReady         bool                  `json:"runtime_ready"`
	MemoryManagerReady   bool                  `json:"memory_manager_ready"`
	UserIsolationEnabled bool                  `json:"user_isolation_enabled"`
	SupportsVision       bool                  `json:"supports_vision"`
	ToolCount            int                   `json:"tool_count"`
	SkillCount           int                   `json:"skill_count"`
	Tools                []CapabilityAgentTool `json:"tools"`
	Warnings             []string              `json:"warnings,omitempty"`
}

type CapabilityAgentTool struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

type CapabilityChannel struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Kind       string `json:"kind"`
	Agent      string `json:"agent"`
	Role       string `json:"role"`
	Registered bool   `json:"registered"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
}

type CapabilityMemory struct {
	FlusherReady bool `json:"flusher_ready"`
}

type CapabilityScheduler struct {
	CronReady          bool `json:"cron_ready"`
	ServiceReady       bool `json:"service_ready"`
	RegisteredJobCount int  `json:"registered_job_count"`
}

type CapabilityPlugins struct {
	Enabled              bool                     `json:"enabled"`
	ConnectedPluginCount int                      `json:"connected_plugin_count"`
	ToolAdapterCount     int                      `json:"tool_adapter_count"`
	ProviderAdapterCount int                      `json:"provider_adapter_count"`
	ChannelBridgeCount   int                      `json:"channel_bridge_count"`
	HookEventCount       int                      `json:"hook_event_count"`
	Items                []CapabilityPluginStatus `json:"items"`
}

type CapabilityPluginStatus struct {
	PluginID        string `json:"plugin_id"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	CapabilityCount int    `json:"capability_count"`
	ToolCount       int    `json:"tool_count"`
	ProviderCount   int    `json:"provider_count"`
	ChannelCount    int    `json:"channel_count"`
	HookCount       int    `json:"hook_count"`
	LastSeenAt      int64  `json:"last_seen_at"`
	ConnectedAt     int64  `json:"connected_at"`
}

type CapabilityMCP struct {
	Initialized bool                  `json:"initialized"`
	ClientCount int                   `json:"client_count"`
	ToolCount   int                   `json:"tool_count"`
	Clients     []CapabilityMCPClient `json:"clients"`
}

type CapabilityMCPClient struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	ToolCount int    `json:"tool_count"`
	Server    string `json:"server,omitempty"`
	Version   string `json:"version,omitempty"`
}

func (g *Gateway) GetCapabilityAudit() *CapabilityAudit {
	audit := &CapabilityAudit{
		Status:   "ok",
		Warnings: make([]string, 0),
		Errors:   make([]string, 0),
	}

	resolvedDefaultAgent := strings.TrimSpace(g.GetDefaultAgentID())
	configuredDefaultAgent := strings.TrimSpace(g.currentConfig().DefaultAgent)
	_, resolvedAgentErr := g.agentMgr.Get(resolvedDefaultAgent)
	audit.Gateway = CapabilityGateway{
		DefaultAgent:      configuredDefaultAgent,
		ResolvedAgent:     resolvedDefaultAgent,
		ResolvedAgentOK:   resolvedDefaultAgent != "" && resolvedAgentErr == nil,
		ConfigPath:        g.configPath,
		DataDir:           g.currentConfig().DataDir,
		Uptime:            time.Since(g.startTime).String(),
		WebchatGateway:    true,
		SlashCommands:     g.slashHandler != nil,
		ChannelsStarted:   g.channelsStarted,
		MediaStoreEnabled: g.mediaStore != nil,
	}
	if !audit.Gateway.ResolvedAgentOK {
		audit.Errors = append(audit.Errors, "default agent is not resolvable")
	}

	audit.buildLLMSection(g.llmReg)
	audit.buildAgentSection(g.agentMgr.List(), g.llmReg)
	audit.buildChannelSection(g)
	audit.buildMemorySection(g)
	audit.buildSchedulerSection(g)
	audit.buildPluginSection(g)
	audit.buildMCPSection(g)

	g.wsConnMu.RLock()
	audit.Summary.WSConnectionCount = len(g.wsConns)
	g.wsConnMu.RUnlock()
	audit.Summary.AgentCount = len(audit.Agents)
	audit.Summary.ProviderCount = len(audit.LLM.Providers)
	audit.Summary.ConfiguredChannelCount = len(audit.Channels)
	audit.Summary.RegisteredBridgeCount = len(g.channelMgr.List())
	audit.Summary.PluginChannelCount = audit.Plugins.ChannelBridgeCount

	if len(audit.Errors) > 0 {
		audit.Status = "critical"
	} else if len(audit.Warnings) > 0 {
		audit.Status = "warning"
	}

	return audit
}

func (a *CapabilityAudit) buildLLMSection(reg *llm.Registry) {
	a.LLM = CapabilityLLM{Providers: []CapabilityLLMProvider{}}
	if reg == nil {
		a.Errors = append(a.Errors, "llm registry is not initialized")
		return
	}
	a.LLM.DefaultProvider = reg.DefaultProviderName()
	a.LLM.DefaultModel = reg.DefaultModelName()
	for _, name := range reg.ListProviderNames() {
		item := CapabilityLLMProvider{Name: name, IsDefault: name == a.LLM.DefaultProvider, Status: "ok"}
		models, err := reg.ListModels(context.Background(), name)
		if err != nil {
			item.Status = "warning"
			item.Error = err.Error()
			a.Warnings = append(a.Warnings, "provider "+name+": "+err.Error())
		} else {
			item.ModelCount = len(models)
			if len(models) == 0 {
				item.Status = "warning"
				item.Error = "no models available"
				a.Warnings = append(a.Warnings, "provider "+name+": no models available")
			}
		}
		a.LLM.Providers = append(a.LLM.Providers, item)
	}
}

func (a *CapabilityAudit) buildAgentSection(agents []*agent.Agent, reg *llm.Registry) {
	a.Agents = make([]CapabilityAgent, 0, len(agents))
	for _, ag := range agents {
		if ag == nil {
			continue
		}
		item := CapabilityAgent{
			ID:                   ag.ID,
			Role:                 string(ag.Role),
			Workspace:            ag.Config.Workspace,
			Provider:             ag.Config.Provider,
			Model:                ag.Config.Model,
			RuntimeReady:         ag.Runtime != nil,
			MemoryManagerReady:   ag.MemoryManager != nil,
			UserIsolationEnabled: ag.UserIsolation.AutoCreate || ag.UserIsolation.AutoProfile,
			Warnings:             make([]string, 0),
		}
		if reg != nil {
			models, err := reg.ListModels(context.Background(), ag.Config.Provider)
			if err == nil {
				for _, model := range models {
					if model.ID == ag.Config.Model {
						item.SupportsVision = model.SupportsVision
						break
					}
				}
			}
		}
		if ag.Tools != nil {
			toolDefs := ag.Tools.List()
			item.ToolCount = len(toolDefs)
			item.Tools = make([]CapabilityAgentTool, 0, len(toolDefs))
			for _, tool := range toolDefs {
				item.Tools = append(item.Tools, CapabilityAgentTool{
					Name:   tool.Function.Name,
					Source: classifyToolSource(tool.Function.Name),
				})
			}
			sort.Slice(item.Tools, func(i, j int) bool { return item.Tools[i].Name < item.Tools[j].Name })
		}
		if ag.Skills != nil {
			item.SkillCount = len(ag.Skills.List())
		}
		item.Status = "ok"
		if !item.RuntimeReady {
			item.Status = "critical"
			item.Warnings = append(item.Warnings, "runtime is not initialized")
		} else if !item.MemoryManagerReady {
			item.Status = "warning"
			item.Warnings = append(item.Warnings, "memory manager is not initialized")
		}
		if item.SkillCount == 0 {
			item.Warnings = append(item.Warnings, "no skills loaded")
			if item.Status == "ok" {
				item.Status = "warning"
			}
		}
		if len(item.Warnings) > 0 {
			a.Warnings = append(a.Warnings, "agent "+ag.ID+": "+strings.Join(item.Warnings, "; "))
		}
		a.Agents = append(a.Agents, item)
	}
	sort.Slice(a.Agents, func(i, j int) bool { return a.Agents[i].ID < a.Agents[j].ID })
}

func (a *CapabilityAudit) buildChannelSection(g *Gateway) {
	channelIDs := make([]string, 0, len(g.currentConfig().Channels))
	for channelID := range g.currentConfig().Channels {
		channelIDs = append(channelIDs, channelID)
	}
	sort.Strings(channelIDs)
	a.Channels = make([]CapabilityChannel, 0, len(channelIDs))
	registered := make(map[string]bool)
	for _, name := range g.channelMgr.List() {
		registered[name] = true
	}
	for _, channelID := range channelIDs {
		cfg := g.currentConfig().Channels[channelID]
		item := CapabilityChannel{
			ID:         channelID,
			Type:       cfg.Type,
			Agent:      cfg.Agent,
			Role:       cfg.Role,
			Registered: registered[channelID],
			Status:     "ok",
		}
		if strings.EqualFold(strings.TrimSpace(cfg.Type), "webchat") {
			item.Kind = "gateway_web"
			item.Status = "ok"
			item.Message = "Handled by the gateway web interface, not by channel bridge registration"
		} else if registered[channelID] {
			item.Kind = "bridge"
			item.Message = "Channel bridge registered"
		} else {
			item.Kind = "bridge"
			item.Status = "warning"
			item.Message = "Channel configured but no bridge registered"
			a.Warnings = append(a.Warnings, "channel "+channelID+": no bridge registered")
		}
		a.Channels = append(a.Channels, item)
	}
}

func (a *CapabilityAudit) buildMemorySection(g *Gateway) {
	a.Memory = CapabilityMemory{
		FlusherReady: g.agentMgr != nil,
	}
}

func (a *CapabilityAudit) buildSchedulerSection(g *Gateway) {
	jobCount := 0
	if g.scheduler != nil {
		jobCount = len(g.scheduler.ListJobs())
	}
	a.Scheduler = CapabilityScheduler{
		CronReady:          g.scheduler != nil,
		ServiceReady:       g.schedulerService != nil,
		RegisteredJobCount: jobCount,
	}
	if !a.Scheduler.CronReady {
		a.Warnings = append(a.Warnings, "scheduler cron is not initialized")
	}
}

func (a *CapabilityAudit) buildPluginSection(g *Gateway) {
	if g.pluginManager == nil {
		a.Plugins = CapabilityPlugins{Enabled: false, Items: []CapabilityPluginStatus{}}
		return
	}
	plugStatuses := g.pluginManager.Statuses()
	hooks := g.pluginManager.ListHookObservers()
	a.Plugins = CapabilityPlugins{
		Enabled:              true,
		ConnectedPluginCount: len(plugStatuses),
		ToolAdapterCount:     len(g.pluginManager.ListToolAdapters()),
		ProviderAdapterCount: len(g.pluginManager.ListProviderAdapters()),
		ChannelBridgeCount:   len(g.pluginManager.ListChannelBridges()),
		HookEventCount:       len(hooks),
		Items:                make([]CapabilityPluginStatus, 0, len(plugStatuses)),
	}
	for _, pl := range plugStatuses {
		if pl == nil {
			continue
		}
		item := CapabilityPluginStatus{
			PluginID:        pl.PluginID,
			Name:            pl.Name,
			Status:          string(pl.Status),
			CapabilityCount: len(pl.Capabilities),
			ConnectedAt:     pl.ConnectedAt.Unix(),
			LastSeenAt:      pl.LastSeenAt.Unix(),
		}
		for _, cap := range pl.Capabilities {
			switch cap.Type {
			case plugin.CapabilityTypeTool:
				item.ToolCount++
			case plugin.CapabilityTypeProvider:
				item.ProviderCount++
			case plugin.CapabilityTypeChannel:
				item.ChannelCount++
			case plugin.CapabilityTypeHook:
				item.HookCount++
			}
		}
		a.Plugins.Items = append(a.Plugins.Items, item)
	}
	sort.Slice(a.Plugins.Items, func(i, j int) bool { return a.Plugins.Items[i].PluginID < a.Plugins.Items[j].PluginID })
}

func (a *CapabilityAudit) buildMCPSection(g *Gateway) {
	if g.agentMgr == nil {
		a.MCP = CapabilityMCP{Clients: []CapabilityMCPClient{}}
		return
	}
	manager := g.agentMgr.GetMCPManager()
	if manager == nil {
		a.MCP = CapabilityMCP{Initialized: false, Clients: []CapabilityMCPClient{}}
		return
	}
	clients := manager.GetAllClients()
	a.MCP = CapabilityMCP{
		Initialized: true,
		ClientCount: len(clients),
		ToolCount:   len(manager.GetAllTools()),
		Clients:     make([]CapabilityMCPClient, 0, len(clients)),
	}
	for name, client := range clients {
		if client == nil {
			continue
		}
		serverInfo := client.GetServerInfo()
		a.MCP.Clients = append(a.MCP.Clients, CapabilityMCPClient{
			Name:      name,
			Connected: client.IsConnected(),
			ToolCount: len(client.GetAllTools()),
			Server:    serverInfo.Name,
			Version:   serverInfo.Version,
		})
	}
	sort.Slice(a.MCP.Clients, func(i, j int) bool { return a.MCP.Clients[i].Name < a.MCP.Clients[j].Name })
}

func classifyToolSource(name string) string {
	if strings.HasPrefix(name, "sessions_") {
		return "session"
	}
	if strings.HasPrefix(name, "schedule_") {
		return "schedule"
	}
	if strings.HasPrefix(name, "skill_") || name == "skill" {
		return "skill"
	}
	if name == "task_plan" {
		return "builtin"
	}
	if strings.HasPrefix(name, "browser_") || strings.HasPrefix(name, "one_") || strings.HasSuffix(name, "_exa") {
		return "mcp"
	}
	if strings.HasPrefix(name, "plugin_") {
		return "plugin"
	}
	return "builtin"
}
