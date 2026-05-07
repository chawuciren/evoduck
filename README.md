# EvoDuck

[English](README.md) | [简体中文](README.zh-CN.md)

EvoDuck is a local-first AI agent framework written in Go for personal workflows, customer support, and internal team support.

It combines native memory, knowledge accumulation, context compression, background experience curation, MCP integration, language-agnostic plugins, and multi-channel delivery in a lightweight runtime.

![EvoDuck banner](docs/assets/brand/banner.png)

![EvoDuck main screenshot](docs/assets/screenshots/main-screenshot.png)

## Why EvoDuck

EvoDuck is built for operators who want AI agents that are inspectable, extensible, and practical in real long-running environments.

Instead of treating the agent as disposable chat, EvoDuck keeps configuration, memory, logs, sessions, and runtime assets close to the operator. It is designed to start fast, stay lightweight, and get stronger through repeated use.

## Key Highlights

- Built for both personal and enterprise-facing use with native `admin`, `employee`, and `customer` role boundaries.
- Works in simple admin mode for solo use, and also supports customer support and internal team support scenarios.
- Requires very little configuration to become useful for support-style workloads.
- Includes a native knowledge base and native memory system so the agent can accumulate durable context over time.
- Performs automatic memory extraction and memory refinement instead of treating every conversation as isolated.
- Uses native context compression to balance capability and token efficiency for long-running use.
- Supports background experience curation, turning valuable work into reusable knowledge and skills.
- Keeps first-class MCP support so teams can reuse the mature MCP ecosystem instead of rebuilding integrations from scratch.
- Provides a lightweight, language-agnostic plugin mechanism for `tool`, `provider`, `channel`, and `hook` extensions.
- Connects agents to real channels including `webchat`, `weixin`, and `wecom`.
- Written in Go for fast startup, responsive execution, and low resource consumption.

## Typical Flow

1. Install or build the `evoduck` binary.
2. Run `evoduck run` for the first time.
3. EvoDuck creates the default config and runtime directories.
4. If the config is still the default template, EvoDuck enters the setup wizard.
5. After setup, extend the system with channels, Skills, MCP servers, and plugins.

## Extensibility

EvoDuck does not force a single extension model.

- Use Skills to package reusable behavior and operational guidance.
- Use MCP to quickly plug into an existing and mature external tool ecosystem.
- Use plugins when you want lightweight integration for your own tools, providers, channels, or hooks.
- Plugins are language-agnostic, which makes it easier to connect internal systems and custom business logic without forcing everything into the Go core.

For third-party skills, users can give an agent a local folder, zip package, or repository address and ask it to install the skill, verify it, and adapt it to EvoDuck's `SKILL.md` format when the source was written for another agent ecosystem. See [Skills and MCP](docs/guides/skills-and-mcp.md).

## Installation

For the full current install, uninstall, update, and first-run behavior, start with [Install and First Run](docs/guides/install.md).

### Quick Install

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/install.ps1 | iex
```

With an HTTP proxy:

```powershell
$env:HTTP_PROXY="http://127.0.0.1:7897"
$env:HTTPS_PROXY="http://127.0.0.1:7897"
irm -Proxy "http://127.0.0.1:7897" https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/install.ps1 | iex
```

macOS/Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/install.sh | sh
```

The install scripts install EvoDuck into a user-level bin directory, register autostart by calling `evoduck install`, and can be rerun later to update an existing installation.

### Quick Uninstall

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/uninstall.ps1 | iex
```

macOS/Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/uninstall.sh | sh
```

The uninstall scripts stop the running service first, then remove autostart, remove the installed binary, remove the installer-managed PATH entry, and ask whether to delete runtime data. The CLI command `evoduck uninstall` only removes autostart.

### Manual Install

If the online script is unavailable, install from the latest GitHub Release manually:

1. Open `https://github.com/chawuciren/evoduck/releases/latest`.
2. Download the asset for your system:
   - Windows x64: `evoduck-windows-amd64.zip`
   - Windows ARM64: `evoduck-windows-arm64.zip`
   - macOS Intel: `evoduck-darwin-amd64.tar.gz`
   - macOS Apple Silicon: `evoduck-darwin-arm64.tar.gz`
   - Linux x64: `evoduck-linux-amd64.tar.gz`
   - Linux ARM64: `evoduck-linux-arm64.tar.gz`
3. Extract the archive. The archive root contains `evoduck.exe` on Windows or `evoduck` on macOS/Linux.
4. Move the executable to a user-level bin directory:
   - Windows: `%USERPROFILE%\.local\bin\evoduck.exe`
   - macOS/Linux: `~/.local/bin/evoduck`
5. Add that bin directory to your `PATH`.
6. Open a new terminal and verify with `evoduck version`.

First use after manual install:

1. Run `evoduck run`.
2. EvoDuck creates the default config under `.evoduck`.
3. If the provider is not configured, the setup wizard starts automatically.
4. Configure channels later with `evoduck channel add`, or edit the config file directly.

### Update Existing Installation

```bash
evoduck update
```

Useful options:

```bash
evoduck update --check
evoduck update --version v0.1.0
evoduck update --force
evoduck update --install-dir ~/.local/bin
```

Environment overrides:

- `EVODUCK_VERSION`: install a specific release version, default `latest`
- `EVODUCK_INSTALL_DIR`: override the install directory
- `EVODUCK_REPO`: override the GitHub repository, default `chawuciren/evoduck`

Binary install locations:

- Linux/macOS: `~/.local/bin/evoduck`
- Windows: `%USERPROFILE%\.local\bin\evoduck.exe`

Runtime data locations:

- Linux/macOS: `~/.evoduck`
- Windows: `%USERPROFILE%\.evoduck`

### Build From Source

Requirements:

- Go 1.26+

```bash
git clone https://github.com/chawuciren/evoduck.git
cd evoduck
go mod download
go build -o evoduck ./cmd/evoduck
```

Windows PowerShell:

```powershell
go build -o evoduck.exe .\cmd\evoduck
```

## First Run

Run EvoDuck directly after installation or build:

```bash
evoduck run
```

If you start from a local Windows build:

```powershell
.\evoduck.exe run
```

Default config path when `--config` is not provided:

- Windows: `%USERPROFILE%\.evoduck\config\config.yaml`
- Linux/macOS: `~/.evoduck/config/config.yaml`

On first run, EvoDuck automatically:

- creates the config file
- creates the runtime root directory
- creates `logs/`
- creates `shared/skills/`
- writes built-in Skills
- creates scaffold files for the default agent

If EvoDuck detects that the config is still using the default template and the default provider is not fully configured, it automatically enters the setup wizard.

## Setup Wizard

You can also start the wizard directly:

```bash
evoduck setup
```

The wizard currently performs these steps:

1. Select the default LLM provider.
2. Collect the required provider-specific settings.
3. Try to fetch available models and let you choose a default model.
4. Set `gateway.host` and `gateway.port`.
5. Optionally configure extra channels.
6. Save the result back to the config file.

## Channels

Current CLI entrypoints:

```bash
evoduck channel types
evoduck channel info weixin
evoduck channel info wecom
evoduck channel list
evoduck channel add
evoduck channel add weixin
evoduck channel add wecom
```

Notes:

- `webchat` is built in and does not require separate setup.
- `weixin` uses a QR-code login flow.
- `wecom` uses `bot_id` and `secret` configuration.

## Service Mode

EvoDuck uses a PM2-style self-managed daemon that requires no admin privileges:

```bash
# Foreground mode (development)
evoduck run

# Daemon mode (background)
evoduck service start
evoduck service status
evoduck service stop
evoduck service restart

# Autostart configuration (no admin required)
evoduck install
evoduck uninstall
```

Process structure:
- **Daemon process**: lightweight supervisor that manages the worker
- **Worker process**: actual business logic
- Automatic restart on worker crash (max 5 retries with exponential backoff)

If `--config` is omitted, the daemon uses the current user's default config path under `.evoduck`.

From a local build directory, prefix the binary path as needed:

```powershell
.\evoduck.exe service start
.\evoduck.exe service status
```

## Documentation

English docs are the default. Chinese docs are preserved under [`docs/zh-CN/`](docs/zh-CN/INDEX.md).

Suggested reading order:

1. [Install and First Run](docs/guides/install.md)
2. [Usage and Setup Wizard](docs/guides/usage.md)
3. [Configuration](docs/guides/configuration.md)
4. [Skills and MCP](docs/guides/skills-and-mcp.md)
5. [Channels](docs/guides/channels.md)
6. [Build and Packaging](docs/guides/build-and-packaging.md)
7. [Plugin Development](docs/guides/plugin-development.md)

Additional docs:

- [Config Checklist](docs/guides/config-checklist.md)
- [Embedding Config](docs/guides/embedding-config.md)
- [Test Guide](docs/guides/test-guide.md)
- [Tools](docs/guides/tools.md)
- [Weixin Channel Details](docs/guides/weixin-channel.md)
- [Full Documentation Index](docs/INDEX.md)

## Project Layout

```text
cmd/
docs/
internal/
pkg/
plugins/
scripts/
web/
```
