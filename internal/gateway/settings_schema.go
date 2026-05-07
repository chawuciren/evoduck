package gateway

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const settingsSecretMask = "********"

type SettingsFieldSchema struct {
	Path           string   `json:"path"`
	Label          string   `json:"label"`
	Type           string   `json:"type"`
	Description    string   `json:"description,omitempty"`
	Section        string   `json:"section,omitempty"`
	Sensitive      bool     `json:"sensitive,omitempty"`
	RestartRequired bool    `json:"restart_required,omitempty"`
	Enum           []string `json:"enum,omitempty"`
}

type SettingsSectionSchema struct {
	ID          string                `json:"id"`
	Label       string                `json:"label"`
	Description string                `json:"description,omitempty"`
	Fields      []SettingsFieldSchema `json:"fields,omitempty"`
}

func settingsSchema() []SettingsSectionSchema {
	return []SettingsSectionSchema{
		{
			ID:          "gateway",
			Label:       "Gateway",
			Description: "Gateway listener and auth settings",
			Fields: []SettingsFieldSchema{
				{Path: "gateway.host", Label: "Host", Type: "text", Section: "gateway", RestartRequired: true},
				{Path: "gateway.port", Label: "Port", Type: "number", Section: "gateway", RestartRequired: true},
				{Path: "gateway.token", Label: "Token", Type: "secret", Section: "gateway", Sensitive: true, RestartRequired: true},
			},
		},
		{
			ID:          "general",
			Label:       "General",
			Description: "Shared runtime defaults and storage paths",
			Fields: []SettingsFieldSchema{
				{Path: "default_agent", Label: "Default Agent", Type: "text", Section: "general"},
				{Path: "data_dir", Label: "Data Directory", Type: "text", Section: "general"},
				{Path: "shared.skills_dir", Label: "Shared Skills Directory", Type: "text", Section: "general"},
			},
		},
		{
			ID:          "agents",
			Label:       "Agents",
			Description: "Agent registry and runtime options",
			Fields: []SettingsFieldSchema{{Path: "agents", Label: "Agents", Type: "map", Section: "agents", RestartRequired: true}},
		},
		{
			ID:          "llm",
			Label:       "LLM",
			Description: "Provider selection and model defaults",
			Fields: []SettingsFieldSchema{
				{Path: "llm.default_provider", Label: "Default Provider", Type: "text", Section: "llm", RestartRequired: true},
				{Path: "llm.default_model", Label: "Default Model", Type: "text", Section: "llm", RestartRequired: true},
				{Path: "llm.providers", Label: "Providers", Type: "map", Section: "llm", RestartRequired: true},
			},
		},
		{
			ID:          "channels",
			Label:       "Channels",
			Description: "External channel integrations",
			Fields: []SettingsFieldSchema{{Path: "channels", Label: "Channels", Type: "map", Section: "channels", RestartRequired: true}},
		},
		{
			ID:          "plugins",
			Label:       "Plugins",
			Description: "Plugin server and registry",
			Fields: []SettingsFieldSchema{
				{Path: "plugins.ws_server", Label: "WebSocket Server", Type: "object", Section: "plugins", RestartRequired: true},
				{Path: "plugins.plugins", Label: "Plugins", Type: "map", Section: "plugins", RestartRequired: true},
			},
		},
		{
			ID:          "tools",
			Label:       "Tools",
			Description: "Backend tool endpoints and auth",
			Fields: []SettingsFieldSchema{
				{Path: "tools.backend_call.endpoints", Label: "Backend Endpoints", Type: "map", Section: "tools", RestartRequired: true},
				{Path: "tools.session", Label: "Session Tools", Type: "object", Section: "tools", RestartRequired: true},
			},
		},
		{
			ID:          "memory",
			Label:       "Memory",
			Description: "Short-term, medium-term, long-term, core, and bootstrap memory settings",
			Fields: []SettingsFieldSchema{{Path: "memory", Label: "Memory", Type: "object", Section: "memory"}},
		},
		{
			ID:          "scheduler",
			Label:       "Scheduler",
			Description: "Heartbeat plus system task schedules for memory and experience curation",
			Fields: []SettingsFieldSchema{
				{Path: "heartbeat", Label: "Heartbeat", Type: "object", Section: "scheduler", RestartRequired: true},
				{Path: "scheduler", Label: "Scheduler", Type: "object", Section: "scheduler"},
			},
		},
		{
			ID:          "daemon",
			Label:       "Daemon",
			Description: "Daemon control plane settings",
			Fields: []SettingsFieldSchema{
				{Path: "daemon.control_port", Label: "Control Port", Type: "number", Section: "daemon", RestartRequired: true},
			},
		},
		{
			ID:          "mcp",
			Label:       "MCP",
			Description: "MCP server registry",
			Fields: []SettingsFieldSchema{{Path: "mcp.servers", Label: "Servers", Type: "map", Section: "mcp", RestartRequired: true}},
		},
		{
			ID:          "proxy",
			Label:       "Proxy",
			Description: "Network proxy configuration for outbound connections",
			Fields: []SettingsFieldSchema{
				{Path: "proxy.enabled", Label: "Enabled", Type: "boolean", Section: "proxy"},
				{Path: "proxy.type", Label: "Default Type", Type: "select", Section: "proxy", Enum: []string{"http", "socks5"}},
				{Path: "proxy.http.url", Label: "HTTP Proxy URL", Type: "text", Section: "proxy", Sensitive: true},
				{Path: "proxy.http.username", Label: "HTTP Username", Type: "text", Section: "proxy", Sensitive: true},
				{Path: "proxy.http.password", Label: "HTTP Password", Type: "secret", Section: "proxy", Sensitive: true},
				{Path: "proxy.socks5.url", Label: "SOCKS5 Proxy URL", Type: "text", Section: "proxy", Sensitive: true},
				{Path: "proxy.socks5.username", Label: "SOCKS5 Username", Type: "text", Section: "proxy", Sensitive: true},
				{Path: "proxy.socks5.password", Label: "SOCKS5 Password", Type: "secret", Section: "proxy", Sensitive: true},
				{Path: "proxy.no_proxy", Label: "No Proxy List", Type: "array", Section: "proxy"},
				{Path: "proxy.controls.llm.enabled", Label: "LLM Proxy Enabled", Type: "boolean", Section: "proxy"},
				{Path: "proxy.controls.llm.type", Label: "LLM Proxy Type", Type: "select", Section: "proxy", Enum: []string{"http", "socks5"}},
				{Path: "proxy.controls.llm.providers", Label: "LLM Providers Control", Type: "map", Section: "proxy"},
				{Path: "proxy.controls.channels.default", Label: "Channels Default", Type: "boolean", Section: "proxy"},
				{Path: "proxy.controls.channels.type", Label: "Channels Proxy Type", Type: "select", Section: "proxy", Enum: []string{"http", "socks5"}},
				{Path: "proxy.controls.channels.per_channel", Label: "Per Channel Control", Type: "map", Section: "proxy"},
				{Path: "proxy.controls.tools.enabled", Label: "Tools Proxy Enabled", Type: "boolean", Section: "proxy"},
				{Path: "proxy.controls.tools.type", Label: "Tools Proxy Type", Type: "select", Section: "proxy", Enum: []string{"http", "socks5"}},
				{Path: "proxy.controls.tools.per_tool", Label: "Per Tool Control", Type: "map", Section: "proxy"},
				{Path: "proxy.controls.mcp.enabled", Label: "MCP Proxy Enabled", Type: "boolean", Section: "proxy"},
				{Path: "proxy.controls.mcp.type", Label: "MCP Proxy Type", Type: "select", Section: "proxy", Enum: []string{"http", "socks5"}},
				{Path: "proxy.controls.mcp.per_server", Label: "Per Server Control", Type: "map", Section: "proxy"},
				{Path: "proxy.controls.plugin.enabled", Label: "Plugin Proxy Enabled", Type: "boolean", Section: "proxy"},
				{Path: "proxy.controls.plugin.type", Label: "Plugin Proxy Type", Type: "select", Section: "proxy", Enum: []string{"http", "socks5"}},
				{Path: "proxy.controls.plugin.per_plugin", Label: "Per Plugin Control", Type: "map", Section: "proxy"},
				{Path: "proxy.controls.exec.enabled", Label: "Exec Proxy Enabled", Type: "boolean", Section: "proxy"},
				{Path: "proxy.controls.exec.type", Label: "Exec Proxy Type", Type: "select", Section: "proxy", Enum: []string{"http", "socks5"}},
				{Path: "proxy.controls.exec.per_command", Label: "Per Command Control", Type: "map", Section: "proxy"},
				{Path: "proxy.controls.subagents.internal.enabled", Label: "Internal Subagent Proxy", Type: "boolean", Section: "proxy"},
				{Path: "proxy.controls.subagents.external.enabled", Label: "External Subagent Proxy", Type: "boolean", Section: "proxy"},
				{Path: "proxy.controls.subagents.external.type", Label: "External Subagent Type", Type: "select", Section: "proxy", Enum: []string{"http", "socks5"}},
				{Path: "proxy.controls.subagents.external.per_agent", Label: "Per Agent Control", Type: "map", Section: "proxy"},
			},
		},
		{
			ID:          "logging",
			Label:       "Logging",
			Description: "Runtime logging behavior",
			Fields: []SettingsFieldSchema{
				{Path: "logging.level", Label: "Level", Type: "select", Section: "logging", Enum: []string{"DEBUG", "INFO", "WARN", "ERROR"}},
				{Path: "logging.json_mode", Label: "JSON Mode", Type: "boolean", Section: "logging"},
				{Path: "logging.color", Label: "Color", Type: "boolean", Section: "logging"},
			},
		},
	}
}

func (g *Gateway) currentConfigRawMap() (map[string]any, error) {
	path, err := g.resolvedConfigPath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read current config: %w", err)
	}
	var current map[string]any
	if err := yaml.Unmarshal(raw, &current); err != nil {
		return nil, fmt.Errorf("decode current config: %w", err)
	}
	return current, nil
}

func cloneSettingsMap(input map[string]any) (map[string]any, error) {
	if input == nil {
		return map[string]any{}, nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var cloned map[string]any
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil, err
	}
	if cloned == nil {
		return map[string]any{}, nil
	}
	return cloned, nil
}

func mergeMaskedSettings(submitted any, current any, sensitive bool) any {
	switch typed := submitted.(type) {
	case map[string]any:
		currentMap, _ := current.(map[string]any)
		merged := make(map[string]any, len(typed))
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			childSensitive := sensitive || isSensitiveSettingsKey(strings.ToLower(key)) || strings.ToLower(key) == "headers"
			merged[key] = mergeMaskedSettings(typed[key], currentMap[key], childSensitive)
		}
		return merged
	case []any:
		currentSlice, _ := current.([]any)
		merged := make([]any, len(typed))
		for i := range typed {
			var currentItem any
			if i < len(currentSlice) {
				currentItem = currentSlice[i]
			}
			merged[i] = mergeMaskedSettings(typed[i], currentItem, sensitive)
		}
		return merged
	case string:
		if sensitive && typed == settingsSecretMask {
			if current != nil {
				return current
			}
			return ""
		}
		return typed
	default:
		return submitted
	}
}

func (g *Gateway) settingsPayloadToYAML(settings map[string]any) (string, error) {
	cloned, err := cloneSettingsMap(settings)
	if err != nil {
		return "", fmt.Errorf("clone settings payload: %w", err)
	}
	current, err := g.currentConfigRawMap()
	if err != nil {
		return "", err
	}
	merged, ok := mergeMaskedSettings(cloned, current, false).(map[string]any)
	if !ok {
		return "", fmt.Errorf("settings payload must be an object")
	}
	data, err := yaml.Marshal(merged)
	if err != nil {
		return "", fmt.Errorf("encode settings yaml: %w", err)
	}
	return string(data), nil
}
