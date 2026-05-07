# 配置检查清单

这是一份按当前代码行为整理的核对清单，适合在首次引导之后或手工改配置之后使用。

## 1. 先确认你走的是哪条路径

当前推荐顺序不是先手写一大份配置，而是：

1. 运行 `evoduck run` 或 `evoduck setup`
2. 让 Setup Wizard 先生成一份能工作的配置
3. 再回过头逐项检查和扩展

## 2. 最小可运行条件

至少需要：

1. `evoduck` 可执行文件已经构建好
2. 默认 agent 可用
3. `channels.webchat` 存在
4. 你选择的默认 provider 已经完成它所需的最小配置

注意：

- 不是所有 provider 都需要 API Key
- 例如 `ollama` 本地方案就可能不需要
- 是否需要 API Key，取决于你在首次向导里选择的 provider 类型

## 3. 配置文件位置

- Windows: `%USERPROFILE%\\.evoduck\\config\\config.yaml`
- Linux/macOS: `~/.evoduck/config/config.yaml`

如果你用了 `--config`，就以你指定的路径为准。

## 4. `llm` 检查

确认：

- `llm.default_provider` 在 `llm.providers` 中存在
- `llm.default_model` 有值
- 对应 provider 的关键参数已经配好

示例：

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

如果你选的是 `ollama`，则重点检查本地地址和模型名，而不是 API Key。

## 5. `agents` 检查

每个 agent 至少检查：

- `role`
- `workspace`
- `provider`
- `model`

示例：

```yaml
agents:
  admin-bot:
    role: admin
    workspace: ./agents/admin-bot
    provider: openai-compatible
    model: gpt-4o
```

## 6. Agent 工作区检查

每个 agent 工作区至少应有：

```text
agents/<agent-id>/
├── AGENTS.md
└── SOUL.md
```

程序通常会自动 scaffold，但如果你改过 `workspace`，建议重新检查。

## 7. `channels` 检查

最小可用配置通常只要：

```yaml
channels:
  webchat:
    type: webchat
    role: admin
    agent: admin-bot
```

并确认：

- `role` 合法
- `agent` 指向已有 agent

不要再把渠道挂在 `agents.<id>` 下面。

## 8. `shared.skills_dir` 检查

```yaml
shared:
  skills_dir: ./shared/skills
```

如果不写，程序会使用运行根目录下的 `shared/skills`。

## 9. `mcp` 检查

如果暂时不用 MCP，保持：

```yaml
mcp:
  servers: {}
```

如果启用 MCP，至少检查：

- `type` 是 `local` 或 `remote`
- `enabled` 为 `true`
- `local` 有 `command`
- `remote` 有 `url`

## 10. `plugins` 检查

如果暂时不用 WS 插件，可以先不配置任何插件。

如果启用插件，至少检查：

- `type` 是 `local` 或 `remote`
- `local` 有 `command`
- `remote` 有 `url`
- `capabilities.allow` 只包含 `tool` / `provider` / `channel` / `hook`

## 11. `memory` 检查

如果只是先跑通，可以先不深调。

若开启向量记忆，至少检查：

- `memory.long_term.vector.enabled`
- `memory.long_term.vector.embedder.type`
- `memory.long_term.vector.embedder.model`
- 如不继承 LLM 配置，则补 `api_key` / `base_url`

## 12. 常见问题

### provider requires API key

先确认是不是你选的 provider 本身就需要 API Key。

如果需要，再检查：

- `api_key` 是否已配置
- 环境变量是否实际生效

### agent workspace missing files

通常表示：

- `workspace` 指到了非预期位置
- scaffold 没生成到该路径

### plugin connection not found

通常表示：

- 插件进程没成功启动
- 插件没有完成 `register`
- `ws_server` 地址或 token 不匹配
