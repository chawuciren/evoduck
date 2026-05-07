package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSetupOptionsUsesFirstRunProviderForEmptyInput(t *testing.T) {
	opts := DefaultSetupOptions("")
	if opts.Provider != defaultFirstRunProvider {
		t.Fatalf("expected provider %q, got %q", defaultFirstRunProvider, opts.Provider)
	}
	if opts.BaseURL != defaultOpenAIBaseURL {
		t.Fatalf("expected default base url %q from catalog entry, got %q", defaultOpenAIBaseURL, opts.BaseURL)
	}
	if opts.Model != defaultOpenAIModel {
		t.Fatalf("expected default model %q from catalog entry, got %q", defaultOpenAIModel, opts.Model)
	}
}

func TestWeixinFirstRunUsesQRLoginSetup(t *testing.T) {
	entry, ok := LookupChannelCatalogEntry("weixin")
	if !ok {
		t.Fatal("expected weixin channel catalog entry")
	}
	if entry.SetupKind != ChannelSetupKindQRLogin {
		t.Fatalf("expected weixin setup kind %q, got %q", ChannelSetupKindQRLogin, entry.SetupKind)
	}
}

func TestWecomFirstRunUsesTokenSetup(t *testing.T) {
	entry, ok := LookupChannelCatalogEntry("wecom")
	if !ok {
		t.Fatal("expected wecom channel catalog entry")
	}
	if entry.SetupKind != ChannelSetupKindToken {
		t.Fatalf("expected wecom setup kind %q, got %q", ChannelSetupKindToken, entry.SetupKind)
	}
}

func TestValidateRejectsDeepSeekOnlyFieldsOnGenericProvider(t *testing.T) {
	cfg := minimalValidConfigForProvider(ProviderConfig{
		Type:            "openai-compatible",
		BaseURL:         "https://api.deepseek.com",
		DefaultModel:    "deepseek-v4-pro",
		Models:          []ProviderModelConfig{{ID: "deepseek-v4-pro", Name: "deepseek-v4-pro", Type: ProviderModelTypeChat}},
		Thinking:        &ThinkingConfig{Type: "enabled"},
		ReasoningReplay: "tool_calls_only",
	})
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected generic provider with DeepSeek-only fields to be rejected")
	}
}

func TestValidateAcceptsDeepSeekReasoningFields(t *testing.T) {
	cfg := minimalValidConfigForProvider(ProviderConfig{
		Type:            "deepseek",
		BaseURL:         "https://api.deepseek.com",
		DefaultModel:    "deepseek-v4-pro",
		Models:          []ProviderModelConfig{{ID: "deepseek-v4-pro", Name: "deepseek-v4-pro", Type: ProviderModelTypeChat}},
		Thinking:        &ThinkingConfig{Type: "enabled"},
		ReasoningReplay: "tool_calls_only",
	})
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected DeepSeek provider config to validate, got %v", err)
	}
}

func minimalValidConfigForProvider(provider ProviderConfig) *Config {
	return &Config{
		DefaultAgent: "admin-bot",
		Gateway:      GatewayConfig{Host: "127.0.0.1", Port: 18789},
		LLM: LLMConfig{
			DefaultProvider: "test",
			DefaultModel:    provider.DefaultModel,
			Providers:       map[string]ProviderConfig{"test": provider},
		},
		Agents: map[string]AgentConfig{
			"admin-bot": {
				Role:      "admin",
				Workspace: os.TempDir(),
				Provider:  "test",
				Model:     provider.DefaultModel,
			},
		},
		Channels: ChannelsConfig{
			"webchat": {Type: "webchat", Agent: "admin-bot", Role: "admin"},
		},
		Memory: MemoryConfig{
			ShortTerm:  ShortTermConfig{MaxMessages: 10, MaxTokens: 1000},
			MediumTerm: MediumTermConfig{MaxSize: 100},
		},
		Scheduler: SchedulerConfig{SystemTasks: SystemSchedulerTasksConfig{
			MemoryCuration:     SystemTaskConfig{Schedule: "0 * * * *"},
			ExperienceCuration: SystemTaskConfig{Schedule: "0 3 * * *"},
		}},
	}
}

func TestApplySetupOptionsUsesFirstRunProviderForEmptyInput(t *testing.T) {
	cfg := &Config{
		Gateway: GatewayConfig{},
		LLM: LLMConfig{
			Providers: map[string]ProviderConfig{
				defaultFirstRunProvider: func() ProviderConfig {
					entry, ok := LookupProviderCatalogEntry(defaultFirstRunProvider)
					if !ok {
						t.Fatal("expected first-run provider catalog entry")
					}
					return ProviderConfig{
						Type:         defaultFirstRunProvider,
						BaseURL:      defaultOpenAIBaseURL,
						DefaultModel: defaultOpenAIModel,
						Models:       append([]ProviderModelConfig(nil), entry.Models...),
					}
				}(),
			},
		},
		Agents: map[string]AgentConfig{
			"admin-bot": {},
		},
	}

	applySetupOptions(cfg, SetupOptions{Provider: "", APIKey: "test-key"})

	if cfg.LLM.DefaultProvider != defaultFirstRunProvider {
		t.Fatalf("expected default provider %q, got %q", defaultFirstRunProvider, cfg.LLM.DefaultProvider)
	}
	provider, ok := cfg.LLM.Providers[defaultFirstRunProvider]
	if !ok {
		t.Fatalf("expected provider %q to exist", defaultFirstRunProvider)
	}
	if provider.Type != defaultFirstRunProvider {
		t.Fatalf("expected provider type %q, got %q", defaultFirstRunProvider, provider.Type)
	}
	if provider.APIKey != "test-key" {
		t.Fatalf("expected api key to be written")
	}
	entry, ok := LookupProviderCatalogEntry(defaultFirstRunProvider)
	if !ok {
		t.Fatal("expected first-run provider catalog entry")
	}
	if !providerModelCatalogEquals(provider.Models, entry.Models) {
		t.Fatalf("expected provider to keep full catalog models, got %+v", provider.Models)
	}
	agent := cfg.Agents["admin-bot"]
	if agent.Provider != defaultFirstRunProvider {
		t.Fatalf("expected agent provider %q, got %q", defaultFirstRunProvider, agent.Provider)
	}
}

func TestFirstRunDisplayLLMUsesFirstRunProvider(t *testing.T) {
	paths := Paths{DataDir: "/tmp/evoduck", SharedSkillsDir: "/tmp/evoduck/shared/skills"}
	cfg := &Config{
		DefaultAgent: "admin-bot",
		DataDir:      paths.DataDir,
		Gateway:      GatewayConfig{Host: "127.0.0.1", Port: 18789},
		LLM: LLMConfig{
			DefaultProvider: defaultFirstRunProvider,
			DefaultModel:    defaultOpenAIModel,
			Providers: map[string]ProviderConfig{
				defaultFirstRunProvider: func() ProviderConfig {
					entry, ok := LookupProviderCatalogEntry(defaultFirstRunProvider)
					if !ok {
						t.Fatal("expected first-run provider catalog entry")
					}
					return ProviderConfig{
						Type:         defaultFirstRunProvider,
						BaseURL:      defaultOpenAIBaseURL,
						DefaultModel: defaultOpenAIModel,
						Models:       append([]ProviderModelConfig(nil), entry.Models...),
						APIKey:       "",
					}
				}(),
			},
		},
		Agents: map[string]AgentConfig{
			"admin-bot": {
				Role:      "admin",
				Workspace: "/tmp/evoduck/agents/admin-bot",
			},
		},
		Channels: ChannelsConfig{
			"webchat": {Type: "webchat", Agent: "admin-bot"},
		},
	}

	provider, model, ok := FirstRunDisplayLLM(cfg, paths)
	if !ok {
		t.Fatal("expected first-run display llm to match seeded default config")
	}
	if provider != defaultFirstRunProvider || model != defaultOpenAIModel {
		t.Fatalf("expected %q/%q, got %q/%q", defaultFirstRunProvider, defaultOpenAIModel, provider, model)
	}
}

func TestFirstRunDisplayLLMRepairsDisplayAfterRuntimeFallback(t *testing.T) {
	paths := Paths{DataDir: "/tmp/evoduck", SharedSkillsDir: "/tmp/evoduck/shared/skills"}
	cfg := &Config{
		DefaultAgent: "admin-bot",
		DataDir:      paths.DataDir,
		Gateway:      GatewayConfig{Host: "127.0.0.1", Port: 18789},
		LLM: LLMConfig{
			DefaultProvider: "openai",
			DefaultModel:    defaultOpenAIModel,
			Providers: map[string]ProviderConfig{
				defaultFirstRunProvider: func() ProviderConfig {
					entry, ok := LookupProviderCatalogEntry(defaultFirstRunProvider)
					if !ok {
						t.Fatal("expected first-run provider catalog entry")
					}
					return ProviderConfig{
						Type:         defaultFirstRunProvider,
						BaseURL:      defaultOpenAIBaseURL,
						DefaultModel: defaultOpenAIModel,
						Models:       append([]ProviderModelConfig(nil), entry.Models...),
						APIKey:       "",
					}
				}(),
			},
		},
		Agents: map[string]AgentConfig{
			"admin-bot": {
				Role:      "admin",
				Workspace: "/tmp/evoduck/agents/admin-bot",
			},
		},
		Channels: ChannelsConfig{
			"webchat": {Type: "webchat", Agent: "admin-bot"},
		},
	}

	provider, model, ok := FirstRunDisplayLLM(cfg, paths)
	if !ok {
		t.Fatal("expected first-run display llm to repair runtime fallback state")
	}
	if provider != defaultFirstRunProvider || model != defaultOpenAIModel {
		t.Fatalf("expected %q/%q, got %q/%q", defaultFirstRunProvider, defaultOpenAIModel, provider, model)
	}
}

func TestDetectFirstRunSetupWithoutConfigDoesNotCreateFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".evoduck", "config", "config.yaml")

	state, err := DetectFirstRunSetup(configPath)
	if err != nil {
		t.Fatalf("DetectFirstRunSetup returned error: %v", err)
	}
	if state == nil {
		t.Fatal("expected first-run setup state")
	}
	if state.ConfigPath != configPath {
		t.Fatalf("expected config path %q, got %q", configPath, state.ConfigPath)
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("expected config file to remain absent, stat err=%v", err)
	}
}

func TestSaveFirstRunSetupCreatesConfigOnlyAfterSuccess(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".evoduck", "config", "config.yaml")

	err := SaveFirstRunSetup(configPath, SetupOptions{
		Provider: "ollama",
		BaseURL:  "http://localhost:11434/v1",
		Model:    "qwen2.5",
		Host:     "127.0.0.1",
		Port:     18789,
	})
	if err != nil {
		t.Fatalf("SaveFirstRunSetup returned error: %v", err)
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file to be created, stat err=%v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.LLM.DefaultProvider != "ollama" {
		t.Fatalf("expected default provider ollama, got %q", cfg.LLM.DefaultProvider)
	}
	if cfg.LLM.DefaultModel != "qwen2.5" {
		t.Fatalf("expected default model qwen2.5, got %q", cfg.LLM.DefaultModel)
	}
}

func TestSaveFirstRunSetupFailureDoesNotCreateFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".evoduck", "config", "config.yaml")

	err := SaveFirstRunSetup(configPath, SetupOptions{
		Provider: "openai-compatible",
		BaseURL:  "",
		APIKey:   "",
		Model:    "missing-model",
		Host:     "127.0.0.1",
		Port:     18789,
	})
	if err == nil {
		t.Fatal("expected SaveFirstRunSetup to fail")
	}
	if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected config file to remain absent, stat err=%v", statErr)
	}
}

func TestSaveFirstRunSetupAllowsEmptyAPIKeyForOpenAICompatible(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".evoduck", "config", "config.yaml")

	err := SaveFirstRunSetup(configPath, SetupOptions{
		Provider: "openai-compatible",
		BaseURL:  "http://127.0.0.1:8080/v1",
		APIKey:   "",
		Model:    "gpt-4o-mini",
		Host:     "127.0.0.1",
		Port:     18789,
	})
	if err != nil {
		t.Fatalf("SaveFirstRunSetup returned error: %v", err)
	}
}

func TestSaveFirstRunSetupAllowsEmptyAPIKeyForLMStudio(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), ".evoduck", "config", "config.yaml")

	err := SaveFirstRunSetup(configPath, SetupOptions{
		Provider: "lmstudio",
		BaseURL:  "http://127.0.0.1:1234/v1",
		APIKey:   "",
		Model:    "local-model",
		Host:     "127.0.0.1",
		Port:     18789,
	})
	if err != nil {
		t.Fatalf("SaveFirstRunSetup returned error: %v", err)
	}
}

func TestValidateAgentsRejectsEmptyPermissionEntries(t *testing.T) {
	cfg := &Config{
		Gateway: GatewayConfig{Host: "127.0.0.1", Port: 18789},
		LLM: LLMConfig{
			DefaultProvider: "stub",
			DefaultModel:    "stub-model",
			Providers: map[string]ProviderConfig{
				"stub": {Type: "stub", DefaultModel: "stub-model", Models: []ProviderModelConfig{{ID: "stub-model", Name: "stub-model", Type: ProviderModelTypeChat}}},
			},
		},
		Agents: map[string]AgentConfig{
			"agent-test": {
				Role:      "employee",
				Workspace: t.TempDir(),
				Provider:  "stub",
				Model:     "stub-model",
				Permissions: AgentPermissionConfig{
					AuthorizedDirectories: []string{""},
					AuthorizedTools:       []string{""},
				},
			},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation to fail for empty permission entries")
	}
}
