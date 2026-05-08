package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chawuciren/evoduck/internal/profile"
	"github.com/chawuciren/evoduck/internal/skill/builtin"
	"gopkg.in/yaml.v3"
)

const appRootName = ".evoduck"

type Paths struct {
	RootDir         string
	ConfigDir       string
	ConfigPath      string
	DataDir         string
	LogsDir         string
	SharedSkillsDir string
}

func DefaultConfigPath() (string, error) {
	paths, err := ResolvePaths("")
	if err != nil {
		return "", err
	}
	return paths.ConfigPath, nil
}

func DefaultDataDir() (string, error) {
	paths, err := ResolvePaths("")
	if err != nil {
		return "", err
	}
	return paths.DataDir, nil
}

func DefaultLogsDir() (string, error) {
	paths, err := ResolvePaths("")
	if err != nil {
		return "", err
	}
	return paths.LogsDir, nil
}

func ResolveConfigPath(configPath string) (string, error) {
	paths, err := ResolvePaths(configPath)
	if err != nil {
		return "", err
	}
	return paths.ConfigPath, nil
}

func ResolvePaths(configPath string) (Paths, error) {
	if strings.TrimSpace(configPath) == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve home dir: %w", err)
		}
		configPath = filepath.Join(homeDir, appRootName, "config", "config.yaml")
	}

	if !filepath.IsAbs(configPath) {
		absPath, err := filepath.Abs(configPath)
		if err != nil {
			return Paths{}, fmt.Errorf("resolve config path: %w", err)
		}
		configPath = absPath
	}

	configPath = filepath.Clean(configPath)
	configDir := filepath.Dir(configPath)
	rootDir := configDir
	if filepath.Base(configDir) == "config" {
		rootDir = filepath.Dir(configDir)
	}

	return Paths{
		RootDir:         rootDir,
		ConfigDir:       configDir,
		ConfigPath:      configPath,
		DataDir:         rootDir,
		LogsDir:         filepath.Join(rootDir, "logs"),
		SharedSkillsDir: filepath.Join(rootDir, "shared", "skills"),
	}, nil
}

func EnsureInitialized(configPath string) (Paths, error) {
	paths, err := ResolvePaths(configPath)
	if err != nil {
		return Paths{}, err
	}

	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		return Paths{}, fmt.Errorf("create config dir: %w", err)
	}
	if err := os.MkdirAll(paths.RootDir, 0o755); err != nil {
		return Paths{}, fmt.Errorf("create app dir: %w", err)
	}

	if !pathExists(paths.ConfigPath) {
		if err := seedConfig(paths); err != nil {
			return Paths{}, err
		}
	}

	cfg, err := loadConfigFromDisk(paths.ConfigPath, paths)
	if err != nil {
		return Paths{}, err
	}
	if err := ensureRuntimeDirs(paths, cfg); err != nil {
		return Paths{}, err
	}
	if err := builtin.EnsureBuiltinSkills(paths.SharedSkillsDir); err != nil {
		return Paths{}, fmt.Errorf("ensure builtin skills: %w", err)
	}
	if err := ensureAgentScaffolds(cfg); err != nil {
		return Paths{}, err
	}

	return paths, nil
}

func seedConfig(paths Paths) error {
	return os.WriteFile(paths.ConfigPath, []byte(defaultConfigYAML(paths)), 0o644)
}

func loadConfigFromDisk(configPath string, paths Paths) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	PrepareForRuntime(&cfg, paths)

	return &cfg, nil
}

func PrepareForRuntime(cfg *Config, paths Paths) {
	expandEnv(cfg)
	setDefaults(cfg, paths.DataDir)
	normalizePaths(cfg, paths)
}

func NormalizeForRuntime(cfg *Config, paths Paths) {
	PrepareForRuntime(cfg, paths)
}

func normalizePaths(cfg *Config, paths Paths) {
	cfg.DataDir = resolveUserPath(cfg.DataDir, paths.RootDir, paths.DataDir)
	if cfg.Shared.SkillsDir == "" {
		cfg.Shared.SkillsDir = paths.SharedSkillsDir
	} else {
		cfg.Shared.SkillsDir = resolveUserPath(cfg.Shared.SkillsDir, paths.RootDir, paths.SharedSkillsDir)
	}

	for id, agentCfg := range cfg.Agents {
		defaultWorkspace := filepath.Join(cfg.DataDir, "agents", id)
		agentCfg.Workspace = resolveUserPath(agentCfg.Workspace, paths.RootDir, defaultWorkspace)
		cfg.Agents[id] = agentCfg
	}

	for name, server := range cfg.MCP.Servers {
		if server.Environment == nil {
			continue
		}
		memoryFilePath := strings.TrimSpace(server.Environment["MEMORY_FILE_PATH"])
		if memoryFilePath != "" {
			server.Environment["MEMORY_FILE_PATH"] = resolveUserPath(memoryFilePath, paths.RootDir, filepath.Join(cfg.DataDir, "mcp-memory.jsonl"))
			cfg.MCP.Servers[name] = server
		}
	}
}

func ensureRuntimeDirs(paths Paths, cfg *Config) error {
	dirs := []string{
		paths.RootDir,
		paths.ConfigDir,
		cfg.DataDir,
		filepath.Join(cfg.DataDir, "agents"),
		filepath.Join(cfg.DataDir, "users"),
		filepath.Join(cfg.DataDir, "sessions"),
		filepath.Join(cfg.DataDir, "scheduler"),
		filepath.Join(cfg.DataDir, "subagents"),

		paths.LogsDir,
		paths.SharedSkillsDir,
	}

	for _, agentCfg := range cfg.Agents {
		if strings.TrimSpace(agentCfg.Workspace) != "" {
			dirs = append(dirs, agentCfg.Workspace)
		}
	}

	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	return nil
}

func ensureAgentScaffolds(cfg *Config) error {
	for id, agentCfg := range cfg.Agents {
		if strings.TrimSpace(agentCfg.Workspace) == "" {
			continue
		}
		if err := profile.EnsureAgentScaffold(agentCfg.Workspace); err != nil {
			return fmt.Errorf("ensure scaffold for agent %s: %w", id, err)
		}
	}
	return nil
}

func resolveUserPath(current, rootDir, defaultPath string) string {
	current = strings.TrimSpace(current)
	if current == "" {
		return filepath.Clean(defaultPath)
	}
	if filepath.IsAbs(current) {
		return filepath.Clean(current)
	}
	return filepath.Clean(filepath.Join(rootDir, current))
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func defaultConfigYAML(paths Paths) string {
	dataDir := filepath.ToSlash(paths.DataDir)
	workspace := filepath.ToSlash(filepath.Join(paths.DataDir, "agents", "admin-bot"))
	skillsDir := filepath.ToSlash(paths.SharedSkillsDir)

	providerBlocks := strings.TrimRight(renderSeededProviderCatalogYAML(), "\n")

	return fmt.Sprintf(`gateway:
  host: 127.0.0.1
  port: 18789
  token: ""

default_agent: admin-bot
data_dir: "%s"

logging:
  level: INFO
  json_mode: false
  color: true

llm:
  default_provider: openai-compatible
  default_model: gpt-4o
  providers:
%s

    # Additional providers follow the same structured schema. The setup wizard
    # and provider catalog seed the full model directory automatically.
    #
    # Example custom OpenAI-compatible provider:
    # custom-openai:
    #   type: openai-compatible
    #   base_url: https://your-openai-compatible-endpoint/v1
    #   api_key: ${CUSTOM_API_KEY}
    #   default_model: your-model
    #   models:
    #     - id: your-model
    #       name: Your Model
    #       type: chat
    #       capabilities:
    #         vision: true
    #         reasoning: false
    #         tool_use: true
    #       context_window: 128000
    #       max_output_tokens: 16384
    #
    # Example local Ollama provider:
    # ollama:
    #   type: ollama
    #   base_url: http://localhost:11434/v1
    #   default_model: qwen2.5
    #   models:
    #     - id: qwen2.5
    #       name: qwen2.5
    #       type: chat
    #       capabilities:
    #         vision: true
    #         reasoning: true
    #         tool_use: true
    #       context_window: 128000
    #       max_output_tokens: 8192

agents:
  admin-bot:
    workspace: "%s"
    role: admin
    user_isolation:
      auto_create: true
      auto_profile: true
    channels:
      - webchat

channels:
  webchat:
    type: webchat
    agent: admin-bot
    role: admin
    token: ""

shared:
  skills_dir: "%s"

tools:
  backend_call:
    endpoints: {}
  session:
    enabled: true
    visibility:
      employee: user
      customer: self
    allow:
      employee:
        - sessions_list
        - sessions_history
        - sessions_send
      customer: []

memory:
  short_term:
    max_messages: 200
    max_tokens: 128000
    keep_recent: 10
    session_ttl: 168h
    cleanup_interval: 1h
  medium_term:
    dir: memory
    max_size: 5000
    load_days: 7
    min_messages_to_extract: 5
    compression_threshold: 10000
  long_term:
    compression_threshold: 15000
    dedup_threshold: 0.95
    cleanup_policy:
      check_interval: 24h
      min_age_days: 30
      batch_size: 30
      reference:
        medium_memory_days: 7
        include_core_memory: true
        include_access_stats: true
  core_memory:
    file: MEMORY.md
    auto_consolidate: true
    importance_threshold: 0.9

heartbeat:
  enabled: false
  interval: 30m
  prompt: "Check if anything needs attention."

scheduler:
  system_tasks:
    memory_curation:
      schedule: "0 */3 * * *"
    experience_curation:
      schedule: "0 3 * * *"

mcp:
  servers: {}
`, dataDir, providerBlocks, workspace, skillsDir)
}

func renderSeededProviderCatalogYAML() string {
	var b strings.Builder
	for _, item := range []struct {
		name      string
		apiKeyEnv string
	}{
		{name: "openai-compatible", apiKeyEnv: "${OPENAI_API_KEY}"},
		{name: "gemini", apiKeyEnv: "${GEMINI_API_KEY}"},
		{name: "anthropic", apiKeyEnv: "${ANTHROPIC_API_KEY}"},
	} {
		block := renderSeededProviderBlock(item.name, item.apiKeyEnv)
		if block == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(block)
	}
	return b.String()
}

func renderSeededProviderBlock(providerName, apiKey string) string {
	entry, ok := LookupProviderCatalogEntry(providerName)
	if !ok {
		return ""
	}
	provider := map[string]ProviderConfig{
		entry.Type: {
			Type:         entry.Type,
			BaseURL:      entry.DefaultBaseURL,
			APIKey:       apiKey,
			DefaultModel: entry.DefaultModel,
			Models:       cloneProviderModelConfigs(entry.Models),
		},
	}
	data, err := yaml.Marshal(provider)
	if err != nil {
		return ""
	}
	return indentYAMLBlock(string(data), 4)
}

func indentYAMLBlock(block string, spaces int) string {
	indent := strings.Repeat(" ", spaces)
	lines := strings.Split(strings.TrimRight(block, "\n"), "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}
