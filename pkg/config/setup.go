package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultOpenAIBaseURL    = "https://api.openai.com/v1"
	defaultOpenAIModel      = "gpt-4o"
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	defaultAnthropicModel   = "claude-sonnet-4-5"
	defaultFirstRunProvider = "openai-compatible"
)

type FirstRunSetupState struct {
	ConfigPath string
	Paths      Paths
	Config     *Config
}

type SetupOptions struct {
	Provider           string
	APIKey             string
	BaseURL            string
	Model              string
	Host               string
	Port               int
	AdditionalChannels []ChannelSetupOption
}

type ChannelSetupOption struct {
	Type       string
	ChannelID  string
	Name       string
	Role       string
	Agent      string
	Token      string // Weixin: token from QR login
	UserID     string // Weixin: user ID
	APIBaseURL string // Weixin: API base URL
	BotID      string // WeCom: AI Bot ID
	Secret     string // WeCom: AI Bot Secret
}

func DetectFirstRunSetup(configPath string) (*FirstRunSetupState, error) {
	resolvedPath, err := ResolveConfigPath(configPath)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	paths, err := ResolvePaths(resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("resolve paths: %w", err)
	}
	if !pathExists(paths.ConfigPath) {
		return &FirstRunSetupState{ConfigPath: paths.ConfigPath, Paths: paths}, nil
	}

	cfg, err := loadConfigFromDisk(paths.ConfigPath, paths)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if !isSeededDefaultConfig(cfg, paths) {
		return nil, nil
	}
	if !defaultProviderNeedsSetup(cfg) {
		return nil, nil
	}

	return &FirstRunSetupState{
		ConfigPath: paths.ConfigPath,
		Paths:      paths,
		Config:     cfg,
	}, nil
}

func SaveFirstRunSetup(configPath string, opts SetupOptions) error {
	resolvedPath, err := ResolveConfigPath(configPath)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}

	paths, err := ResolvePaths(resolvedPath)
	if err != nil {
		return fmt.Errorf("resolve paths: %w", err)
	}

	cfg, err := loadFirstRunSetupConfig(paths)
	if err != nil {
		return err
	}

	applySetupOptions(cfg, opts)
	NormalizeForRuntime(cfg, paths)
	if err := cfg.ValidateWithEnv(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	outData, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	dir := filepath.Dir(paths.ConfigPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.WriteFile(paths.ConfigPath, outData, 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

func loadFirstRunSetupConfig(paths Paths) (*Config, error) {
	if pathExists(paths.ConfigPath) {
		cfg, err := loadConfigFromDisk(paths.ConfigPath, paths)
		if err != nil {
			return nil, fmt.Errorf("load config: %w", err)
		}
		return cfg, nil
	}

	var cfg Config
	if err := yaml.Unmarshal([]byte(defaultConfigYAML(paths)), &cfg); err != nil {
		return nil, fmt.Errorf("parse default config: %w", err)
	}
	NormalizeForRuntime(&cfg, paths)
	return &cfg, nil
}

func DefaultSetupOptions(provider string) SetupOptions {
	provider = NormalizeFirstRunProviderName(provider)
	opts := SetupOptions{
		Provider: provider,
		Host:     "127.0.0.1",
		Port:     18789,
	}
	if entry, ok := LookupProviderCatalogEntry(provider); ok {
		opts.Provider = entry.Type
		opts.BaseURL = entry.DefaultBaseURL
		opts.Model = entry.DefaultModel
	} else {
		opts.Provider = defaultFirstRunProvider
		opts.BaseURL = defaultOpenAIBaseURL
		opts.Model = defaultOpenAIModel
	}

	return opts
}

func isSeededDefaultConfig(cfg *Config, paths Paths) bool {
	if cfg == nil {
		return false
	}
	if cfg.DefaultAgent != "admin-bot" {
		return false
	}
	if cfg.LLM.DefaultProvider != defaultFirstRunProvider || cfg.LLM.DefaultModel != defaultOpenAIModel {
		return false
	}
	if len(cfg.Agents) != 1 || len(cfg.Channels) != 1 {
		return false
	}
	adminAgent, ok := cfg.Agents["admin-bot"]
	if !ok || adminAgent.Role != "admin" {
		return false
	}
	if filepath.Clean(adminAgent.Workspace) != filepath.Clean(filepath.Join(paths.DataDir, "agents", "admin-bot")) {
		return false
	}
	webchat, ok := cfg.Channels["webchat"]
	if !ok || webchat.Type != "webchat" || webchat.Agent != "admin-bot" {
		return false
	}
	provider, ok := cfg.LLM.Providers[defaultFirstRunProvider]
	if !ok || provider.Type != defaultFirstRunProvider {
		return false
	}
	entry, ok := LookupProviderCatalogEntry(defaultFirstRunProvider)
	if !ok {
		return false
	}
	if provider.DefaultModel != entry.DefaultModel {
		return false
	}
	return providerModelCatalogEquals(provider.Models, entry.Models)
}

func defaultProviderNeedsSetup(cfg *Config) bool {
	provider, ok := cfg.LLM.Providers[cfg.LLM.DefaultProvider]
	if !ok {
		return false
	}
	if provider.Type == "ollama" {
		return false
	}
	return strings.TrimSpace(provider.APIKey) == ""
}

func applySetupOptions(cfg *Config, opts SetupOptions) {
	providerName := NormalizeFirstRunProviderName(opts.Provider)
	defaults := DefaultSetupOptions(providerName)
	entry, _ := LookupProviderCatalogEntry(providerName)

	cfg.Gateway.Host = strings.TrimSpace(opts.Host)
	if cfg.Gateway.Host == "" {
		cfg.Gateway.Host = defaults.Host
	}
	if opts.Port > 0 {
		cfg.Gateway.Port = opts.Port
	} else if cfg.Gateway.Port == 0 {
		cfg.Gateway.Port = defaults.Port
	}

	provider := cfg.LLM.Providers[providerName]
	provider.Type = providerName
	provider.APIKey = strings.TrimSpace(opts.APIKey)
	provider.BaseURL = strings.TrimSpace(opts.BaseURL)
	provider.DefaultModel = strings.TrimSpace(opts.Model)
	if provider.DefaultModel == "" {
		provider.DefaultModel = defaults.Model
	}
	if provider.BaseURL == "" && defaults.BaseURL != "" {
		provider.BaseURL = defaults.BaseURL
	}
	provider.Models = cloneProviderModelConfigs(entry.Models)

	cfg.LLM.DefaultProvider = providerName
	cfg.LLM.DefaultModel = provider.DefaultModel
	cfg.LLM.Providers = map[string]ProviderConfig{providerName: provider}

	for id, agent := range cfg.Agents {
		if strings.TrimSpace(agent.Provider) == "" || agent.Provider == "openai" || agent.Provider == defaultFirstRunProvider {
			agent.Provider = providerName
		}
		if strings.TrimSpace(agent.Model) == "" || agent.Model == defaultOpenAIModel {
			agent.Model = provider.DefaultModel
		}
		cfg.Agents[id] = agent
	}

	// Apply additional channels (skip webchat since it's the built-in gateway web layer entry)
	for _, ch := range opts.AdditionalChannels {
		if ch.Type == "" || ch.ChannelID == "" {
			continue
		}
		// Skip webchat - it's always present as the built-in gateway web layer entry
		if ch.Type == "webchat" {
			continue
		}
		channelCfg := ChannelConfig{
			Type:       NormalizeChannelName(ch.Type),
			Name:       ch.Name,
			Role:       ch.Role,
			Agent:      ch.Agent,
			Token:      ch.Token,
			UserID:     ch.UserID,
			APIBaseURL: ch.APIBaseURL,
			BotID:      ch.BotID,
			Secret:     ch.Secret,
		}
		if channelCfg.Type == "" {
			channelCfg.Type = ch.Type
		}
		if channelCfg.Role == "" {
			channelCfg.Role = "employee"
		}
		if channelCfg.Agent == "" {
			channelCfg.Agent = cfg.DefaultAgent
		}
		cfg.Channels[ch.ChannelID] = channelCfg
	}
}

func FirstRunDisplayLLM(cfg *Config, paths Paths) (string, string, bool) {
	if cfg == nil {
		return "", "", false
	}
	provider, ok := cfg.LLM.Providers[defaultFirstRunProvider]
	if !ok || provider.Type != defaultFirstRunProvider {
		return "", "", false
	}
	if cfg.DefaultAgent != "admin-bot" {
		return "", "", false
	}
	if len(cfg.Agents) != 1 || len(cfg.Channels) != 1 {
		return "", "", false
	}
	adminAgent, ok := cfg.Agents["admin-bot"]
	if !ok || adminAgent.Role != "admin" {
		return "", "", false
	}
	if filepath.Clean(adminAgent.Workspace) != filepath.Clean(filepath.Join(paths.DataDir, "agents", "admin-bot")) {
		return "", "", false
	}
	webchat, ok := cfg.Channels["webchat"]
	if !ok || webchat.Type != "webchat" || webchat.Agent != "admin-bot" {
		return "", "", false
	}
	entry, ok := LookupProviderCatalogEntry(defaultFirstRunProvider)
	if !ok {
		return "", "", false
	}
	if strings.TrimSpace(provider.APIKey) != "" || provider.DefaultModel != entry.DefaultModel {
		return "", "", false
	}
	if !providerModelCatalogEquals(provider.Models, entry.Models) {
		return "", "", false
	}
	return defaultFirstRunProvider, provider.DefaultModel, true
}

func providerModelCatalogEquals(left, right []ProviderModelConfig) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if strings.TrimSpace(left[i].ID) != strings.TrimSpace(right[i].ID) {
			return false
		}
		if strings.TrimSpace(left[i].Name) != strings.TrimSpace(right[i].Name) {
			return false
		}
		if normalizeProviderModelType(left[i].Type) != normalizeProviderModelType(right[i].Type) {
			return false
		}
		if left[i].Capabilities != right[i].Capabilities {
			return false
		}
		if left[i].ContextWindow != right[i].ContextWindow {
			return false
		}
		if left[i].MaxOutputTokens != right[i].MaxOutputTokens {
			return false
		}
	}
	return true
}

func normalizeProvider(provider string) string {
	return NormalizeProviderName(provider)
}
