# 插件开发

本文档只讨论 EvoDuck 自己的 WS 插件系统，不是 MCP。

如果你的目标只是给 agent 增加外部工具，优先考虑 MCP。只有在你需要 provider、channel 或 hook 级扩展时，再进入插件系统。

## 1. 当前支持的 capability

当前插件系统支持四类能力：

- `tool`
- `provider`
- `channel`
- `hook`

## 2. 它们分别做什么

- `tool`: 把外部功能注册成 agent 可调用工具
- `provider`: 接入新的模型 provider
- `channel`: 接入新的消息桥接
- `hook`: 观察或影响运行时事件

## 3. 配置入口

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

## 4. 当前已确认的配置字段

- `enabled`
- `type`
- `command`
- `environment`
- `url`
- `token`
- `restart`
- `restart_delay`
- `max_restarts`
- `override`
- `capabilities.allow`
- `connect_timeout`
- `request_timeout`

校验层还会检查：

- `type` 必须是 `local` 或 `remote`
- `local` 必须有 `command`
- `remote` 必须有 `url`
- `capabilities.allow` 只能是 `tool` / `provider` / `channel` / `hook`

## 5. 本地插件的运行方式

本地插件通常由 EvoDuck 主进程拉起，不需要你手动再启动一个单独命令窗口。

主程序会向本地插件注入环境变量，例如：

- `EVODUCK_PLUGIN_ID`
- `EVODUCK_PLUGIN_TOKEN`
- `EVODUCK_WS_URL`

插件随后通过 WebSocket 向 EvoDuck 注册自己的 capability。

## 6. 运行时里插件如何被使用

当前实现中：

- `tool` capability 会被包装成 tool adapter，并注册进 agent 工具表
- `provider` capability 会注册到 LLM registry
- `channel` capability 会生成 channel bridge
- `hook` capability 会参与运行时 hook 分发

## 7. 仓库里的 demo plugin

当前仓库中已有几类最小 demo：

- `plugins/echo-tool`
- `plugins/mock-provider`
- `plugins/mock-channel`
- `plugins/mock-hook`

建议按这个顺序阅读：

1. `plugins/echo-tool/README.md`
2. `plugins/mock-provider/README.md`
3. `plugins/mock-channel/README.md`
4. `plugins/mock-hook/README.md`

## 8. 最小 tool 插件示例

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
      request_timeout: 5000
```

## 9. 什么时候用插件，什么时候用 MCP

优先用 MCP：

- 只需要工具扩展

优先用插件：

- 需要新的 provider
- 需要新的 channel
- 需要 hook
- 需要持续双向运行时连接

## 10. 当前建议

第一次扩展 EvoDuck 时，建议顺序是：

1. 先写 Skill
2. 再接 MCP
3. 确认确实需要运行时扩展后，再做 WS 插件
