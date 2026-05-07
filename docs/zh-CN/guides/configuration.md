# 配置结构

本文档只讲当前配置文件结构和字段职责，不替代安装与首次引导。

## 1. 配置文件位置

默认配置文件：

- Windows: `%USERPROFILE%\\.evoduck\\config\\config.yaml`
- Linux/macOS: `~/.evoduck/config/config.yaml`

也可以通过 `--config` 使用任意路径。

## 2. 顶层结构

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

用途：

- 网关监听地址
- 网关端口
- 网关鉴权 token

## 4. `default_agent`

```yaml
default_agent: admin-bot
```

如果请求没有显式指定 agent，系统会优先使用这里。

## 5. `data_dir`

```yaml
data_dir: E:/path/to/runtime-root
```

它控制运行根目录位置，包括 agent 默认工作区、日志、session、users、scheduler 状态和共享存储。

未显式配置时，EvoDuck 会使用默认的按用户隔离运行根目录：

- Windows: `%USERPROFILE%\\.evoduck`
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

`models` 现在是显式结构：请求模型名、显示名、模型类型、能力位、上下文窗口和最大输出 token 都由本地配置决定。

这里定义：

- 默认 provider
- 默认 model
- provider 列表及其参数

DeepSeek V4 的 OpenAI-compatible 扩展必须使用显式 provider type：

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

`thinking`、`reasoning_replay`、`user_id` 等 DeepSeek 专项字段只在 `type: deepseek` 时启用。普通 `type: openai-compatible` 即使 `base_url` 或模型名指向 DeepSeek，也只按通用 OpenAI-compatible 处理。

## 7. `agents`

```yaml
agents:
  admin-bot:
    role: admin
    workspace: ./agents/admin-bot
    provider: openai-compatible
    model: gpt-4o
```

关键字段：

- `role`
- `workspace`
- `provider`
- `model`
- `permissions`
- `user_isolation`

首次运行时，EvoDuck 还会在运行根目录下为默认 agent 工作区生成 scaffold 文件。

## 8. `channels`

```yaml
channels:
  webchat:
    type: webchat
    role: admin
    agent: admin-bot

  wecom-sales:
    type: wecom
    role: employee
    agent: sales-bot
    bot_id: your-bot-id
    secret: ${WECOM_SECRET}
```

注意：

- 当前实现里 channel 是顶层配置
- 不是写在 `agents.<id>` 下面

## 9. `shared`

```yaml
shared:
  skills_dir: ./shared/skills
```

共享 Skill 从这里加载。首次运行时，EvoDuck 会创建 shared skills 目录，并写入内置 Skill；已有用户内容不会被覆盖。

## 10. `tools`

当前重点是：

- `tools.backend_call`
- `tools.session`

示例：

```yaml
tools:
  session:
    enabled: true
    visibility:
      employee: user
      customer: self
    allow:
      employee: [sessions_list, sessions_history, sessions_send]
      customer: []
```

## 11. `memory`

`memory` 包含：

- `extract`
- `cleanup`
- `short_term`
- `medium_term`
- `long_term`
- `core_memory`
- `bootstrap`

如果只是先跑通系统，可以先不深调这块。

短期 session 压缩主要由 `short_term` 控制：

```yaml
memory:
  short_term:
    max_messages: 200
    max_tokens: 128000
    keep_recent: 10
```

未配置时，默认超过 200 条消息或估算超过 128000 tokens 会触发 session 压缩。Web context 面板也使用这个短期压缩阈值作为上限显示，不再使用 provider/model 的最大 context window 估计值。

## 12. `scheduler`

```yaml
scheduler:
  system_tasks:
    memory_curation:
      schedule: "0 * * * *"
    experience_curation:
      schedule: "0 3 * * *"
```

## 13. `plugins`

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
      capabilities:
        allow: ["tool"]
```

## 14. `mcp`

```yaml
mcp:
  servers:
    one-search:
      type: local
      enabled: true
      command: ["npx", "-y", "one-search-mcp"]
      timeout: 30000
```

当前支持：

- `local`
- `remote`

## 15. `logging`

```yaml
logging:
  level: INFO
  json_mode: false
  color: true
```

## 16. 一条原则

第一次使用时，优先走 Setup Wizard。

安装和首次运行行为见 [安装与首次启动](install.md)，日常操作见 [首次引导与日常使用](usage.md)。

只有当你已经理解系统结构，或者准备做渠道、MCP、插件等扩展时，再手工编辑这些字段。
