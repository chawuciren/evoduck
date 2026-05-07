# Usage and Setup Wizard

[English](../guides/usage.md) | [简体中文](../zh-CN/guides/usage.md)

This guide focuses on practical day-to-day runtime flows rather than installation mechanics or individual config fields.

## 1. Setup Wizard

Start the wizard explicitly:

```bash
evoduck setup
```

Or start EvoDuck normally:

```bash
evoduck run
```

If the config is still the default template, `run` automatically starts the first-run wizard.

## 2. What the Wizard Configures

The wizard currently:

1. Selects the default LLM provider.
2. Collects required provider settings.
3. Attempts to fetch available models.
4. Lets you choose the default model.
5. Sets `gateway.host`.
6. Sets `gateway.port`.
7. Optionally configures extra channels.
8. Saves the config file.

## 3. First-Run Provider Choices

Supported first-run provider families include:

- OpenAI-compatible providers
- Ollama / LM Studio / vLLM / LiteLLM
- OpenAI / Gemini / Anthropic
- Multiple OpenAI-compatible vendors
- Bedrock / Vertex AI

The wizard covers most first-time setup work, so you usually do not need to hand-write a full config file before first use.

For installation, uninstallation, binary update behavior, and first-run file locations, see [Install and First Run](install.md).

## 4. Daily Startup

Common startup:

```bash
evoduck run
```

Use a custom config:

```bash
evoduck run --config /path/to/config.yaml
```

Windows local build example:

```powershell
.\evoduck.exe run --config E:\path\to\config.yaml
```

## 5. Version and Update

Show version info:

```bash
evoduck version
```

Update to the latest release:

```bash
evoduck update
```

Check only:

```bash
evoduck update --check
```

Install a specific version or directory:

```bash
evoduck update --version v0.1.1
evoduck update --install-dir ~/.local/bin
```

If the target binary is the current process, `update` starts a helper and replaces the binary after the current process exits. On Windows, running services are stopped before update and restarted afterward if they were running.

## 6. Service Mode

EvoDuck uses a PM2-style self-managed daemon that requires no admin privileges. The daemon supervisor manages a worker process with automatic restart on crash.

`evoduck install` and `evoduck uninstall` belong to autostart management only. They do not install or remove the binary itself.

**Foreground mode (development):**

```bash
evoduck run
```

**Daemon mode (background):**

```bash
evoduck service start
evoduck service status
evoduck service stop
evoduck service restart
```

**Autostart configuration:**

```bash
evoduck install
evoduck uninstall
```

Process structure:
- **Daemon process**: lightweight supervisor that manages the worker
- **Worker process**: actual business logic
- Automatic restart on worker crash (max 5 retries, 3s initial delay, 2.0 backoff factor)

If `--config` is omitted, the daemon uses the current user's default config path. After changing config, restart the service so the new process reads the latest file.

## 7. Channel Management

List supported channel types:

```bash
evoduck channel types
```

Show channel details:

```bash
evoduck channel info weixin
evoduck channel info wecom
```

List configured channels:

```bash
evoduck channel list
```

Add channels:

```bash
evoduck channel add
evoduck channel add weixin
evoduck channel add wecom
```

Remove a channel:

```bash
evoduck channel remove --channel-id weixin-cs
```

Reconnect a channel by restarting the service:

```bash
evoduck channel reconnect --channel-id wecom-sales
```

## 8. Runtime Directories to Watch

- `agents/<agent-id>/`
- `shared/skills/`
- `logs/`
- `sessions/`
- `users/`

## 9. What to Edit

- System connections, models, MCP, plugins, channels: edit config.
- Agent behavior: edit `AGENTS.md` / `SOUL.md`.
- User profile or long-term user memory: inspect the corresponding user directory and its `USER.md` / `MEMORY.md`.
- Shared project knowledge or research: use Knowledge tools or the knowledge directory.
- Domain capability: add a `SKILL.md` package.

Guidance:

- Long-term information about a user belongs in Memory.
- Reusable project, system, process, or research information belongs in Knowledge.
- If you are unsure whether something is already captured, search Knowledge first.

## 10. Next Reading

1. [Configuration](configuration.md)
2. [Skills and MCP](skills-and-mcp.md)
3. [Channels](channels.md)
4. [Plugin Development](plugin-development.md)
