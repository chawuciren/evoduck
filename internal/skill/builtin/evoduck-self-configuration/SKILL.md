---
name: evoduck-self-configuration
description: Guide for configuring EvoDuck itself. Use this when the user asks the agent to inspect, explain, or modify EvoDuck configuration, skills, memory, knowledge, tools, MCP, plugins, channels, or reload behavior.
license: MIT
compatibility: evoduck
metadata:
  evoduck:
    role: admin
    tags: [meta, evoduck, configuration, memory, skills, reload]
---

# EvoDuck Self Configuration Guide

Use this guide when the user asks you to configure EvoDuck, explain what this agent can do, change runtime behavior, add skills, write memory, adjust tools, edit MCP, configure channels, or diagnose why a configuration change did not take effect.

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

## Configuration Files

Default installation layout:
- App root: `~/.evoduck`
- Config file: `~/.evoduck/config/config.yaml`
- Data directory: configured by `data_dir`, defaulting to the app root
- Agent workspaces: usually `<data_dir>/agents/<agent-id>`
- Shared skills: configured by `shared.skills_dir`, defaulting to `<app-root>/shared/skills`

The config file is YAML. Top-level sections include:
- `gateway`: host, port, access token
- `default_agent`: default agent id
- `data_dir`: runtime data root
- `logging`: level, json mode, color
- `llm`: default provider, default model, provider definitions, model options
- `agents`: agent workspace, role, provider/model overrides, permissions, user isolation
- `channels`: channel bindings such as webchat, weixin, wecom, or plugin-provided channels
- `shared`: shared skill directory
- `tools`: backend_call and session tool settings
- `memory`: short-term, medium-term, long-term, core memory, bootstrap limits
- `heartbeat`: periodic prompt settings
- `scheduler`: built-in system task schedules
- `mcp`: local or remote MCP server definitions
- `plugins`: plugin websocket server and plugin definitions

Sensitive fields include `token`, `api_key`, `secret`, passwords, and headers. Never expose secret values in responses. Prefer environment variables such as `${OPENAI_API_KEY}` in config.

## Complete Config Template

Use this as a field reference, not as a mandatory starting point. Omit sections you do not need only when defaults or empty maps are acceptable.

```yaml
gateway:
  host: 127.0.0.1
  port: 18789
  token: "" # Sensitive. Optional gateway auth token.

default_agent: admin-bot
data_dir: "~/.evoduck"

logging:
  level: INFO # DEBUG, INFO, WARN, ERROR
  json_mode: false
  color: true

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
        - gpt-4o
      tool_choice: auto # auto, none, required, or a specific tool/function name
      parallel_tool_calls: true
      response_format:
        type: text # text, json_object, or json_schema
      stop: []
      presence_penalty: 0
      frequency_penalty: 0
      max_completion_tokens: 0 # 0 means omit provider request field
      reasoning_effort: "" # provider-specific, e.g. low, medium, high
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

    anthropic:
      type: anthropic
      base_url: https://api.anthropic.com
      api_key: ${ANTHROPIC_API_KEY}
      default_model: claude-sonnet-4-5
      models:
        - claude-sonnet-4-5

    gemini:
      type: gemini
      api_key: ${GEMINI_API_KEY}
      default_model: gemini-2.5-flash
      models:
        - gemini-2.5-flash

    ollama:
      type: ollama
      base_url: http://localhost:11434/v1
      default_model: qwen2.5
      models:
        - qwen2.5

    bedrock:
      type: bedrock
      default_model: anthropic.claude-3-5-sonnet-20240620-v1:0
      models:
        - anthropic.claude-3-5-sonnet-20240620-v1:0
      metadata:
        region: us-east-1
        profile: default

    vertex-ai:
      type: vertex-ai
      default_model: gemini-2.5-flash
      models:
        - gemini-2.5-flash
      metadata:
        project: your-gcp-project
        location: us-central1

agents:
  admin-bot:
    role: admin # admin, employee, customer
    workspace: "~/.evoduck/agents/admin-bot"
    provider: openai-compatible
    model: gpt-4o
    permissions:
      authorized_directories: [] # Empty means role/workspace defaults.
      authorized_tools: [] # Empty means default tool set for role.
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
  backend_call:
    endpoints:
      example-api:
        url: https://api.example.com/items
        method: GET
        auth:
          type: bearer # Tool-specific string; commonly bearer, header, or none.
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
      employee: user # user or self policy; see session tool policy behavior
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
      schedule: "0 * * * *"
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

    remote-search:
      type: remote
      enabled: false
      url: https://mcp.example.com
      headers:
        Authorization: Bearer ${MCP_TOKEN}
      timeout: 30000
```

## Config Field Reference

### `gateway`

- `gateway.host`: string. Listener host. Default is `127.0.0.1`. Must not be empty. Restart required.
- `gateway.port`: integer. Listener port. Default is `18789`. Must be `1..65535`. Restart required.
- `gateway.token`: string. Sensitive gateway auth token. Restart required for listener/auth behavior to be safely refreshed.

### `default_agent` And `data_dir`

- `default_agent`: string. Agent id used when a request does not explicitly select an agent. Config reload can update gateway/router default behavior where wired, but verify in runtime.
- `data_dir`: string path. Runtime data root for agents, users, sessions, scheduler, logs, and knowledge. Changing this after startup should be treated as restart-required.

### `logging`

- `logging.level`: enum `DEBUG`, `INFO`, `WARN`, `ERROR`. Runtime reloadable.
- `logging.json_mode`: bool. Runtime reloadable.
- `logging.color`: bool. Runtime reloadable.
- Environment override: `LOG_LEVEL`, `LOG_JSON_MODE`, and `LOG_COLOR` can override or influence runtime logging behavior.

### `llm`

- `llm.default_provider`: string key under `llm.providers`. Must exist. Active agents usually require restart/re-registration to switch.
- `llm.default_model`: string. Must be listed under the default provider's `models`. Active agents usually require restart/re-registration to switch.
- `llm.providers`: map of provider names to provider configs. Static provider changes usually require restart because the LLM registry is built at startup.

Provider fields:
- `type`: provider type. Required.
- `base_url`: string. Required for OpenAI-compatible preset types and many compatible providers.
- `api_key`: string. Sensitive. May be a literal or `${ENV_VAR}`.
- `headers`: map string to string. Sensitive as a group in UI masking. Useful for provider-specific auth or routing headers.
- `default_model`: string. Required and must be present in `models`.
- `models`: list of strings. Required non-empty.
- `tool_choice`: string. `auto`, `none`, `required`, or a specific tool/function name.
- `parallel_tool_calls`: nullable bool. When set, forwards parallel tool call preference to compatible providers.
- `response_format`: map. Common `type` values are `text`, `json_object`, and `json_schema`.
- `stop`: list of stop strings.
- `presence_penalty`: nullable number. Provider-specific numeric range.
- `frequency_penalty`: nullable number. Provider-specific numeric range.
- `max_completion_tokens`: integer. If `0`, omitted from provider request.
- `reasoning_effort`: string. Provider-specific, commonly `low`, `medium`, or `high`.
- `verbosity`: string. Provider-specific.
- `user`: string. Forwarded as provider user identifier where supported.
- `safety_identifier`: string. Forwarded to compatible providers where supported.
- `service_tier`: string. Provider-specific service tier.
- `n`: integer. If `0`, omitted.
- `seed`: nullable integer.
- `logprobs`: bool.
- `top_logprobs`: integer. If greater than `0`, also enables `logprobs` for compatible requests.
- `store`: nullable bool.
- `include_usage`: nullable bool. Available in config; provider support depends on implementation.
- `metadata`: map string to string.
- `chat_template_kwargs`: map string to any. Forwarded to compatible providers where supported.

### `agents`

- `agents.<id>.role`: enum `admin`, `employee`, `customer`. Required. Controls tool access and skill role checks.
- `agents.<id>.workspace`: string path. Required and must be creatable. Agent prompt scaffold files live here.
- `agents.<id>.permissions.authorized_directories`: list of paths. Empty means default role/workspace permissions. Empty string entries are invalid.
- `agents.<id>.permissions.authorized_tools`: list of tool names. Empty means default tool set for the role. If non-empty, tool registration is filtered through this allowlist. Empty string entries are invalid.
- `agents.<id>.provider`: provider name. Must exist in `llm.providers` after defaults are applied.
- `agents.<id>.model`: model name. If non-empty, must be declared under the selected provider's `models`.
- `agents.<id>.user_isolation.auto_create`: bool. Defaults to true when omitted/false at runtime defaulting.
- `agents.<id>.user_isolation.auto_profile`: bool. Defaults to true when omitted/false at runtime defaulting.
- `agents.<id>.temperature`: nullable number. Agent-level LLM option.
- `agents.<id>.max_tokens`: integer. Agent-level LLM option. `0` means omit/default.
- `agents.<id>.top_p`: nullable number. Agent-level LLM option.
- `agents.<id>.max_iterations`: integer. Defaults to `100` if `0` or negative.

Agent registration changes, roles, permissions, workspaces, provider/model choices, and LLM options should be treated as restart-required for active agents.

### `channels`

- `channels.<id>.type`: string. Built-ins include `webchat`, `weixin`, and `wecom`; plugins can define more. `webchat` is reserved for the gateway web layer.
- `channels.<id>.name`: string display name.
- `channels.<id>.role`: enum `admin`, `employee`, `customer`. Required by validation semantics.
- `channels.<id>.agent`: string agent id. If non-empty, must reference an existing configured agent.
- `channels.<id>.token`: string. Sensitive. Used by Weixin and possibly plugin channels.
- `channels.<id>.user_id`: string. Weixin account user id.
- `channels.<id>.api_base_url`: string. Weixin API base URL. Empty uses the built-in default.
- `channels.<id>.bot_id`: string. WeCom AI Bot id.
- `channels.<id>.secret`: string. Sensitive. WeCom AI Bot secret.

Channel config may be rebuilt by config reload, but connection-level changes can require reconnect or service restart. Treat new channel types from plugins as restart-required unless a supported hot path is known.

### `shared`

- `shared.skills_dir`: string path. Shared skills are loaded from this directory. Changing this after manager initialization should be treated as restart-required. New or edited skill files inside the current directory can be loaded with `system_reload scope="skills"`.

### `tools.backend_call`

- `tools.backend_call.endpoints`: map of endpoint names. If empty, `backend_call` is not registered.
- `url`: endpoint URL.
- `method`: HTTP method string.
- `auth.type`: auth mode string interpreted by the backend_call tool.
- `auth.token`: sensitive auth token.
- `auth.header`: auth header name.
- `allowed_roles`: list of roles allowed to call the endpoint.
- `rate_limit`: integer request limit used by the tool implementation.
- `timeout`: Go duration, e.g. `30s`, `1m`.

Backend endpoint additions or removals affect tool registration and should be treated as restart-required for active agents.

### `tools.session`

- `enabled`: bool. Enables session tools policy.
- `visibility.employee`: string policy for employee role, commonly `user`.
- `visibility.customer`: string policy for customer role, commonly `self`.
- `allow.employee`: list of session tool names allowed for employee role.
- `allow.customer`: list of session tool names allowed for customer role.

Session tool availability is determined by role, policy, explicit agent tool allowlists, and current user context. Tool registration changes should be treated as restart-required for active agents.

### `memory.short_term`

- `max_messages`: integer. Default `200`. Must be at least `1`.
- `max_tokens`: integer. Default `128000`. Must be at least `100`.
- `keep_recent`: integer. Default `10`. Must not be negative.
- `session_ttl`: duration. Default `168h`.
- `cleanup_interval`: duration. Default `1h`.
- `flush_before_compact`: bool. If true, attempts memory flush before compaction.

### `memory.medium_term`

- `dir`: string. Default `memory`.
- `max_size`: integer. Default `5000`. Must be at least `100`.
- `load_days`: integer. Default `7`.
- `min_messages_to_extract`: integer. Default `5`.
- `compression_threshold`: integer. Default `10000`.

### `memory.long_term`

- `vector.enabled`: bool. Enables vector memory search/index behavior.
- `vector.embedder.type`: string. Required when vector is enabled.
- `vector.embedder.model`: string.
- `vector.embedder.dimensions`: integer.
- `vector.embedder.api_key`: sensitive string. If omitted, embedder may inherit provider settings depending on implementation.
- `vector.embedder.base_url`: string.
- `vector.prefetch_limit`: integer. Default `5`. Must be at least `1` when vector is enabled.
- `vector.score_threshold`: number. Default `0.7`. Must be between `0` and `1` when vector is enabled.
- `dedup_threshold`: number. Default `0.95`.
- `cleanup_policy.check_interval`: duration. Default `24h`.
- `cleanup_policy.min_age_days`: integer. Default `30`.
- `cleanup_policy.batch_size`: integer. Default `30`.
- `cleanup_policy.reference.medium_memory_days`: integer. Default `7`.
- `cleanup_policy.reference.include_core_memory`: bool.
- `cleanup_policy.reference.include_access_stats`: bool.
- `compression_threshold`: integer. Default `15000`.

### `memory.core_memory` And `memory.bootstrap`

- `core_memory.file`: string. Default `MEMORY.md`.
- `core_memory.auto_consolidate`: bool.
- `core_memory.importance_threshold`: number. Default `0.9`.
- `bootstrap.max_file_chars`: integer. Default `20000`.
- `bootstrap.max_total_chars`: integer. Default `150000`.
- `bootstrap.warning_threshold`: number. Default `0.8`.
- `bootstrap.truncation_strategy`: string. Default `head`. Code comments mention `head` and `tail`.

### `heartbeat` And `scheduler`

- `heartbeat.enabled`: bool.
- `heartbeat.interval`: duration. Default `30m`.
- `heartbeat.prompt`: string.
- `scheduler.system_tasks.memory_curation.schedule`: cron expression. Default `0 * * * *`. Must parse as standard cron.
- `scheduler.system_tasks.experience_curation.schedule`: cron expression. Default `0 3 * * *`. Must parse as standard cron.

### `plugins`

- `plugins.ws_server.host`: string. Default `127.0.0.1`.
- `plugins.ws_server.port`: integer. Default `19000`. Must be `0..65535`.
- `plugins.plugins.<name>.enabled`: bool. Disabled plugins skip most validation.
- `plugins.plugins.<name>.type`: enum `local`, `remote` when enabled.
- `plugins.plugins.<name>.command`: list of strings. Required for enabled local plugins.
- `plugins.plugins.<name>.environment`: map string to string.
- `plugins.plugins.<name>.url`: string. Required for enabled remote plugins.
- `plugins.plugins.<name>.token`: sensitive string.
- `plugins.plugins.<name>.restart`: enum `always`, `on-failure`, `never`, or empty.
- `plugins.plugins.<name>.restart_delay`: integer. Must not be negative.
- `plugins.plugins.<name>.max_restarts`: integer. Must not be negative.
- `plugins.plugins.<name>.override`: bool.
- `plugins.plugins.<name>.capabilities.allow`: list containing only `tool`, `provider`, `channel`, or `hook`.
- `plugins.plugins.<name>.connect_timeout`: integer. Must not be negative.
- `plugins.plugins.<name>.request_timeout`: integer. Must not be negative.

Plugin process definitions, websocket server settings, and plugin capabilities should be treated as restart-required for active services.

### `mcp`

- `mcp.servers.<name>.type`: enum `local` or `remote`.
- `mcp.servers.<name>.enabled`: bool.
- `mcp.servers.<name>.command`: list of strings. Used by local MCP servers.
- `mcp.servers.<name>.environment`: map string to string. `MEMORY_FILE_PATH` is normalized as a path if present.
- `mcp.servers.<name>.url`: string. Used by remote MCP servers.
- `mcp.servers.<name>.headers`: map string to string. Sensitive as a group.
- `mcp.servers.<name>.timeout`: integer milliseconds.

MCP server process and connection changes normally require service restart to fully apply to active agents.

## Provider Type Reference

Direct registry provider types:
- `openai`
- `openai-compatible`
- `openai-responses-compatible`
- `anthropic`
- `anthropic-compatible`
- `gemini`
- `gemini-compatible`
- `ollama`
- `bedrock`
- `vertex-ai`
- `azure`

Preset provider types that are normalized to OpenAI-compatible behavior when initialized:
- `deepseek`
- `minimax`
- `minimax-cn`
- `openrouter`
- `dashscope`
- `dashscope-cn`
- `dashscope-coding`
- `dashscope-coding-cn`
- `xai`
- `groq`
- `mistral`
- `together`
- `fireworks`
- `perplexity`
- `moonshot`
- `nvidia`
- `cloudflare-ai-gateway`
- `vercel-ai-gateway`
- `helicone`
- `portkey`
- `cohere`
- `novita`
- `google-ai-studio`
- `siliconflow`
- `zhipu`
- `zhipu-cn`
- `zhipu-coding`
- `zhipu-coding-cn`
- `baidu-qianfan`
- `tencent-hunyuan`
- `bytedance`
- `bytedance-cn`
- `iflytek-spark`
- `cerebras`
- `replicate`
- `sambanova`
- `akle`
- `kilo`
- `opencode`
- `lmstudio`
- `vllm`
- `litellm`

Provider environment validation rules:
- `openai`: needs `api_key` or `OPENAI_API_KEY`.
- `anthropic`: needs `api_key` or `ANTHROPIC_API_KEY`.
- `gemini`: needs `api_key`, `GEMINI_API_KEY`, or `GOOGLE_API_KEY`.
- OpenAI-compatible and preset providers: need non-empty `base_url`.
- `bedrock`: needs `metadata.region`, `AWS_REGION`, or `AWS_DEFAULT_REGION`.
- `vertex-ai`: needs `metadata.project` or `GOOGLE_CLOUD_PROJECT`, and needs `metadata.location`, `metadata.region`, `GOOGLE_CLOUD_LOCATION`, or `GOOGLE_CLOUD_REGION`.
- `ollama`: no API key environment validation.

## Validation Rules

Current config validation enforces:
- `gateway.port` must be between `1` and `65535`.
- `gateway.host` cannot be empty.
- At least one agent must be configured.
- Agent `role` must be `admin`, `employee`, or `customer`.
- Agent `workspace` cannot be empty and must be creatable.
- Agent permission entries cannot be empty strings.
- `llm.default_provider` and `llm.default_model` cannot be empty.
- `llm.default_provider` must exist in `llm.providers`.
- `llm.default_model` must be declared under the default provider's `models`.
- Every provider needs non-empty `type`, non-empty `default_model`, and at least one `models` entry.
- Provider `default_model` must exist in provider `models`.
- Every agent provider must exist in `llm.providers`.
- Every non-empty agent model must be declared under that agent's provider.
- `memory.short_term.max_messages` must be at least `1`.
- `memory.short_term.max_tokens` must be at least `100`.
- `memory.short_term.keep_recent` cannot be negative.
- `memory.medium_term.max_size` must be at least `100`.
- If vector memory is enabled, `memory.long_term.vector.embedder.type` is required.
- If vector memory is enabled, `memory.long_term.vector.prefetch_limit` must be at least `1`.
- If vector memory is enabled, `memory.long_term.vector.score_threshold` must be between `0` and `1`.
- Built-in scheduler cron expressions cannot be empty and must parse as standard cron.
- Channel `role` must be `admin`, `employee`, or `customer`.
- Non-empty channel `agent` must reference an existing configured agent.
- `plugins.ws_server.port` must be between `0` and `65535`.
- Enabled plugin `type` must be `local` or `remote`.
- Enabled local plugins require `command`.
- Enabled remote plugins require `url`.
- Plugin `restart`, when set, must be `always`, `on-failure`, or `never`.
- Plugin restart and timeout numeric fields cannot be negative.
- Plugin capabilities must be `tool`, `provider`, `channel`, or `hook`.

## Reload Matrix

Use `system_reload` when available and the current role is admin.

Reload scopes:
- `skills`: reloads `SKILL.md` files into each active agent's in-memory skill loader.
- `config`: reloads config from disk and applies the runtime-safe subset through the gateway reload path.
- `all`: reloads config, restores system scaffolds, and reloads skills.

Usually effective after `system_reload scope="skills"`:
- Creating a new shared or agent-local `SKILL.md`.
- Editing an existing skill body, description, license, compatibility, metadata, tags, or role restriction.

Usually effective after `system_reload scope="config"` or `scope="all"`:
- `logging` settings.
- `default_agent` in gateway/router paths that read the refreshed config.
- `memory` settings used by newly built prompts/runtime flows.
- `scheduler.system_tasks` registration.
- Some `channels` rebuild behavior handled by the gateway reload path.

Usually requires service restart or agent re-registration to fully apply:
- `gateway.host`, `gateway.port`, and gateway token listener/auth behavior.
- Agent definitions, roles, workspaces, permissions, provider/model selection, and LLM generation options already bound to active agents.
- LLM provider definitions and default provider/model used by already registered agents.
- Tool registration changes controlled by `tools`.
- `backend_call` endpoint additions/removals that affect whether the tool is registered.
- MCP server additions, removals, command changes, headers, or enabled state.
- Plugin websocket server and plugin process definitions.
- `data_dir` and `shared.skills_dir` path changes for already initialized managers.

If a change touches active providers, tools, MCP, plugins, agent workspaces, or process listening addresses, tell the user a restart is the safe path even if `system_reload config` validates the file.

## Skills

Skill locations:
- Agent-local: `<agent-workspace>/skills/<skill-name>/SKILL.md`
- Shared: `<shared.skills_dir>/<skill-name>/SKILL.md`

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
- Use `skill_list` before creating a new skill.
- Use `skill_detail` on likely matches before editing or creating.
- Prefer updating an existing close skill over creating overlapping skills.
- Do not use `parameters` or template-style double-brace placeholders; skills are plain Markdown instruction packages.
- Use `metadata.evoduck.role` for role restriction and `metadata.evoduck.tags` for tags.
- Use supporting files such as `examples/` or `templates/` for long reference material.
- After writing or editing `SKILL.md`, call `system_reload` with `scope="skills"` and verify with `skill_detail`.
- Built-in skills are copied into the shared skill directory only when missing; user edits are preserved.

## Memory

Use memory tools for user-specific memory and agent bootstrap files. The tools route automatically by `path`; do not pass or invent a `scope` parameter.

Agent bootstrap paths:
- `AGENTS.md`: agent operating rules
- `SOUL.md`: agent identity, mission, tone, and durable boundaries
- `TOOLS.md`, `IDENTITY.md`, `HEARTBEAT.md`, `BOOTSTRAP.md`: additional agent bootstrap files

Use user memory for user-specific durable context, not global project knowledge.

Writable user memory paths:
- `USER.md`: user profile and preferences
- `MEMORY.md`: user-specific long-term memory
- `memory/YYYY-MM-DD.md`: user-specific daily medium-term memory

Memory write rules:
- Use `memory_read` or `memory_search` before editing when possible.
- Use `memory_write` to create or overwrite allowed agent bootstrap or user memory files.
- Use `memory_edit` for append, prepend, or exact text replacement.
- `USER.md`, `MEMORY.md`, and `memory/YYYY-MM-DD.md` route to the user memory directory.
- `AGENTS.md`, `SOUL.md`, `TOOLS.md`, `IDENTITY.md`, `HEARTBEAT.md`, and `BOOTSTRAP.md` route to the agent workspace.
- Admin and system curator contexts can target `user_id`; ordinary contexts should use the current user context.
- Do not store secrets, unconfirmed guesses, transient tool traces, or one-off task details as durable memory.

Use shared knowledge instead of memory for reusable project, architecture, runbook, or domain facts that should apply across users.

## Knowledge

Use knowledge tools for shared reusable information under the shared knowledge base.

Workflow:
- Use `knowledge_tree` to discover entries.
- Use `knowledge_search` for likely related content.
- Use `knowledge_read` before editing an existing entry.
- Use `knowledge_write`, `knowledge_edit`, or `knowledge_delete` only when available and appropriate for the role.
- Prefer updating an existing entry over creating duplicates.

Do not put user-specific profile details into shared knowledge.

## Tools And Capability Boundaries

Available tools depend on role, config, permissions, plugins, MCP, and channel/session context. Always trust the current prompt's tool list over assumptions.

Common boundaries:
- Customer role has a smaller tool surface.
- Employee and admin roles can have file, memory, schedule, HTTP, exec, and knowledge write tools when authorized.
- `system_reload` is admin-only.
- `backend_call` exists only when backend endpoints are configured.
- Session tools depend on `tools.session` config, role policy, and user context.
- MCP tools appear only after enabled MCP servers initialize successfully.
- Plugin tools appear only after plugins connect and register capabilities.

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

Channels bind external conversations to agents. Webchat is gateway-reserved. Other channel types can be built-in or plugin-defined. Channel config may be re-read by config reload, but connection-level behavior can still require restart or reconnect.

## Safe Configuration Workflow

When asked to change EvoDuck configuration:

1. Inspect the relevant current config or runtime files.
2. Identify whether the change is runtime-reloadable or restart-required.
3. Make the smallest config or file edit.
4. Avoid exposing or hardcoding secrets; use environment variables where possible.
5. Preserve unrelated existing user configuration fields.
6. Run validation when a validation path or config reload is available.
7. If reloadable, call the correct `system_reload` scope.
8. Verify with the relevant list/detail/read tool.
9. Tell the user what changed and whether restart is still needed.

If unsure whether a change is safe to hot-reload, classify it as restart-required.

## Self-Inspection Checklist

Before answering questions about what you are or what you can do:
- Check loaded tools in the prompt or with available tool discovery.
- Check `skill_list` for relevant built-in or shared skills.
- Read `AGENTS.md` and `SOUL.md` if identity or behavior is unclear.
- Read config only when needed and avoid printing sensitive fields.
- Distinguish actual current capabilities from capabilities EvoDuck can support after configuration.

Never claim a configuration, memory, skill, plugin, MCP server, or channel is active until you have verified it from current runtime state or files.
