# Plugin Development

[English](../guides/plugin-development.md) | [简体中文](../zh-CN/guides/plugin-development.md)

This guide covers EvoDuck's WebSocket plugin system. It is not MCP.

If you only need to add external tools for agents, prefer MCP first. Use plugins when you need provider, channel, or hook-level extensions.

## 1. Supported Capabilities

The plugin system currently supports four capability types:

- `tool`
- `provider`
- `channel`
- `hook`

## 2. Capability Roles

- `tool`: exposes external functionality as agent-callable tools
- `provider`: adds model provider integrations
- `channel`: adds message bridges
- `hook`: observes or influences runtime events

## 3. Config Entry

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

## 4. Confirmed Config Fields

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

Validation checks:

- `type` must be `local` or `remote`
- `local` requires `command`
- `remote` requires `url`
- `capabilities.allow` may only contain `tool`, `provider`, `channel`, or `hook`

## 5. Local Plugin Runtime

Local plugins are usually started by EvoDuck. You do not need to start them manually.

EvoDuck injects environment variables such as:

- `EVODUCK_PLUGIN_ID`
- `EVODUCK_PLUGIN_TOKEN`
- `EVODUCK_WS_URL`

The plugin then connects over WebSocket and registers its capabilities.

## 6. Runtime Usage

Current behavior:

- `tool` capabilities are wrapped as tool adapters and registered into the agent tool table.
- `provider` capabilities are registered into the LLM registry.
- `channel` capabilities create channel bridges.
- `hook` capabilities participate in runtime hook dispatch.

## 7. Demo Plugins

Repository demos:

- `plugins/echo-tool`
- `plugins/mock-provider`
- `plugins/mock-channel`
- `plugins/mock-hook`

Recommended reading order:

1. `plugins/echo-tool/README.md`
2. `plugins/mock-provider/README.md`
3. `plugins/mock-channel/README.md`
4. `plugins/mock-hook/README.md`

## 8. Minimal Tool Plugin Flow

A tool plugin should:

1. Read `EVODUCK_PLUGIN_ID`, `EVODUCK_PLUGIN_TOKEN`, and `EVODUCK_WS_URL`.
2. Connect to the plugin WebSocket server.
3. Send a `register` request with a `tool` capability.
4. Handle `tool.execute` requests.
5. Return a response frame with the tool result.

## 9. Hooks

Hooks can be observer hooks or mutating hooks.

Examples:

- `after_tool_call`
- `after_llm_complete`
- `before_tool_call`
- `before_llm_call`
- `before_agent_start`
- `before_message_send`
- `after_message_receive`
- `on_conversation_binding`

Mutating hooks can block or patch selected runtime behavior.

## 10. Operational Notes

- Keep plugin request timeouts short and explicit.
- Use `restart: never` for development fixtures.
- Use capability allowlists to limit plugin scope.
- Prefer synthetic test data in demo plugins.
