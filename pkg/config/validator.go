package config

import (
	"fmt"
	"os"
	"slices"
	"strings"

	robfigcron "github.com/robfig/cron/v3"
)

// ValidationError 配置验证错误
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("config validation error: %s - %s", e.Field, e.Message)
}

// ValidationErrors 多个验证错误
type ValidationErrors []ValidationError

func (errs ValidationErrors) Error() string {
	var messages []string
	for _, err := range errs {
		messages = append(messages, err.Error())
	}
	return strings.Join(messages, "\n")
}

// Validate 验证配置
func (c *Config) Validate() error {
	var errs ValidationErrors

	if err := c.validateGateway(); err != nil {
		errs = append(errs, err...)
	}
	if err := c.validateAgents(); err != nil {
		errs = append(errs, err...)
	}
	if err := c.validateLLM(); err != nil {
		errs = append(errs, err...)
	}
	if err := c.validateMemory(); err != nil {
		errs = append(errs, err...)
	}
	if err := c.validateScheduler(); err != nil {
		errs = append(errs, err...)
	}
	if err := c.validateChannels(); err != nil {
		errs = append(errs, err...)
	}
	if err := c.validatePlugins(); err != nil {
		errs = append(errs, err...)
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

func (c *Config) validateGateway() ValidationErrors {
	var errs ValidationErrors

	if c.Gateway.Port < 1 || c.Gateway.Port > 65535 {
		errs = append(errs, ValidationError{
			Field:   "gateway.port",
			Message: "port must be between 1 and 65535",
		})
	}
	if c.Gateway.Host == "" {
		errs = append(errs, ValidationError{
			Field:   "gateway.host",
			Message: "host cannot be empty",
		})
	}

	return errs
}

func (c *Config) validateAgents() ValidationErrors {
	var errs ValidationErrors

	if len(c.Agents) == 0 {
		errs = append(errs, ValidationError{
			Field:   "agents",
			Message: "at least one agent must be configured",
		})
		return errs
	}

	for id, agent := range c.Agents {
		if agent.Role != "admin" && agent.Role != "employee" && agent.Role != "customer" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("agents.%s.role", id),
				Message: "role must be 'admin', 'employee' or 'customer'",
			})
		}

		if agent.Workspace == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("agents.%s.workspace", id),
				Message: "workspace cannot be empty",
			})
		} else {
			if err := os.MkdirAll(agent.Workspace, 0o755); err != nil {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("agents.%s.workspace", id),
					Message: fmt.Sprintf("workspace directory cannot be created: %s", agent.Workspace),
				})
			}
		}

		for i, dir := range agent.Permissions.AuthorizedDirectories {
			if strings.TrimSpace(dir) == "" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("agents.%s.permissions.authorized_directories[%d]", id, i),
					Message: "directory cannot be empty",
				})
			}
		}

		for i, toolName := range agent.Permissions.AuthorizedTools {
			if strings.TrimSpace(toolName) == "" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("agents.%s.permissions.authorized_tools[%d]", id, i),
					Message: "tool name cannot be empty",
				})
			}
		}

		for i, agentID := range agent.Permissions.AuthorizedSubagents {
			agentID = strings.TrimSpace(agentID)
			if agentID == "" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("agents.%s.permissions.authorized_subagents[%d]", id, i),
					Message: "subagent id cannot be empty",
				})
				continue
			}
			if agentID == "*" {
				if strings.TrimSpace(agent.Role) != "admin" {
					errs = append(errs, ValidationError{
						Field:   fmt.Sprintf("agents.%s.permissions.authorized_subagents[%d]", id, i),
						Message: "wildcard subagent authorization is only allowed for admin agents",
					})
				}
				continue
			}
			if agentID == "experience-curator" && strings.TrimSpace(agent.Role) != "admin" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("agents.%s.permissions.authorized_subagents[%d]", id, i),
					Message: "system subagent experience-curator requires admin role",
				})
				continue
			}
			if _, ok := c.Agents[agentID]; !ok && agentID != "experience-curator" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("agents.%s.permissions.authorized_subagents[%d]", id, i),
					Message: fmt.Sprintf("subagent %q is not configured", agentID),
				})
			}
		}

		for i, name := range agent.Permissions.AuthorizedExternalSubagents {
			name = strings.TrimSpace(name)
			if name == "" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("agents.%s.permissions.authorized_external_subagents[%d]", id, i),
					Message: "external subagent name cannot be empty",
				})
			}
		}
	}

	return errs
}

func (c *Config) validateLLM() ValidationErrors {
	var errs ValidationErrors

	if c.LLM.DefaultProvider == "" {
		errs = append(errs, ValidationError{
			Field:   "llm.default_provider",
			Message: "default provider must be specified",
		})
	}
	if len(c.LLM.Providers) == 0 {
		errs = append(errs, ValidationError{
			Field:   "llm.providers",
			Message: "at least one LLM provider must be configured",
		})
		return errs
	}
	if c.LLM.DefaultModel == "" {
		errs = append(errs, ValidationError{
			Field:   "llm.default_model",
			Message: "default model must be specified",
		})
	}

	defaultProvider, ok := c.LLM.Providers[c.LLM.DefaultProvider]
	if !ok {
		errs = append(errs, ValidationError{
			Field:   "llm.default_provider",
			Message: fmt.Sprintf("default provider '%s' not found in providers", c.LLM.DefaultProvider),
		})
	} else if c.LLM.DefaultModel != "" && !providerHasModel(defaultProvider.Models, c.LLM.DefaultModel) {
		errs = append(errs, ValidationError{
			Field:   "llm.default_model",
			Message: fmt.Sprintf("default model '%s' is not declared under provider '%s'", c.LLM.DefaultModel, c.LLM.DefaultProvider),
		})
	}

	for name, provider := range c.LLM.Providers {
		if provider.Type == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("llm.providers.%s.type", name),
				Message: "provider type cannot be empty",
			})
		}
		if provider.DefaultModel == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("llm.providers.%s.default_model", name),
				Message: "default_model must be specified",
			})
		}
		if len(provider.Models) == 0 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("llm.providers.%s.models", name),
				Message: "at least one model must be specified",
			})
		} else {
			seenModelIDs := make(map[string]struct{}, len(provider.Models))
			for i, model := range provider.Models {
				fieldPrefix := fmt.Sprintf("llm.providers.%s.models[%d]", name, i)
				modelID := strings.TrimSpace(model.ID)
				if modelID == "" {
					errs = append(errs, ValidationError{
						Field:   fieldPrefix + ".id",
						Message: "id cannot be empty",
					})
				} else {
					if _, exists := seenModelIDs[modelID]; exists {
						errs = append(errs, ValidationError{
							Field:   fieldPrefix + ".id",
							Message: fmt.Sprintf("duplicate model id %q", modelID),
						})
					} else {
						seenModelIDs[modelID] = struct{}{}
					}
				}
				if strings.TrimSpace(model.Name) == "" {
					errs = append(errs, ValidationError{
						Field:   fieldPrefix + ".name",
						Message: "name cannot be empty",
					})
				}
				switch normalizeProviderModelType(model.Type) {
				case ProviderModelTypeChat, ProviderModelTypeEmbedding, ProviderModelTypeRerank:
				default:
					errs = append(errs, ValidationError{
						Field:   fieldPrefix + ".type",
						Message: "type must be 'chat', 'embedding' or 'rerank'",
					})
				}
				if model.ContextWindow < 0 {
					errs = append(errs, ValidationError{
						Field:   fieldPrefix + ".context_window",
						Message: "context_window cannot be negative",
					})
				}
				if model.MaxOutputTokens < 0 {
					errs = append(errs, ValidationError{
						Field:   fieldPrefix + ".max_output_tokens",
						Message: "max_output_tokens cannot be negative",
					})
				}
			}
			if provider.DefaultModel != "" && !providerHasModel(provider.Models, provider.DefaultModel) {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("llm.providers.%s.default_model", name),
					Message: "default_model must exist in models",
				})
			}
		}
		if provider.Thinking != nil {
			thinkingType := strings.TrimSpace(strings.ToLower(provider.Thinking.Type))
			if provider.Type != "deepseek" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("llm.providers.%s.thinking", name),
					Message: "thinking is only supported by provider type 'deepseek'",
				})
			} else if thinkingType != "enabled" && thinkingType != "disabled" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("llm.providers.%s.thinking.type", name),
					Message: "thinking.type must be 'enabled' or 'disabled'",
				})
			}
		}
		if strings.TrimSpace(provider.ReasoningReplay) != "" {
			replay := strings.TrimSpace(strings.ToLower(provider.ReasoningReplay))
			if provider.Type != "deepseek" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("llm.providers.%s.reasoning_replay", name),
					Message: "reasoning_replay is only supported by provider type 'deepseek'",
				})
			} else if replay != "none" && replay != "tool_calls_only" && replay != "all" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("llm.providers.%s.reasoning_replay", name),
					Message: "reasoning_replay must be 'none', 'tool_calls_only', or 'all'",
				})
			}
		}
	}

	for name, agent := range c.Agents {
		provider, ok := c.LLM.Providers[agent.Provider]
		if !ok {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("agents.%s.provider", name),
				Message: fmt.Sprintf("provider '%s' not found in llm.providers", agent.Provider),
			})
			continue
		}
		if agent.Model != "" && !providerHasModel(provider.Models, agent.Model) {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("agents.%s.model", name),
				Message: fmt.Sprintf("model '%s' is not declared under provider '%s'", agent.Model, agent.Provider),
			})
		}
	}

	return errs
}

func (c *Config) validateMemory() ValidationErrors {
	var errs ValidationErrors

	if c.Memory.ShortTerm.MaxMessages < 1 {
		errs = append(errs, ValidationError{
			Field:   "memory.short_term.max_messages",
			Message: "max_messages must be at least 1",
		})
	}
	if c.Memory.ShortTerm.MaxTokens < 100 {
		errs = append(errs, ValidationError{
			Field:   "memory.short_term.max_tokens",
			Message: "max_tokens must be at least 100",
		})
	}
	if c.Memory.ShortTerm.KeepRecent < 0 {
		errs = append(errs, ValidationError{
			Field:   "memory.short_term.keep_recent",
			Message: "keep_recent cannot be negative",
		})
	}
	if c.Memory.MediumTerm.MaxSize < 100 {
		errs = append(errs, ValidationError{
			Field:   "memory.medium_term.max_size",
			Message: "max_size must be at least 100",
		})
	}

	if c.Memory.LongTerm.Vector.Enabled {
		if c.Memory.LongTerm.Vector.Embedder.Type == "" {
			errs = append(errs, ValidationError{
				Field:   "memory.long_term.vector.embedder.type",
				Message: "embedder type is required when vector is enabled",
			})
		}
		if c.Memory.LongTerm.Vector.PrefetchLimit < 1 {
			errs = append(errs, ValidationError{
				Field:   "memory.long_term.vector.prefetch_limit",
				Message: "prefetch_limit must be at least 1",
			})
		}
		if c.Memory.LongTerm.Vector.ScoreThreshold < 0 || c.Memory.LongTerm.Vector.ScoreThreshold > 1 {
			errs = append(errs, ValidationError{
				Field:   "memory.long_term.vector.score_threshold",
				Message: "score_threshold must be between 0 and 1",
			})
		}
	}

	return errs
}

func (c *Config) validateScheduler() ValidationErrors {
	var errs ValidationErrors

	if strings.TrimSpace(c.Scheduler.SystemTasks.MemoryCuration.Schedule) == "" {
		errs = append(errs, ValidationError{
			Field:   "scheduler.system_tasks.memory_curation.schedule",
			Message: "schedule cannot be empty",
		})
	} else if _, err := robfigcron.ParseStandard(c.Scheduler.SystemTasks.MemoryCuration.Schedule); err != nil {
		errs = append(errs, ValidationError{
			Field:   "scheduler.system_tasks.memory_curation.schedule",
			Message: fmt.Sprintf("invalid cron expression: %v", err),
		})
	}

	if strings.TrimSpace(c.Scheduler.SystemTasks.ExperienceCuration.Schedule) == "" {
		errs = append(errs, ValidationError{
			Field:   "scheduler.system_tasks.experience_curation.schedule",
			Message: "schedule cannot be empty",
		})
	} else if _, err := robfigcron.ParseStandard(c.Scheduler.SystemTasks.ExperienceCuration.Schedule); err != nil {
		errs = append(errs, ValidationError{
			Field:   "scheduler.system_tasks.experience_curation.schedule",
			Message: fmt.Sprintf("invalid cron expression: %v", err),
		})
	}

	return errs
}

func (c *Config) validateChannels() ValidationErrors {
	var errs ValidationErrors

	for name, channel := range c.Channels {
		if channel.Role != "admin" && channel.Role != "employee" && channel.Role != "customer" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("channels.%s.role", name),
				Message: "role must be 'admin', 'employee' or 'customer'",
			})
		}
		if channel.Agent != "" {
			if _, ok := c.Agents[channel.Agent]; !ok {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("channels.%s.agent", name),
					Message: fmt.Sprintf("agent '%s' not found in agents configuration", channel.Agent),
				})
			}
		}
	}

	return errs
}

func (c *Config) validatePlugins() ValidationErrors {
	var errs ValidationErrors

	if c.Plugins.WSServer.Port < 0 || c.Plugins.WSServer.Port > 65535 {
		errs = append(errs, ValidationError{
			Field:   "plugins.ws_server.port",
			Message: "port must be between 0 and 65535",
		})
	}

	for name, plugin := range c.Plugins.Plugins {
		if strings.TrimSpace(name) == "" {
			errs = append(errs, ValidationError{
				Field:   "plugins.plugins",
				Message: "plugin name cannot be empty",
			})
			continue
		}

		if !plugin.Enabled {
			continue
		}

		if plugin.Type != "local" && plugin.Type != "remote" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("plugins.plugins.%s.type", name),
				Message: "type must be 'local' or 'remote'",
			})
		}

		if plugin.Type == "local" && len(plugin.Command) == 0 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("plugins.plugins.%s.command", name),
				Message: "local plugin requires command",
			})
		}

		if plugin.Type == "remote" && strings.TrimSpace(plugin.URL) == "" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("plugins.plugins.%s.url", name),
				Message: "remote plugin requires url",
			})
		}

		if plugin.Restart != "" && plugin.Restart != "always" && plugin.Restart != "on-failure" && plugin.Restart != "never" {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("plugins.plugins.%s.restart", name),
				Message: "restart must be 'always', 'on-failure' or 'never'",
			})
		}

		if plugin.RestartDelay < 0 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("plugins.plugins.%s.restart_delay", name),
				Message: "restart_delay cannot be negative",
			})
		}

		if plugin.MaxRestarts < 0 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("plugins.plugins.%s.max_restarts", name),
				Message: "max_restarts cannot be negative",
			})
		}

		if plugin.ConnectTimeout < 0 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("plugins.plugins.%s.connect_timeout", name),
				Message: "connect_timeout cannot be negative",
			})
		}

		if plugin.RequestTimeout < 0 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("plugins.plugins.%s.request_timeout", name),
				Message: "request_timeout cannot be negative",
			})
		}

		for i, capability := range plugin.Capabilities.Allow {
			if capability != "tool" && capability != "provider" && capability != "channel" && capability != "hook" {
				errs = append(errs, ValidationError{
					Field:   fmt.Sprintf("plugins.plugins.%s.capabilities.allow[%d]", name, i),
					Message: "capability must be one of 'tool', 'provider', 'channel', 'hook'",
				})
			}
		}
	}

	// 校验工具调用兜底超时
	if c.Tools.DefaultTimeout < 0 {
		errs = append(errs, ValidationError{
			Field:   "tools.default_timeout",
			Message: "default_timeout cannot be negative",
		})
	}

	// 校验 MCP 服务器调用超时
	for name, server := range c.MCP.Servers {
		if server.CallTimeout < 0 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("mcp.servers.%s.call_timeout", name),
				Message: "call_timeout cannot be negative",
			})
		}
		if server.Timeout < 0 {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("mcp.servers.%s.timeout", name),
				Message: "timeout cannot be negative",
			})
		}
	}

	return errs
}

func containsString(items []string, target string) bool {
	return slices.Contains(items, target)
}

// MustValidate 验证配置，失败时 panic
func (c *Config) MustValidate() {
	if err := c.Validate(); err != nil {
		panic(fmt.Sprintf("Configuration validation failed:\n%v", err))
	}
}

func (c *Config) providersRequiringEnvValidation() map[string]ProviderConfig {
	providers := make(map[string]ProviderConfig)

	if defaultProvider, ok := c.LLM.Providers[c.LLM.DefaultProvider]; ok {
		providers[c.LLM.DefaultProvider] = defaultProvider
	}
	for _, agent := range c.Agents {
		if agent.Provider == "" {
			continue
		}
		provider, ok := c.LLM.Providers[agent.Provider]
		if !ok {
			continue
		}
		providers[agent.Provider] = provider
	}

	return providers
}

// ValidateWithEnv 验证配置并检查环境变量
func (c *Config) ValidateWithEnv() error {
	if err := c.Validate(); err != nil {
		return err
	}
	for name, provider := range c.providersRequiringEnvValidation() {
		switch provider.Type {
		case "openai":
			if provider.APIKey == "" && os.Getenv("OPENAI_API_KEY") == "" {
				return fmt.Errorf("provider %s requires OPENAI_API_KEY", name)
			}
		case "openai-compatible", "openai-responses-compatible", "gemini-compatible", "anthropic-compatible", "deepseek", "minimax", "openrouter", "dashscope", "dashscope-cn", "dashscope-coding", "dashscope-coding-cn", "groq", "mistral", "together", "fireworks", "perplexity", "moonshot", "nvidia", "litellm", "lmstudio", "vllm", "cloudflare-ai-gateway", "vercel-ai-gateway", "helicone", "xai", "azure", "google-ai-studio", "siliconflow", "zhipu", "zhipu-cn", "zhipu-coding", "zhipu-coding-cn", "baidu-qianfan", "tencent-hunyuan", "bytedance", "bytedance-cn", "iflytek-spark", "cerebras", "replicate", "sambanova", "akle", "kilo", "opencode", "cohere", "novita":
			if strings.TrimSpace(provider.BaseURL) == "" {
				return fmt.Errorf("provider %s requires base_url", name)
			}
		case "bedrock":
			if strings.TrimSpace(provider.Metadata["region"]) == "" && os.Getenv("AWS_REGION") == "" && os.Getenv("AWS_DEFAULT_REGION") == "" {
				return fmt.Errorf("provider %s requires metadata.region or AWS_REGION", name)
			}
		case "vertex-ai":
			project := strings.TrimSpace(provider.Metadata["project"])
			location := strings.TrimSpace(provider.Metadata["location"])
			if project == "" && os.Getenv("GOOGLE_CLOUD_PROJECT") == "" {
				return fmt.Errorf("provider %s requires metadata.project or GOOGLE_CLOUD_PROJECT", name)
			}
			if location == "" && strings.TrimSpace(provider.Metadata["region"]) == "" && os.Getenv("GOOGLE_CLOUD_LOCATION") == "" && os.Getenv("GOOGLE_CLOUD_REGION") == "" {
				return fmt.Errorf("provider %s requires metadata.location or GOOGLE_CLOUD_LOCATION", name)
			}
		case "ollama":
			continue
		case "anthropic":
			if provider.APIKey == "" && os.Getenv("ANTHROPIC_API_KEY") == "" {
				return fmt.Errorf("provider %s requires ANTHROPIC_API_KEY", name)
			}
		case "gemini":
			if provider.APIKey == "" && os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("GOOGLE_API_KEY") == "" {
				return fmt.Errorf("provider %s requires GEMINI_API_KEY or GOOGLE_API_KEY", name)
			}
		}
	}
	return nil
}
