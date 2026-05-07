package gateway

import (
	"path/filepath"
	"testing"

	"github.com/chawuciren/evoduck/pkg/config"
)

func TestDisplayConfigUsesFirstRunProviderInSeededState(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".evoduck", "config", "config.yaml")
	paths, err := config.ResolvePaths(configPath)
	if err != nil {
		t.Fatalf("resolve paths: %v", err)
	}
	gw := &Gateway{configPath: configPath}
	entry, ok := config.LookupProviderCatalogEntry("openai-compatible")
	if !ok {
		t.Fatal("expected openai-compatible catalog entry")
	}
	cfg := &config.Config{
		DefaultAgent: "admin-bot",
		DataDir:      paths.DataDir,
		Gateway:      config.GatewayConfig{Host: "127.0.0.1", Port: 18789},
		LLM: config.LLMConfig{
			DefaultProvider: "openai",
			DefaultModel:    "gpt-4o",
			Providers: map[string]config.ProviderConfig{
				"openai-compatible": {
					Type:         "openai-compatible",
					BaseURL:      "https://api.openai.com/v1",
					DefaultModel: "gpt-4o",
					Models:       append([]config.ProviderModelConfig(nil), entry.Models...),
					APIKey:       "",
				},
			},
		},
		Agents: map[string]config.AgentConfig{
			"admin-bot": {
				Role:      "admin",
				Workspace: filepath.Join(paths.DataDir, "agents", "admin-bot"),
			},
		},
		Channels: config.ChannelsConfig{
			"webchat": {Type: "webchat", Agent: "admin-bot"},
		},
	}

	display := gw.displayConfig(cfg)
	if display.LLM.DefaultProvider != "openai-compatible" {
		t.Fatalf("expected display provider openai-compatible, got %q", display.LLM.DefaultProvider)
	}
	if display.LLM.DefaultModel != "gpt-4o" {
		t.Fatalf("expected display model gpt-4o, got %q", display.LLM.DefaultModel)
	}
	if cfg.LLM.DefaultProvider != "openai" {
		t.Fatalf("expected original config to remain unchanged, got %q", cfg.LLM.DefaultProvider)
	}
}

func TestCurrentConfigRawMapResolvesDefaultConfigPath(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("USERPROFILE", homeDir)
	t.Setenv("HOME", homeDir)

	paths, err := config.EnsureInitialized("")
	if err != nil {
		t.Fatalf("ensure initialized: %v", err)
	}

	gw := &Gateway{configPath: ""}

	resolvedPath, err := gw.resolvedConfigPath()
	if err != nil {
		t.Fatalf("resolve config path: %v", err)
	}
	if resolvedPath != paths.ConfigPath {
		t.Fatalf("expected resolved config path %q, got %q", paths.ConfigPath, resolvedPath)
	}

	rawMap, err := gw.currentConfigRawMap()
	if err != nil {
		t.Fatalf("current config raw map: %v", err)
	}
	if len(rawMap) == 0 {
		t.Fatal("expected current config raw map to contain seeded config values")
	}
}
