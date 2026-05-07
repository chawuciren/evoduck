package gateway

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"gopkg.in/yaml.v3"
)

var settingsLog = logger.NewModuleLogger("settings")

type SettingsValidationIssue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type SettingsOperationResult struct {
	AppliedNow      []string `json:"applied_now,omitempty"`
	RestartRequired []string `json:"restart_required,omitempty"`
	BackupPath      string   `json:"backup_path,omitempty"`
	RolledBack      bool     `json:"rolled_back,omitempty"`
}

func (g *Gateway) currentConfig() *config.Config {
	g.configMu.RLock()
	defer g.configMu.RUnlock()
	return g.config
}

func (g *Gateway) setCurrentConfig(cfg *config.Config) {
	g.configMu.Lock()
	defer g.configMu.Unlock()
	g.config = cfg
}

func (g *Gateway) resolvedConfigPath() (string, error) {
	path, err := config.ResolveConfigPath(g.configPath)
	if err != nil {
		return "", fmt.Errorf("resolve config path: %w", err)
	}
	return path, nil
}

func (g *Gateway) configSnapshot() map[string]any {
	cfg := g.currentConfig()
	if cfg == nil {
		return map[string]any{}
	}

	cfg = g.displayConfig(cfg)

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return map[string]any{}
	}

	var snapshot map[string]any
	if err := yaml.Unmarshal(data, &snapshot); err != nil {
		return map[string]any{}
	}

	maskSensitiveSettings(snapshot)
	return snapshot
}

func maskSensitiveSettings(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(key)
			if isSensitiveSettingsKey(lower) || lower == "headers" {
				typed[key] = maskSettingsValue(child)
				continue
			}
			maskSensitiveSettings(child)
		}
	case []any:
		for _, child := range typed {
			maskSensitiveSettings(child)
		}
	}
}

func isSensitiveSettingsKey(key string) bool {
	switch key {
	case "token", "api_key", "apikey", "secret", "password":
		return true
	default:
		return false
	}
}

func maskSettingsValue(value any) any {
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return ""
		}
		return "********"
	case map[string]any:
		masked := make(map[string]any, len(typed))
		for key := range typed {
			masked[key] = "********"
		}
		return masked
	case []any:
		masked := make([]any, len(typed))
		for i := range typed {
			masked[i] = "********"
		}
		return masked
	default:
		return "********"
	}
}

func (g *Gateway) displayConfig(cfg *config.Config) *config.Config {
	if cfg == nil {
		return nil
	}
	cloned := *cfg
	paths, err := config.ResolvePaths(g.configPath)
	if err == nil {
		if provider, model, ok := config.FirstRunDisplayLLM(&cloned, paths); ok {
			cloned.LLM.DefaultProvider = provider
			cloned.LLM.DefaultModel = model
		}
	}
	return &cloned
}

func validationIssuesFromError(err error) []SettingsValidationIssue {
	verrs, ok := err.(config.ValidationErrors)
	if !ok {
		return nil
	}
	issues := make([]SettingsValidationIssue, 0, len(verrs))
	for _, verr := range verrs {
		issues = append(issues, SettingsValidationIssue{Field: verr.Field, Message: verr.Message})
	}
	return issues
}

func validateConfigYAML(raw string, configPath string) (*config.Config, []SettingsValidationIssue, error) {
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, nil, err
	}

	resolvedPath, err := config.ResolveConfigPath(configPath)
	if err != nil {
		return nil, nil, err
	}
	paths, err := config.ResolvePaths(resolvedPath)
	if err != nil {
		return nil, nil, err
	}
	config.PrepareForRuntime(&cfg, paths)

	cfgCopy := cfg
	if err := cfgCopy.ValidateWithEnv(); err != nil {
		if issues := validationIssuesFromError(err); len(issues) > 0 {
			return &cfgCopy, issues, nil
		}
		return nil, nil, err
	}

	return &cfgCopy, nil, nil
}

func (g *Gateway) applyConfig(cfg *config.Config) (SettingsOperationResult, error) {
	logger.Configure(cfg.Logging.Level, cfg.Logging.JSONMode, cfg.Logging.Color)
	if cfg.DataDir != "" {
		logger.SetFileOutputDir(filepath.Join(cfg.DataDir, "logs"))
	}
	if err := g.ensureExperienceCuratorAgent(cfg); err != nil {
		return SettingsOperationResult{}, err
	}
	g.setCurrentConfig(cfg)
	g.rebuildChannels(cfg, g.channelsStarted)
	if err := g.registerSystemScheduledTasks(); err != nil {
		return SettingsOperationResult{}, err
	}
	return SettingsOperationResult{
		AppliedNow:      []string{"logging", "default_agent", "memory", "scheduler.system_tasks"},
		RestartRequired: []string{"gateway", "channels", "agents", "llm.default_provider", "llm.default_model", "llm.providers", "tools", "mcp"},
	}, nil
}

func writeConfigFileAtomically(path string, raw []byte) error {
	tmpPath := path + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	if _, err := file.Write(raw); err != nil {
		file.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync temp config: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("replace config file: %w", err)
	}
	return nil
}

func (g *Gateway) restoreConfig(path string, previousRaw []byte, previousCfg *config.Config) error {
	if err := writeConfigFileAtomically(path, previousRaw); err != nil {
		return err
	}
	if previousCfg != nil {
		g.setCurrentConfig(previousCfg)
	}
	return nil
}

func (g *Gateway) saveConfigYAML(raw string) (*SettingsOperationResult, []SettingsValidationIssue, error) {
	cfg, issues, err := validateConfigYAML(raw, g.configPath)
	if err != nil {
		settingsLog.Error("Configuration validation failed before save", logger.Fields{"error": err.Error(), "config_path": g.configPath})
		return nil, nil, fmt.Errorf("validate config yaml: %w", err)
	}
	if len(issues) > 0 {
		settingsLog.Warn("Configuration validation reported issues", logger.Fields{"config_path": g.configPath, "issue_count": len(issues)})
		return nil, issues, nil
	}

	path, err := g.resolvedConfigPath()
	if err != nil {
		settingsLog.Error("Configuration save failed because config path could not be resolved", logger.Fields{"error": err.Error(), "config_path": g.configPath})
		return nil, nil, err
	}

	previousRaw, err := os.ReadFile(path)
	if err != nil {
		settingsLog.Error("Configuration save failed while reading current config", logger.Fields{"error": err.Error(), "config_path": path})
		return nil, nil, fmt.Errorf("read current config: %w", err)
	}
	previousCfg := g.currentConfig()

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	backupPath := filepath.Join(dir, fmt.Sprintf("%s.bak.%d", base, time.Now().Unix()))

	if err := os.WriteFile(backupPath, previousRaw, 0o644); err != nil {
		settingsLog.Error("Configuration backup write failed", logger.Fields{"error": err.Error(), "config_path": path, "backup_path": backupPath})
		return nil, nil, fmt.Errorf("write backup config: %w", err)
	}
	if err := writeConfigFileAtomically(path, []byte(raw)); err != nil {
		settingsLog.Error("Configuration file write failed", logger.Fields{"error": err.Error(), "config_path": path})
		return nil, nil, err
	}

	result, err := g.applyConfig(cfg)
	if err != nil {
		settingsLog.Error("Configuration apply failed; attempting rollback", logger.Fields{"error": err.Error(), "config_path": path, "backup_path": backupPath})
		rollbackErr := g.restoreConfig(path, previousRaw, previousCfg)
		if rollbackErr != nil {
			settingsLog.Error("Configuration rollback failed after apply error", logger.Fields{"error": rollbackErr.Error(), "config_path": path, "backup_path": backupPath})
			return nil, nil, fmt.Errorf("apply config: %v; rollback failed: %w", err, rollbackErr)
		}
		result.RolledBack = true
		result.BackupPath = backupPath
		settingsLog.Warn("Configuration rollback completed after apply error", logger.Fields{"config_path": path, "backup_path": backupPath})
		return &result, nil, fmt.Errorf("apply config: %w", err)
	}
	result.BackupPath = backupPath
	settingsLog.Info("Configuration saved and applied", logger.Fields{"config_path": path, "backup_path": backupPath})
	return &result, nil, nil
}

func (g *Gateway) reloadConfigFromDisk() (*SettingsOperationResult, []SettingsValidationIssue, error) {
	path, err := g.resolvedConfigPath()
	if err != nil {
		return nil, nil, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil, err
	}
	if err := cfg.ValidateWithEnv(); err != nil {
		if issues := validationIssuesFromError(err); len(issues) > 0 {
			return nil, issues, nil
		}
		return nil, nil, err
	}
	result, err := g.applyConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("apply config: %w", err)
	}
	return &result, nil, nil
}
