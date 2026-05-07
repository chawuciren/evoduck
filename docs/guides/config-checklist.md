# Config Checklist

[English](../guides/config-checklist.md) | [简体中文](../zh-CN/guides/config-checklist.md)

Use this checklist before and after starting EvoDuck.

## 1. Before First Run

- Confirm `evoduck` is on PATH or use an explicit binary path.
- Confirm the config path you intend to use.
- Confirm runtime directory is writable.
- Confirm provider credentials are available through environment variables or config.
- Confirm the gateway port is free.

## 2. LLM Provider

- `llm.default_provider` exists in `llm.providers`.
- Provider `type` is supported.
- `base_url` is correct for compatible providers.
- `default_model` is set.
- Required API key is available.

## 3. Agent

- `default_agent` exists in `agents`.
- Agent workspace is writable.
- Agent role is intentional.
- Agent provider and model are valid.

## 4. Channels

- `webchat` exists for first-run validation.
- `weixin` or `wecom` credentials are set only when needed.
- Channel agent references are valid.
- Channel roles are narrow enough for the audience.

## 5. Tools

- Backend-call endpoints are explicitly allowlisted.
- MCP servers are reachable if configured.
- Plugin commands or URLs are valid if configured.

## 6. Memory and Knowledge

- Runtime memory directories are writable.
- Vector memory is disabled unless embedder config is ready.
- Knowledge paths do not contain secrets.

## 7. Service Mode

- No admin privileges required (PM2-style self-managed daemon).
- Confirm daemon config path is absolute.
- Confirm daemon working directory points to the current user's `.evoduck` root.
- Restart service after config changes.

## 8. Validation Commands

```bash
evoduck version
evoduck run
evoduck channel list
evoduck service status
```
