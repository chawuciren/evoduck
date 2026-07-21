---
name: evoduck-self-configuration
description: Comprehensive guide for configuring EvoDuck itself. Use this when the user asks the agent to inspect, explain, or modify EvoDuck configuration, setup flow, skills, memory, knowledge, tools, MCP, plugins, channels, proxy behavior, reload behavior, or runtime state.
license: MIT
compatibility: evoduck
metadata:
  evoduck:
    role: admin
    tags: [meta, evoduck, configuration, memory, skills, reload]
---

# EvoDuck Self Configuration Guide

Use this guide when the user asks you to configure EvoDuck, explain what this EvoDuck instance can do, change runtime behavior, add or update skills, adjust tools, edit MCP or plugins, configure channels, inspect memory routing, or diagnose why a configuration change did not take effect.

## Identity And Scope

You are an EvoDuck agent running inside an EvoDuck service.

Your behavior is shaped by these sources, in order of practical importance:
- System and developer instructions from the runtime
- Tool availability in the current prompt
- Loaded skill list and any skill the agent explicitly uses
- Agent workspace bootstrap files such as `AGENTS.md`, `SOUL.md`, `TOOLS.md`, `IDENTITY.md`, `HEARTBEAT.md`, and `BOOTSTRAP.md`
- User-isolated `USER.md`, `MEMORY.md`, and daily memory files when user isolation is active
- The YAML config file loaded by the service

Do not guess missing runtime details. Inspect the config, workspace files, tools, and skill list before changing behavior.

## Installation And Config Layout

Default installation layout:
- Windows app root: `%USERPROFILE%/.evoduck`
- Linux/macOS app root: `~/.evoduck`
- Default config file: `<app-root>/config/config.yaml`
- Runtime root: `data_dir` if configured, otherwise the app root
- Agent workspaces: usually `<data_dir>/agents/<agent-id>`
- Shared skills: `shared.skills_dir`, defaulting to `<app-root>/shared/skills`
- Logs: usually `<app-root>/logs`

Custom config paths are supported through `--config`. When a custom config path is used:
- the config directory is the parent of that file
- if the config file is under a `config/` directory, the runtime root defaults to that directory's parent
- otherwise the runtime root defaults to the config file's parent directory
- relative paths in config are resolved against the runtime root, not the current shell directory

Runtime initialization does the following:
- creates config and runtime directories
- seeds a default config when missing
- expands environment variables in config
- applies defaults and normalizes paths
- ensures built-in skills exist in the shared skills directory
- ensures agent scaffold files exist for configured agent workspaces

## First-Run Setup

The first-run flow is catalog-driven and interactive. It writes the config file to disk after a final validation pass.

### Trigger

- `evoduck run` triggers first-run setup automatically when the config still matches the seeded default template **and** the default provider still needs credentials. The exact detection rules are:
  - `default_agent` is `admin-bot`
  - `default_provider` is the seeded default and `default_model` is the seeded default model
  - exactly one agent (`admin-bot`, role `admin`) and one channel (`webchat`)
  - the default provider block still matches the provider catalog's seeded models exactly
  - the default provider's `api_key` is empty **and** the provider type is not `ollama` (ollama needs no key)
- `evoduck setup` runs the same setup flow explicitly. Re-running it on an already-configured instance is a no-op when the config no longer matches the seeded default.
- A config that was hand-edited away from the seeded default will not auto-trigger setup, even if it is missing credentials.

### What the wizard collects

The wizard runs these steps in order:

1. **Provider** — choose from `FirstRunProviderCatalog()`. Every catalog entry supports first-run, both vendor-native types (`openai`, `anthropic`, `gemini`, `ollama`, `bedrock`, `vertex-ai`, `azure`) and compatible/preset types. Choices accept the numeric alias, the type name, or an alias.
2. **Credentials and base URL** — gathered per provider kind:
   - `openai`, `anthropic`, `gemini`: API key required (anthropic/openai also prompt base URL with default).
   - `ollama`: base URL only, no key.
   - `bedrock`, `vertex-ai`: no model list fetch required; cloud metadata comes from env or `provider.metadata`.
   - the generic OpenAI-/Gemini-/Anthropic-compatible types: validated base URL + optional API key.
   - `minimax` / `minimax-cn` and all other compatible presets: base URL with default + optional API key.
3. **Default model** — the wizard attempts a live `ListModels` call against the chosen provider; if it succeeds it lists the discovered models (plus a "custom model name" option) and offers the catalog default as the default. If the live fetch fails it falls back to manual entry (required for the generic compatible types, optional-with-default for others).
4. **Gateway host and port** — defaults `127.0.0.1` / `18789`; port validated to `1..65535`.
5. **Optional channels** — `webchat` is always included as the built-in gateway entry and is not offered for setup. Optional catalog entries (`weixin`, `wecom`, and any plugin-provided channel that declares first-run support) are listed with their setup kind:
   - `weixin`: QR-login flow that yields the channel token, user id, and optional account metadata
   - `wecom`: interactive credential entry for `bot_id` and `secret`
   - This step can be skipped (`s`) and redone later via `evoduck channel add` or by editing the config.
6. **Save and validate** — the wizard writes the config, applies runtime normalization, and runs `ValidateWithEnv()` (which includes both structural validation and provider environment checks). If validation fails the config is not saved and the error is shown.

### What gets written

- `llm.providers` is replaced with a single block for the chosen provider, seeded from the provider catalog's model list (ensuring the chosen default model is present).
- The `admin-bot` agent's provider/model are pointed at the chosen provider and default model.
- `gateway.host` / `gateway.port` are written.
- Any additional channel chosen in step 5 is appended alongside the always-present `webchat` entry.

The printed summary shows the chosen provider and the config file path. Provider keys are written into the config (consider `${ENV_VAR}` substitution if you prefer to keep them out of the file).

### First-run catalogs

First-run provider choices come from `providerPresets` filtered by `SupportsFirstRun`; all current catalog entries are first-run-eligible, including vendor-native and compatible/preset types such as:

- `openai-compatible`
- `openai-responses-compatible`
- `gemini-compatible`
- `anthropic-compatible`
- `ollama`
- `openai`
- `gemini`
- `anthropic`
- `deepseek`
- `bedrock`
- `vertex-ai`
- `azure`
- and the full set of catalog presets (dashscope, zhipu, bytedance, openrouter, xai, groq, mistral, cohere, novita, moonshot, nvidia, perplexity, together, fireworks, cerebras, replicate, sambanova, iflytek-spark, baidu-qianfan, tencent-hunyuan, siliconflow, minimax/minimax-cn, litellm/lmstudio/vllm, xiaomi-mimo, cloudflare-ai-gateway, vercel-ai-gateway, helicone, portkey, akle, kilo, opencode, google-ai-studio)

First-run channel choices come from `channelCatalog` filtered by `SupportsFirstRun`:

- `webchat` — built-in gateway web interface (always present, never set up via the wizard)
- `weixin` — QR-login flow that yields token and optional account metadata
- `wecom` — AI Bot credentials using `bot_id` and `secret`

### Post-setup

After setup, `evoduck run` starts the gateway. Channels added later with `evoduck channel add` and most `logging`/`memory`/`scheduler` settings can be applied with `system_reload scope="config"`; provider, tool-registration, MCP, plugin, proxy-process, and listening-port changes require a restart (see Reload Matrix).

## Configuration Files

The config file is YAML. Current top-level sections include:
- `gateway`: host, port, access token
- `default_agent`: default agent id
- `data_dir`: runtime data root
- `tool_result_condense_limit`: oversize tool-result truncation threshold (default `32768`)
- `image_auto_compress_limit`: image auto-compress threshold (default `32768`)
- `agents`: agent workspace, role, permissions, provider/model overrides, user isolation, per-agent generation options
- `shared`: shared skill directory
- `llm`: default provider, default model, provider definitions, model options
- `channels`: webchat, weixin, wecom, or plugin-provided channel bindings
- `plugins`: plugin websocket server and plugin definitions
- `tools`: backend-call, session, and global tool-call timeout configuration
- `memory`: short-term, medium-term, long-term, core memory, and bootstrap limits
- `heartbeat`: periodic self-check prompt settings
- `scheduler`: built-in system task schedules
- `mcp`: local or remote MCP server definitions
- `logging`: runtime logging settings
- `proxy`: global and per-surface proxy policy
- `daemon`: daemon control settings
- `session_archive`: `/resume` session archive retention (default enabled)
- `fusion`: Fusion roundtable consultation tool (admin-only)
- `image_describe`: vision-delegated image description tool (all roles)

Sensitive fields include `token`, `api_key`, `secret`, passwords, and auth headers. Never expose secret values in responses. Prefer environment variables such as `${OPENAI_API_KEY}` in config.

## Complete Config Template

Use this as a field reference, not as a mandatory starting point. Omit sections you do not need only when defaults or empty maps are acceptable.

```yaml
gateway:
  host: 127.0.0.1
  port: 18789
  token: "" # Sensitive. Optional gateway auth token.

default_agent: admin-bot
data_dir: "~/.evoduck"
tool_result_condense_limit: 32768 # default 32*1024
image_auto_compress_limit: 32768 # default 32*1024

logging:
  level: INFO # DEBUG, INFO, WARN, ERROR
  json_mode: false
  color: true

proxy:
  enabled: false
  type: http # http or socks5
  http:
    url: ""
    username: ""
    password: "" # Sensitive
  socks5:
    url: ""
    username: ""
    password: "" # Sensitive
  no_proxy: []
  controls:
    llm:
      enabled: false
      providers: {}
      type: ""
    channels:
      default: false
      per_channel: {}
      type: ""
    tools:
      enabled: false
      per_tool: {}
      type: ""
    mcp:
      enabled: false
      per_server: {}
      type: ""
    plugin:
      enabled: false
      per_plugin: {}
      type: ""
    exec:
      enabled: false
      per_command: {}
      type: ""
    subagents:
      internal:
        enabled: null # null means inherit parent process behavior
      external:
        enabled: false
        per_agent: {}
        type: ""
    update:
      enabled: false
      type: ""

daemon:
  control_port: 18791

llm:
  default_provider: openai-compatible
  default_model: gpt-4o
  providers:
    openai-compatible:
      type: openai-compatible
      base_url: https://api.openai.com/v1
      api_key: ${OPENAI_API_KEY} # Sensitive. Prefer env vars.
      headers: {}
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
        - id: text-embedding-3-small
          name: text-embedding-3-small
          type: embedding
          capabilities:
            vision: false
            reasoning: false
            tool_use: false
          context_window: 8192
          max_output_tokens: 0
      tool_choice: auto # auto, none, required, or a specific tool/function name
      parallel_tool_calls: true
      response_format:
        type: text # text, json_object, or json_schema
      stop: []
      presence_penalty: 0
      frequency_penalty: 0
      max_completion_tokens: 0 # 0 means omit provider request field
      reasoning_effort: "" # provider-specific
      verbosity: "" # provider-specific
      user: ""
      safety_identifier: ""
      service_tier: ""
      n: 0
      seed: null
      logprobs: false
      top_logprobs: 0
      store: null
      include_usage: null
      metadata: {}
      chat_template_kwargs: {}

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

    anthropic:
      type: anthropic
      base_url: https://api.anthropic.com
      api_key: ${ANTHROPIC_API_KEY}
      default_model: claude-sonnet-4-5
      models:
        - id: claude-sonnet-4-5
          name: claude-sonnet-4-5
          type: chat
          capabilities:
            vision: true
            reasoning: true
            tool_use: true
          context_window: 200000
          max_output_tokens: 64000

    gemini:
      type: gemini
      api_key: ${GEMINI_API_KEY}
      default_model: gemini-2.5-flash
      models:
        - id: gemini-2.5-flash
          name: gemini-2.5-flash
          type: chat
          capabilities:
            vision: true
            reasoning: true
            tool_use: true
          context_window: 1048576
          max_output_tokens: 65536

    ollama:
      type: ollama
      base_url: http://localhost:11434/v1
      default_model: qwen2.5
      models:
        - id: qwen2.5
          name: qwen2.5
          type: chat
          capabilities:
            vision: true
            reasoning: true
            tool_use: true
          context_window: 128000
          max_output_tokens: 8192

    bedrock:
      type: bedrock
      default_model: anthropic.claude-3-5-sonnet-20240620-v1:0
      metadata:
        region: us-east-1
        profile: default
      models:
        - id: anthropic.claude-3-5-sonnet-20240620-v1:0
          name: anthropic.claude-3-5-sonnet-20240620-v1:0
          type: chat
          capabilities:
            vision: true
            reasoning: true
            tool_use: true
          context_window: 200000
          max_output_tokens: 8192

    vertex-ai:
      type: vertex-ai
      default_model: gemini-2.5-flash
      metadata:
        project: your-gcp-project
        location: us-central1
      models:
        - id: gemini-2.5-flash
          name: gemini-2.5-flash
          type: chat
          capabilities:
            vision: true
            reasoning: true
            tool_use: true
          context_window: 1048576
          max_output_tokens: 65536

agents:
  admin-bot:
    role: admin # admin, employee, customer
    workspace: "~/.evoduck/agents/admin-bot"
    provider: openai-compatible
    model: gpt-4o
    permissions:
      authorized_directories: [] # Empty means role/workspace defaults.
      authorized_tools: [] # Empty means default tool set for role.
      authorized_subagents: [] # Admin may use * to allow all internal subagents.
      authorized_external_subagents: []
    user_isolation:
      auto_create: true
      auto_profile: true
    temperature: null # 0.0-2.0, null means provider default
    max_tokens: 0 # 0 means omit provider request field
    top_p: null
    max_iterations: 100

channels:
  webchat:
    type: webchat
    name: WebChat
    role: admin
    agent: admin-bot
    token: ""

  weixin-example:
    type: weixin
    name: My Weixin
    role: employee
    agent: admin-bot
    token: ${WEIXIN_TOKEN}
    user_id: ""
    api_base_url: ""

  wecom-example:
    type: wecom
    name: WeCom Bot
    role: employee
    agent: admin-bot
    bot_id: ${WECOM_BOT_ID}
    secret: ${WECOM_SECRET}

shared:
  skills_dir: "~/.evoduck/shared/skills"

tools:
  default_timeout: 60s # global fallback timeout for all tool calls; 0 disables the fallback
  backend_call:
    endpoints:
      example-api:
        url: https://api.example.com/items
        method: GET
        auth:
          type: bearer
          token: ${EXAMPLE_API_TOKEN}
          header: Authorization
        allowed_roles:
          - admin
          - employee
        rate_limit: 60
        timeout: 30s
  session:
    enabled: true
    visibility:
      employee: user # user or self
      customer: self
    allow:
      employee:
        - sessions_list
        - sessions_history
        - sessions_send
        - sessions_run
      customer: []

memory:
  short_term:
    max_messages: 200
    max_tokens: 128000
    keep_recent: 10
    session_ttl: 168h
    cleanup_interval: 1h
    flush_before_compact: false
  medium_term:
    dir: memory
    max_size: 5000
    load_days: 7
    min_messages_to_extract: 5
    compression_threshold: 10000
  long_term:
    vector:
      enabled: false
      embedder:
        type: openai # openai, local, ollama, or implementation-specific
        model: text-embedding-3-small
        dimensions: 1536
        api_key: ${EMBEDDING_API_KEY}
        base_url: https://api.openai.com/v1
      prefetch_limit: 5
      score_threshold: 0.7
    dedup_threshold: 0.95
    cleanup_policy:
      check_interval: 24h
      min_age_days: 30
      batch_size: 30
      reference:
        medium_memory_days: 7
        include_core_memory: true
        include_access_stats: true
    compression_threshold: 15000
  core_memory:
    file: MEMORY.md
    auto_consolidate: true
    importance_threshold: 0.9
  bootstrap:
    max_file_chars: 20000
    max_total_chars: 150000
    warning_threshold: 0.8
    truncation_strategy: head # head or tail

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

plugins:
  ws_server:
    host: 127.0.0.1
    port: 19000
  plugins:
    echo-tool:
      enabled: false
      type: local # local or remote
      command: ["go", "run", "./plugins/echo-tool"]
      environment: {}
      url: ""
      token: "" # Sensitive
      restart: on-failure # always, on-failure, never, or empty default
      restart_delay: 5
      max_restarts: 3
      override: false
      capabilities:
        allow:
          - tool # tool, provider, channel, hook
      connect_timeout: 30
      request_timeout: 60

mcp:
  servers:
    local-search:
      type: local
      enabled: false
      command: ["npx", "-y", "one-search-mcp"]
      environment: {}
      timeout: 30000
      call_timeout: 0 # per-call fallback in ms; 0 uses tools.default_timeout

    remote-search:
      type: remote
      enabled: false
      url: https://mcp.example.com
      headers:
        Authorization: Bearer ${MCP_TOKEN}
      timeout: 30000
      call_timeout: 0

# Session archive powers the /resume feature. Enabled by default; the config
# below is the effective default behavior and can be omitted entirely.
# To DISABLE it, set enabled: false AND set max_per_key or max_age_hours to a
# non-zero value (see Config Field Reference for the exact rule).
session_archive:
  enabled: true
  max_per_key: 50
  max_age_hours: 720 # 30 days

# Fusion roundtable consultation tool. Registered only when enabled and panel
# is non-empty, and only for admin agents.
fusion:
  enabled: false
  mode: judge # judge | raw | both
  timeout: "120s"
  panel:
    - provider: deepseek # references an llm.providers entry
      model: deepseek-v4-pro
      label: DeepSeek # optional
  judge:
    provider: anthropic
    model: claude-sonnet-4-5
    label: Judge

# Vision-delegated image description tool. Lets a non-vision main model "see"
# images by calling a vision-capable model. Available to all roles when enabled.
image_describe:
  enabled: false
  provider: openai-compatible # references an llm.providers entry
  model: gpt-4o
  timeout: "60s"
```

## Config Field Reference

### `gateway`

- `gateway.host`: string. Listener host. Default is `127.0.0.1`. Must not be empty. Restart required.
- `gateway.port`: integer. Listener port. Default is `18789`. Must be `1..65535`. Restart required.
- `gateway.token`: string. Sensitive gateway auth token. Restart required for listener/auth behavior to be safely refreshed.

### `default_agent`, `data_dir`, Tool Result/Image Limits

- `default_agent`: string. Agent id used when a request does not explicitly select an agent. Config reload can update gateway/router default behavior where wired, but verify in runtime.
- `data_dir`: string path. Runtime data root for agents, users, sessions, scheduler state, subagents, and knowledge. Changing this after startup should be treated as restart-required.
- `tool_result_condense_limit`: integer. Threshold for large tool-result condensation behavior. Default `32768`. Treat as runtime-behavior config and verify after reload.
- `image_auto_compress_limit`: integer. Threshold for automatic image compression behavior. Default `32768`. Treat as runtime-behavior config and verify after reload.

### `logging`

- `logging.level`: enum `DEBUG`, `INFO`, `WARN`, `ERROR`. Runtime reloadable.
- `logging.json_mode`: bool. Runtime reloadable.
- `logging.color`: bool. Runtime reloadable.
- Environment override paths may still influence runtime logging behavior.

### `proxy`

Global proxy:
- `proxy.enabled`: bool. Master proxy switch.
- `proxy.type`: string. Common values are `http` or `socks5`.
- `proxy.http.url`, `proxy.socks5.url`: proxy endpoints.
- `proxy.http.username`, `proxy.http.password`, `proxy.socks5.username`, `proxy.socks5.password`: optional credentials. Sensitive.
- `proxy.no_proxy`: list of bypass hostnames or domains.

Fine-grained proxy controls:
- `proxy.controls.llm`: provider call proxy policy, including `enabled`, optional `providers`, and optional `type` override.
- `proxy.controls.channels`: default and per-channel proxy policy.
- `proxy.controls.tools`: default and per-tool proxy policy.
- `proxy.controls.mcp`: default and per-server proxy policy for MCP calls and MCP subprocess startup.
- `proxy.controls.plugin`: default and per-plugin proxy policy for plugin calls and subprocess startup.
- `proxy.controls.exec`: default and per-command proxy policy for exec commands.
- `proxy.controls.subagents.internal.enabled`: `null` means inherit parent process behavior.
- `proxy.controls.subagents.external`: default and per-agent proxy policy for external subagents.
- `proxy.controls.update`: proxy policy for update commands.

Proxy policy changes should be treated as runtime-sensitive and verified carefully; process-startup or integration-surface changes are safest with restart.

### `daemon`

- `daemon.control_port`: integer. Daemon control port. Default behavior is gateway port plus `2` when omitted by runtime defaults. Treat listener changes as restart-required.

### `llm`

- `llm.default_provider`: string key under `llm.providers`. Must exist.
- `llm.default_model`: string. Must be declared under the default provider's `models`.
- `llm.providers`: map of provider names to provider configs.

Provider fields:
- `type`: provider type. Required.
- `base_url`: string. Required for OpenAI-compatible preset types and many compatible providers.
- `api_key`: string. Sensitive. May be a literal or `${ENV_VAR}`.
- `headers`: map string to string. Sensitive as a group in UI masking.
- `default_model`: string. Required and must be present in `models`.
- `models`: list of model objects. Each item includes:
  - `id`: required unique request model id
  - `name`: required display name
  - `type`: required, one of `chat`, `embedding`, `rerank`
  - `capabilities`: chat-capability flags such as `vision`, `reasoning`, and `tool_use`
  - `context_window`: integer, must not be negative
  - `max_output_tokens`: integer, must not be negative
- `tool_choice`: string. `auto`, `none`, `required`, or a specific tool/function name.
- `thinking`: currently supported only for provider type `deepseek`, with `type` `enabled` or `disabled`.
- `parallel_tool_calls`: nullable bool.
- `response_format`: map. Common `type` values are `text`, `json_object`, and `json_schema`.
- `stop`: list of stop strings.
- `presence_penalty`, `frequency_penalty`: nullable numbers.
- `max_completion_tokens`: integer. If `0`, omitted from provider request.
- `reasoning_effort`: string. Provider-specific.
- `reasoning_replay`: currently supported only by `deepseek`; allowed values are `none`, `tool_calls_only`, or `all`.
- `verbosity`: string. Provider-specific.
- `user`, `user_id`, `safety_identifier`, `service_tier`: provider-specific routing or safety fields.
- `n`: integer. If `0`, omitted.
- `seed`: nullable integer.
- `logprobs`: bool.
- `top_logprobs`: integer.
- `store`: nullable bool.
- `include_usage`: nullable bool.
- `metadata`: map string to string. Provider-specific routing/safety fields. Used by `bedrock` (`region`, `profile`) and `vertex-ai` (`project`, `location`, `region`); see Provider Type Reference for which keys each cloud type reads.
- `chat_template_kwargs`: map string to any.

### `agents`

- `agents.<id>.role`: enum `admin`, `employee`, `customer`. Required.
- `agents.<id>.workspace`: string path. Required and must be creatable.
- `agents.<id>.permissions.authorized_directories`: list of paths. Empty means default role/workspace permissions.
- `agents.<id>.permissions.authorized_tools`: list of tool names. Empty means default tool set for the role.
- `agents.<id>.permissions.authorized_subagents`: list of internal subagent ids. Empty string entries are invalid. `*` is only allowed for admin agents. `experience-curator` requires admin role.
- `agents.<id>.permissions.authorized_external_subagents`: list of external subagent names. Empty string entries are invalid.
- `agents.<id>.provider`: provider name. Must exist in `llm.providers`.
- `agents.<id>.model`: model name. If non-empty, must be declared under that provider's `models`.
- `agents.<id>.user_isolation.auto_create`: bool.
- `agents.<id>.user_isolation.auto_profile`: bool.
- `agents.<id>.temperature`: nullable number.
- `agents.<id>.max_tokens`: integer. `0` means omit/default.
- `agents.<id>.top_p`: nullable number.
- `agents.<id>.max_iterations`: integer. Runtime defaults apply when non-positive.

Agent registration changes, roles, permissions, workspaces, provider/model choices, and LLM options should be treated as restart-required for active agents.

### `channels`

- `channels.<id>.type`: string. Built-ins include `webchat`, `weixin`, and `wecom`; plugins can define more.
- `channels.<id>.name`: string display name.
- `channels.<id>.role`: enum `admin`, `employee`, `customer`.
- `channels.<id>.agent`: string agent id. If non-empty, must reference an existing configured agent.
- `channels.<id>.token`: string. Sensitive. Used by Weixin and some plugin channels.
- `channels.<id>.user_id`: string. Weixin account user id.
- `channels.<id>.api_base_url`: string. Weixin API base URL. Empty uses built-in defaults.
- `channels.<id>.bot_id`: string. WeCom AI Bot id.
- `channels.<id>.secret`: string. Sensitive. WeCom AI Bot secret.

Channel config may be rebuilt by config reload, but connection-level changes can require reconnect or restart. Treat new channel types from plugins as restart-required unless a supported hot path is known.

### `shared`

- `shared.skills_dir`: string path. Shared skills are loaded from this directory. Changing this after manager initialization should be treated as restart-required. New or edited skill files inside the current directory can be loaded with `system_reload scope="skills"`.

### `tools` (top-level)

- `tools.default_timeout`: Go duration. Global fallback timeout applied to all tool calls (built-in, plugin, and MCP) when the call has no more specific timeout. Default `60s`. `0` disables the fallback. Changes take effect for newly registered agents, so treat as restart-required for active agents.

### `tools.backend_call`

- `tools.backend_call.endpoints`: map of endpoint names. If empty, `backend_call` may not be registered.
- `url`: endpoint URL.
- `method`: HTTP method string.
- `auth.type`: auth mode string interpreted by the backend_call tool.
- `auth.token`: sensitive auth token.
- `auth.header`: auth header name.
- `allowed_roles`: list of roles allowed to call the endpoint.
- `rate_limit`: integer request limit used by the tool implementation.
- `timeout`: Go duration, such as `30s` or `1m`.

Backend endpoint additions or removals affect tool registration and should be treated as restart-required for active agents.

### `tools.session`

- `enabled`: bool. Enables session tools policy.
- `visibility.employee`: string policy for employee role.
- `visibility.customer`: string policy for customer role.
- `allow.employee`: list of session tool names allowed for employee role.
- `allow.customer`: list of session tool names allowed for customer role.

Session tool availability is determined by role, policy, explicit agent tool allowlists, and current user context. Tool registration changes should be treated as restart-required for active agents.

### `memory.short_term`

- `max_messages`: integer. Must be at least `1`.
- `max_tokens`: integer. Must be at least `100`.
- `keep_recent`: integer. Must not be negative.
- `session_ttl`: duration.
- `cleanup_interval`: duration.
- `flush_before_compact`: bool.

### `memory.medium_term`

- `dir`: string.
- `max_size`: integer. Must be at least `100`.
- `load_days`: integer.
- `min_messages_to_extract`: integer.
- `compression_threshold`: integer.

### `memory.long_term`

- `vector.enabled`: bool.
- `vector.embedder.type`: required when vector memory is enabled.
- `vector.embedder.model`: string.
- `vector.embedder.dimensions`: integer.
- `vector.embedder.api_key`: sensitive string.
- `vector.embedder.base_url`: string.
- `vector.prefetch_limit`: integer. Must be at least `1` when vector memory is enabled.
- `vector.score_threshold`: number. Must be between `0` and `1` when vector memory is enabled.
- `dedup_threshold`: number.
- `cleanup_policy.check_interval`: duration.
- `cleanup_policy.min_age_days`: integer.
- `cleanup_policy.batch_size`: integer.
- `cleanup_policy.reference.medium_memory_days`: integer.
- `cleanup_policy.reference.include_core_memory`: bool.
- `cleanup_policy.reference.include_access_stats`: bool.
- `compression_threshold`: integer.

### `memory.core_memory` And `memory.bootstrap`

- `core_memory.file`: string. Default is usually `MEMORY.md`.
- `core_memory.auto_consolidate`: bool.
- `core_memory.importance_threshold`: number.
- `bootstrap.max_file_chars`: integer.
- `bootstrap.max_total_chars`: integer.
- `bootstrap.warning_threshold`: number.
- `bootstrap.truncation_strategy`: string. Common values are `head` and `tail`.

### `heartbeat` And `scheduler`

- `heartbeat.enabled`: bool.
- `heartbeat.interval`: duration.
- `heartbeat.prompt`: string.
- `scheduler.system_tasks.memory_curation.schedule`: cron expression. Must parse as standard cron.
- `scheduler.system_tasks.experience_curation.schedule`: cron expression. Must parse as standard cron.

### `plugins`

- `plugins.ws_server.host`: string.
- `plugins.ws_server.port`: integer. Must be `0..65535`.
- `plugins.plugins.<name>.enabled`: bool.
- `plugins.plugins.<name>.type`: `local` or `remote` when enabled.
- `plugins.plugins.<name>.command`: list of strings. Required for enabled local plugins.
- `plugins.plugins.<name>.environment`: map string to string.
- `plugins.plugins.<name>.url`: string. Required for enabled remote plugins.
- `plugins.plugins.<name>.token`: sensitive string.
- `plugins.plugins.<name>.restart`: `always`, `on-failure`, `never`, or empty.
- `plugins.plugins.<name>.restart_delay`: integer. Must not be negative.
- `plugins.plugins.<name>.max_restarts`: integer. Must not be negative.
- `plugins.plugins.<name>.override`: bool.
- `plugins.plugins.<name>.capabilities.allow`: list containing only `tool`, `provider`, `channel`, or `hook`.
- `plugins.plugins.<name>.connect_timeout`: integer. Must not be negative.
- `plugins.plugins.<name>.request_timeout`: integer. Must not be negative.

Plugin process definitions, websocket server settings, and plugin capabilities should be treated as restart-required for active services.

### `mcp`

- `mcp.servers.<name>.type`: `local` or `remote`.
- `mcp.servers.<name>.enabled`: bool.
- `mcp.servers.<name>.command`: list of strings. Used by local MCP servers.
- `mcp.servers.<name>.environment`: map string to string. `MEMORY_FILE_PATH` is normalized as a path if present.
- `mcp.servers.<name>.url`: string. Used by remote MCP servers.
- `mcp.servers.<name>.headers`: map string to string. Sensitive as a group.
- `mcp.servers.<name>.timeout`: integer milliseconds. Initialization timeout.
- `mcp.servers.<name>.call_timeout`: integer milliseconds. Per-tool-call fallback timeout; `0` means fall back to `tools.default_timeout`.

MCP server process and connection changes normally require service restart to fully apply to active agents.

### `session_archive`

- `session_archive.enabled`: bool. Whether `/resume` session archiving is active.
- `session_archive.max_per_key`: integer. Maximum archive entries kept per key.
- `session_archive.max_age_hours`: integer. Maximum age in hours before an archive entry is purged.

Default-on behavior: archiving is enabled by default. To **disable** it you must set `enabled: false` **and** set either `max_per_key` or `max_age_hours` to a non-zero value. If you only set `enabled: false` with all-zero limits, archiving stays enabled (this guards `/resume` against being silently turned off by an under-specified block). The simplest way to disable is to write an explicit block with `enabled: false` and non-zero limits. When enabled, archive data is stored under `<data_dir>/sessions.archive`.

### `fusion`

- `fusion.enabled`: bool. Master switch; the tool is not registered when `false`.
- `fusion.panel`: list of `FusionMember` references (`provider`, `model`, optional `label`). Must be non-empty for the tool to register.
- `fusion.judge`: optional `FusionMember` used as the deciding model when `mode` is `judge` or `both`.
- `fusion.mode`: default return mode — `judge`, `raw`, or `both`.
- `fusion.timeout`: per-member call timeout (Go duration string, e.g. `"120s"`).

`provider` and `model` values must reference an existing entry under `llm.providers` and a model declared under that provider. The Fusion tool is registered only for **admin** agents; it requires `enabled: true` plus a non-empty `panel`. Treat changes as restart-required.

### `image_describe`

- `image_describe.enabled`: bool. Master switch; the tool is not registered when `false`.
- `image_describe.provider`: string. References an existing entry under `llm.providers`.
- `image_describe.model`: string. Model id declared under that provider (a vision-capable model, e.g. `gpt-4o`, `qwen-vl-plus`, `llava:7b`).
- `image_describe.timeout`: per-call timeout (Go duration string, e.g. `"60s"`).

The tool is registered for **all roles** (admin, employee, customer) when `enabled` is true and both `provider` and `model` are non-empty. It lets a non-vision main model delegate image understanding to the configured vision model. Treat changes as restart-required.

## Provider Type Reference

Direct registry provider types (each with a dedicated constructor in the LLM registry) include:
- `openai`
- `openai-compatible`
- `openai-responses-compatible`
- `anthropic`
- `anthropic-compatible`
- `gemini`
- `gemini-compatible`
- `google-ai-studio` (routes to the Gemini provider)
- `ollama`
- `deepseek`
- `openrouter`
- `dashscope`, `dashscope-cn`, `dashscope-coding`, `dashscope-coding-cn`
- `xai`
- `mistral`
- `perplexity`
- `cohere`
- `replicate`
- `bedrock`
- `vertex-ai`
- `azure`

The catalog presets above (`deepseek`, `openrouter`, `dashscope*`, `xai`, `mistral`, `perplexity`, `cohere`, `replicate`, `google-ai-studio`) have provider-specific behavior — they are not plain OpenAI-compatible pass-through. Catalog presets with OpenAI-compatible transport (verified at startup, not as a distinct registry type) include:
- `minimax`, `minimax-cn`
- `groq`, `nvidia`, `moonshot`, `together`, `fireworks`
- `cerebras`, `sambanova`
- `bytedance`, `bytedance-cn`, `baidu-qianfan`, `tencent-hunyuan`, `iflytek-spark`
- `siliconflow`
- `zhipu`, `zhipu-cn`, `zhipu-coding`, `zhipu-coding-cn`
- `novita`, `akle`, `kilo`, `opencode`
- `cloudflare-ai-gateway`, `vercel-ai-gateway`, `helicone`, `portkey`
- `xiaomi-mimo` (Xiaomi MiMo)
- `litellm`, `lmstudio`, `vllm` (local OpenAI-compatible servers)
- additional compatible presets from the provider catalog

Every catalog entry currently sets `SupportsFirstRun: true`, so all of the above are eligible in the first-run wizard. Provider type is matched case-insensitively and trimmed; numeric aliases (e.g. `1` = `openai-compatible`) are accepted by the wizard and `NormalizeFirstRunProviderName`.

Provider environment validation rules currently include:
- `openai`: needs `api_key` or `OPENAI_API_KEY`
- `anthropic`: needs `api_key` or `ANTHROPIC_API_KEY`
- `gemini`: needs `api_key`, `GEMINI_API_KEY`, or `GOOGLE_API_KEY`
- OpenAI-compatible, Responses-compatible, Gemini-compatible, Anthropic-compatible, and all catalog preset providers: need non-empty `base_url`
- `bedrock`: needs `metadata.region`, `AWS_REGION`, or `AWS_DEFAULT_REGION`
- `vertex-ai`: needs `metadata.project` (or `GOOGLE_CLOUD_PROJECT`) and `metadata.location`/`metadata.region` (or `GOOGLE_CLOUD_LOCATION`/`GOOGLE_CLOUD_REGION`)
- `ollama`: no API key environment validation

## Validation Rules

Current config validation enforces at least:
- `gateway.port` must be between `1` and `65535`
- `gateway.host` cannot be empty
- at least one agent must be configured
- agent `role` must be `admin`, `employee`, or `customer`
- agent `workspace` cannot be empty and must be creatable
- agent permission entries cannot be empty strings
- wildcard internal subagent authorization is admin-only
- `experience-curator` internal subagent authorization requires admin role
- external subagent names cannot be empty strings
- `llm.default_provider` and `llm.default_model` cannot be empty
- `llm.default_provider` must exist in `llm.providers`
- `llm.default_model` must be declared under the default provider's `models`
- every provider needs non-empty `type`, non-empty `default_model`, and at least one `models` entry
- every model entry needs non-empty `id` and `name`
- model `id` must be unique within a single provider (duplicate ids are rejected)
- model `type` must be `chat`, `embedding`, or `rerank`
- model `context_window` and `max_output_tokens` cannot be negative
- provider `default_model` must exist in provider `models`
- `thinking` and `reasoning_replay` currently have `deepseek`-specific restrictions
- every agent provider must exist in `llm.providers`
- every non-empty agent model must be declared under that agent's provider
- `memory.short_term.max_messages` must be at least `1`
- `memory.short_term.max_tokens` must be at least `100`
- `memory.short_term.keep_recent` cannot be negative
- `memory.medium_term.max_size` must be at least `100`
- if vector memory is enabled, embedder type is required
- if vector memory is enabled, `prefetch_limit` must be at least `1`
- if vector memory is enabled, `score_threshold` must be between `0` and `1`
- built-in scheduler cron expressions cannot be empty and must parse as standard cron
- channel `role` must be `admin`, `employee`, or `customer`
- non-empty channel `agent` must reference an existing configured agent
- `plugins.ws_server.port` must be between `0` and `65535`
- enabled plugin `type` must be `local` or `remote`
- enabled local plugins require `command`
- enabled remote plugins require `url`
- plugin `restart`, when set, must be `always`, `on-failure`, or `never`
- plugin restart and timeout numeric fields cannot be negative
- plugin capabilities must be `tool`, `provider`, `channel`, or `hook`
- `tools.default_timeout` cannot be negative
- `mcp.servers.<name>.timeout` and `mcp.servers.<name>.call_timeout` cannot be negative

`session_archive` has no structural validation beyond field types, but its effective enabled state follows the default-on rule described under `session_archive` in Config Field Reference.

## Reload Matrix

Use `system_reload` when available and the current role is admin.

Reload scopes:
- `skills`: reloads `SKILL.md` files into each active agent's in-memory skill loader
- `config`: reloads config from disk and applies the runtime-safe subset through the gateway reload path
- `all`: reloads config, restores system scaffolds, and reloads skills

Usually effective after `system_reload scope="skills"`:
- creating a new shared or agent-local `SKILL.md`
- editing an existing skill body, description, license, compatibility, metadata, tags, or role restriction

Usually effective after `system_reload scope="config"` or `scope="all"`:
- `logging` settings
- `default_agent` in gateway/router paths that read the refreshed config
- `memory` settings used by newly built prompts/runtime flows
- `scheduler.system_tasks` registration
- some `channels` rebuild behavior handled by the gateway reload path
- some general runtime behavior fields such as tool-result/image thresholds when the current runtime path consumes them dynamically

Usually requires service restart or agent re-registration to fully apply:
- `gateway.host`, `gateway.port`, gateway token listener/auth behavior
- `daemon.control_port`
- `data_dir` and `shared.skills_dir` path changes for already initialized managers
- agent definitions, roles, workspaces, permissions, provider/model selection, and LLM generation options already bound to active agents
- LLM provider definitions and default provider/model used by already registered agents
- proxy changes that affect process startup or integration surfaces
- tool registration changes controlled by `tools` (including `tools.default_timeout`, `tools.session`, and `tools.backend_call`)
- backend endpoint additions/removals that affect whether a tool is registered
- MCP server additions, removals, command changes, headers, `timeout`/`call_timeout`, or enabled state
- plugin websocket server and plugin process definitions
- `fusion` and `image_describe` tool registration (provider/model/panel/enabled changes)

If a change touches active providers, tools, MCP, plugins, proxy behavior, agent workspaces, daemon/listener ports, or process listening addresses, tell the user a restart is the safe path even if `system_reload config` validates the file.

## Skills

Skill locations:
- agent-local: `<agent-workspace>/skills/<skill-name>/SKILL.md`
- shared: `<shared.skills_dir>/<skill-name>/SKILL.md`

Skill file format:

```markdown
---
name: skill-name
description: When to use this skill
license: MIT
compatibility: evoduck
metadata:
  evoduck:
    role: admin
    tags: [meta, configuration]
---

# Skill Title

Instructions for the agent.
```

Rules:
- use `skill_list` before creating a new skill
- use `skill_detail` on likely matches before editing or creating
- prefer updating an existing close skill over creating overlapping skills
- `SKILL.md` is the runtime entrypoint; support files are optional
- use `metadata.evoduck.role` for role restriction and `metadata.evoduck.tags` for tags
- avoid deprecated frontmatter such as top-level `tags`, `parameters`, legacy `requires.role`, or template-style placeholders
- after writing or editing `SKILL.md`, call `system_reload` with `scope="skills"` and verify with `skill_detail`
- built-in skills are copied into the shared skill directory only when missing; user edits are preserved

Useful CLI commands:
- `evoduck skills list`
- `evoduck skills detail <skill-name>`
- `evoduck skills verify <skill-name>`
- `evoduck skills install <path-or-zip>`
- `evoduck skills pack <path>`

## Memory

Use memory tools for user-specific memory and agent bootstrap files. The tools route automatically by `path`; do not pass or invent a `scope` parameter.

Agent bootstrap paths:
- `AGENTS.md`: agent operating rules
- `SOUL.md`: agent identity, mission, tone, and durable boundaries
- `TOOLS.md`, `IDENTITY.md`, `HEARTBEAT.md`, `BOOTSTRAP.md`: additional agent bootstrap files when those roles are established

Default scaffold creation currently ensures:
- `AGENTS.md`
- `SOUL.md`
- user-isolated `users/USER.md` when user scaffolds are created

Writable user memory paths:
- `USER.md`: user profile and preferences
- `MEMORY.md`: user-specific long-term memory
- `memory/YYYY-MM-DD.md`: user-specific daily medium-term memory

Memory write rules:
- use `memory_read` or `memory_search` before editing when possible
- use `memory_write` to create or overwrite allowed agent bootstrap or user memory files
- use `memory_edit` for append, prepend, or exact text replacement
- `USER.md`, `MEMORY.md`, and `memory/YYYY-MM-DD.md` route to the user memory directory
- bootstrap files route to the agent workspace
- admin and system curator contexts can target `user_id`; ordinary contexts should use the current user context
- do not store secrets, unconfirmed guesses, transient tool traces, or one-off task details as durable memory

Use shared knowledge instead of user memory for reusable project, architecture, runbook, or domain facts that should apply across users.

## Knowledge

Use knowledge tools for shared reusable information under the shared knowledge base.

Workflow:
- use `knowledge_tree` to discover entries
- use `knowledge_search` for likely related content
- use `knowledge_read` before editing an existing entry
- use `knowledge_write`, `knowledge_edit`, or `knowledge_delete` only when available and appropriate for the role
- prefer updating an existing entry over creating duplicates

Do not put user-specific profile details into shared knowledge.

## Tools And Capability Boundaries

Available tools depend on role, config, permissions, plugins, MCP, channel context, and session context. Always trust the current prompt's tool list over assumptions.

Common boundaries:
- customer role has a smaller tool surface
- employee and admin roles can have file, memory, schedule, HTTP, exec, and knowledge write tools when authorized
- `system_reload` is admin-only
- `backend_call` exists only when backend endpoints are configured
- session tools depend on `tools.session` config, role policy, explicit agent tool allowlists, and user context
- MCP tools appear only after enabled MCP servers initialize successfully
- plugin tools appear only after plugins connect and register capabilities
- internal subagent access depends on `authorized_subagents`
- external subagent access depends on `authorized_external_subagents`
- `fusion` (roundtable consultation) is admin-only and registered only when `fusion.enabled` is true and `panel` is non-empty
- `image_describe` (vision-delegated image description) is available to all roles when `image_describe.enabled` is true and both `provider` and `model` are set

File tools are constrained by authorized directories. If a write fails due to path authorization, inspect agent permissions and use an allowed path instead of trying to escape the sandbox.

## MCP

MCP local config shape:

```yaml
mcp:
  servers:
    server-name:
      type: local
      enabled: true
      command: ["npx", "-y", "some-mcp-server"]
      environment: {}
      timeout: 30000
```

MCP remote config shape:

```yaml
mcp:
  servers:
    server-name:
      type: remote
      enabled: true
      url: https://mcp.example.com
      headers:
        Authorization: Bearer ${MCP_TOKEN}
      timeout: 30000
```

MCP server process and connection changes normally require service restart to fully apply to active agents.

## Plugins And Channels

Plugins can provide tools, providers, channels, or hooks depending on declared capabilities. Plugin process definitions, websocket server settings, and plugin startup behavior should be treated as restart-required unless the user confirms a supported hot-reload path.

Channels bind external conversations to agents.
- `webchat` is the built-in gateway web layer and not a regular external bridge
- `weixin` uses QR-login-derived credentials and optional account metadata
- `wecom` uses AI Bot credentials over WebSocket
- plugin-defined channels follow plugin capability registration and should be treated as restart-sensitive unless proven otherwise

## Safe Configuration Workflow

When asked to change EvoDuck configuration:

1. Inspect the relevant current config or runtime files.
2. Identify whether the change is runtime-reloadable or restart-required.
3. Make the smallest config or file edit.
4. Avoid exposing or hardcoding secrets; use environment variables where possible.
5. Preserve unrelated existing user configuration fields.
6. Run validation when a validation path or config reload is available.
7. If reloadable, call the correct `system_reload` scope.
8. Verify with the relevant list, detail, or read path.
9. Tell the user what changed and whether restart is still needed.

If unsure whether a change is safe to hot-reload, classify it as restart-required.

## Self-Inspection Checklist

Before answering questions about what you are or what you can do:
- check loaded tools in the prompt or with available tool discovery
- check `skill_list` for relevant built-in or shared skills
- read `AGENTS.md` and `SOUL.md` if identity or behavior is unclear
- read config only when needed and avoid printing sensitive fields
- distinguish actual current capabilities from capabilities EvoDuck can support after configuration

Never claim a configuration, memory, skill, plugin, MCP server, proxy path, or channel is active until you have verified it from current runtime state or files.
