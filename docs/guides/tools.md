# Tools

[English](../guides/tools.md) | [简体中文](../zh-CN/guides/tools.md)

This guide summarizes tool sources and operational boundaries.

## 1. Tool Sources

EvoDuck tools can come from:

- built-in tools
- MCP servers
- WebSocket plugins
- backend-call endpoints

## 2. Built-In Tools

Built-in tools cover local runtime operations such as file access, memory, knowledge, sessions, schedule management, time, and web-related helpers.

Exact availability depends on config and runtime permissions.

## 3. Backend Call

Backend-call endpoints are configured under:

```yaml
tools:
  backend_call:
    endpoints: {}
```

Use explicit endpoint allowlists. Do not expose broad internal networks by default.

## 4. MCP Tools

MCP tools are provided by configured MCP servers.

Use MCP for external tools when an MCP server already exists or when you want protocol-level isolation.

## 5. Plugin Tools

Plugin tools are registered by WebSocket plugins with `tool` capability.

Demo reference:

- `plugins/echo-tool`

## 6. Safety Boundaries

- Keep tool permissions narrow.
- Avoid committing secrets into configs.
- Prefer environment variables for API keys.
- Use role boundaries for channel-facing agents.
- Log enough for debugging without storing sensitive payloads unnecessarily.

## 7. Troubleshooting

If a tool is unavailable:

1. Check config validation.
2. Check service logs.
3. Confirm plugin or MCP server startup.
4. Confirm capability registration.
5. Confirm agent role and tool permissions.
