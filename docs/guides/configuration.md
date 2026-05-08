# Configuration

[English](../guides/configuration.md) | [简体中文](../zh-CN/guides/configuration.md)

This guide describes the current config file structure and field responsibilities. It does not replace the install or first-run guides.

## 1. Config File Location

Default config file:

- Windows: `%USERPROFILE%\.evoduck\config\config.yaml`
- Linux/macOS: `~/.evoduck/config/config.yaml`

You can also use any path with `--config`.

## 2. Top-Level Structure

```yaml
gateway:
default_agent:
data_dir:
logging:
llm:
agents:
channels:
shared:
tools:
memory:
heartbeat:
scheduler:
plugins:
mcp:
```

## 3. `gateway`

```yaml
gateway:
  host: 127.0.0.1
  port: 18789
  token: ""
```

Controls gateway bind host, port, and optional gateway token.

## 4. `default_agent`

```yaml
default_agent: admin-bot
```

Used when a request does not explicitly select an agent.

## 5. `data_dir`

```yaml
data_dir: E:/path/to/runtime-root
```

Controls the runtime root location, including default agent workspaces, logs, sessions, users, scheduler state, and shared storage.

When unset, EvoDuck initializes its runtime under the default per-user root:

- Windows: `%USERPROFILE%\.evoduck`
- Linux/macOS: `~/.evoduck`

## 6. `llm`

```yaml
llm:
  default_provider: openai-compatible
  default_model: gpt-4o
  providers:
    openai-compatible:
      type: openai-compatible
      base_url: https://api.openai.com/v1
      api_key: ${OPENAI_API_KEY}
      default_model: gpt-4o
      models:
        - id: gpt-4o
          name: gpt-4o
          type: chat
          capabilities:
            vision: true
            reasoning: false
            tool_use: true
          context_window: 128000
          max_output_tokens: 16384
```

Each `models` item is now explicit: request model ID, display name, model type, capability flags, context window, and max output tokens.

Defines model providers and defaults. Environment-variable interpolation is supported for secrets.

Common provider families include OpenAI-compatible, Ollama, OpenAI, Gemini, Anthropic, Bedrock, and Vertex AI.

DeepSeek V4 uses an explicit provider type for its OpenAI-compatible extensions:

```yaml
llm:
  providers:
    deepseek:
      type: deepseek
      base_url: https://api.deepseek.com
      api_key: ${DEEPSEEK_API_KEY}
      default_model: deepseek-v4-pro
      thinking:
        type: enabled
      reasoning_replay: tool_calls_only
      models:
        - id: deepseek-v4-pro
          name: deepseek-v4-pro
          type: chat
          capabilities:
            vision: false
            reasoning: true
            tool_use: true
          context_window: 128000
          max_output_tokens: 16384
        - id: deepseek-v4-flash
          name: deepseek-v4-flash
          type: chat
          capabilities:
            vision: false
            reasoning: true
            tool_use: true
          context_window: 128000
          max_output_tokens: 16384
```

DeepSeek-only fields such as `thinking`, `reasoning_replay`, and `user_id` are enabled only when `type: deepseek` is configured. A plain `type: openai-compatible` provider remains generic even if its `base_url` or model name points to DeepSeek.

## 7. `agents`

```yaml
agents:
  admin-bot:
    workspace: ./agents/admin-bot
    role: admin
    provider: openai-compatible
    model: gpt-4o
    user_isolation:
      auto_create: true
      auto_profile: true
```

Each agent has its own workspace, role, model selection, and user-isolation policy. On first run, EvoDuck also creates scaffold files for the default agent workspace under the runtime root.

## 8. `channels`

```yaml
channels:
  webchat:
    type: webchat
    agent: admin-bot
    role: admin
```

Channels bind external or built-in message surfaces to agents.

Important built-ins:

- `webchat`: built-in web gateway
- `weixin`: Weixin QR-login channel
- `wecom`: WeCom AI Bot channel

## 9. `shared`

```yaml
shared:
  skills_dir: ./shared/skills
```

Defines shared Skill storage. On first run, EvoDuck creates the shared skills directory and writes built-in Skills into it without overwriting existing user changes.

## 10. `tools`

```yaml
tools:
  backend_call:
    endpoints: {}
```

Defines built-in tool configuration, including backend-call allowlists.

## 11. `memory`

Memory is split into short-term, medium-term, long-term, core memory, and bootstrap settings.

Common fields include:

- short-term message limits and TTL
- medium-term storage directory and extraction thresholds
- long-term vector settings
- cleanup policy
- core memory file
- bootstrap file loading limits

## 12. `heartbeat`

```yaml
heartbeat:
  enabled: false
  interval: 30m
  prompt: "Check if anything needs attention."
```

Controls optional periodic self-check prompts.

## 13. `scheduler`

```yaml
scheduler:
  system_tasks:
    memory_curation:
      schedule: "0 */3 * * *"
    experience_curation:
      schedule: "0 3 * * *"
```

Controls scheduled system tasks and user-defined schedules.

## 14. `plugins`

```yaml
plugins:
  ws_server:
    host: 127.0.0.1
    port: 19000
  plugins:
    echo-tool:
      enabled: true
      type: local
      command: ["go", "run", "./plugins/echo-tool"]
      restart: never
      capabilities:
        allow: ["tool"]
```

Configures EvoDuck WebSocket plugins. Plugins can provide tools, providers, channels, and hooks.

## 15. `mcp`

```yaml
mcp:
  servers: {}
```

Configures MCP servers. Prefer MCP for external tool integration unless you specifically need provider, channel, or hook extensions.

## 16. Validation Notes

For installation and first-run behavior, see [Install and First Run](install.md). For daily operations, see [Usage and Setup Wizard](usage.md).

Before running, check:

- default provider exists under `llm.providers`
- default agent exists under `agents`
- channel agent references are valid
- paths are writable
- required provider secrets are available via environment variables or config
