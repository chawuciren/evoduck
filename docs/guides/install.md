# Install and First Run

[English](../guides/install.md) | [简体中文](../zh-CN/guides/install.md)

This guide is the authoritative reference for installing, uninstalling, updating, and starting `evoduck` for the first time, along with the current configuration and runtime file locations.

## 1. Quick Install

Default binary install locations:

1. Linux/macOS: `~/.local/bin/evoduck`
2. Windows: `%USERPROFILE%\.local\bin\evoduck.exe`

Runtime data remains under:

1. Linux/macOS: `~/.evoduck`
2. Windows: `%USERPROFILE%\.evoduck`

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/install.ps1 | iex
```

If your network requires an HTTP proxy, `irm -Proxy` only proxies the script download. The script itself reads `HTTPS_PROXY` and `HTTP_PROXY` when downloading release assets:

```powershell
$env:HTTP_PROXY="http://127.0.0.1:7897"
$env:HTTPS_PROXY="http://127.0.0.1:7897"
irm -Proxy "http://127.0.0.1:7897" https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/install.ps1 | iex
```

macOS/Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/install.sh | sh
```

Rerunning the install script updates an existing installation. The script first tries `evoduck update`; if the installed binary is too old or update fails, it falls back to downloading and replacing the binary itself. After the binary is in place, the script calls `evoduck install` to register autostart.

Important distinction:

- install scripts perform binary installation or update
- `evoduck install` only registers autostart for the current binary and config path

Quick uninstall:

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/uninstall.ps1 | iex
```

macOS/Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/uninstall.sh | sh
```

The uninstall scripts stop the running service first, remove autostart, remove the installed binary, remove the installer-managed PATH entry, and ask whether to delete runtime data.

Important distinction:

- uninstall scripts perform the full uninstall flow
- `evoduck uninstall` only removes autostart

## 2. Manual Install

If the online script is unavailable, install from the latest GitHub Release manually:

```text
https://github.com/chawuciren/evoduck/releases/latest
```

Release asset names:

- Windows x64: `evoduck-windows-amd64.zip`
- Windows ARM64: `evoduck-windows-arm64.zip`
- Linux x64: `evoduck-linux-amd64.tar.gz`
- Linux ARM64: `evoduck-linux-arm64.tar.gz`
- macOS Intel: `evoduck-darwin-amd64.tar.gz`
- macOS Apple Silicon: `evoduck-darwin-arm64.tar.gz`

### Windows Manual Install

1. Download `evoduck-windows-amd64.zip` for x64 Windows, or `evoduck-windows-arm64.zip` for Windows ARM64.
2. Extract the zip file.
3. Find `evoduck.exe` in the archive root.
4. Create this directory if it does not exist: `%USERPROFILE%\.local\bin`.
5. Move `evoduck.exe` to `%USERPROFILE%\.local\bin\evoduck.exe`.
6. Add `%USERPROFILE%\.local\bin` to your user `Path` environment variable.
7. Open a new PowerShell window and run `evoduck version`.

### macOS Manual Install

1. Download `evoduck-darwin-amd64.tar.gz` for Intel Macs, or `evoduck-darwin-arm64.tar.gz` for Apple Silicon.
2. Extract the tarball.
3. Find `evoduck` in the archive root.
4. Create `~/.local/bin` if it does not exist.
5. Move `evoduck` to `~/.local/bin/evoduck`.
6. Make sure the file is executable.
7. Add `~/.local/bin` to your shell `PATH` if it is not already present.
8. Open a new terminal and run `evoduck version`.

### Linux Manual Install

1. Download `evoduck-linux-amd64.tar.gz` for x64 Linux, or `evoduck-linux-arm64.tar.gz` for ARM64 Linux.
2. Extract the tarball.
3. Find `evoduck` in the archive root.
4. Create `~/.local/bin` if it does not exist.
5. Move `evoduck` to `~/.local/bin/evoduck`.
6. Make sure the file is executable.
7. Add `~/.local/bin` to your shell `PATH` if it is not already present.
8. Open a new terminal and run `evoduck version`.

For macOS/Linux, common shell profile files are `~/.profile`, `~/.bashrc`, and `~/.zshrc`.

After manual install, run `evoduck run`. EvoDuck creates `.evoduck`, writes the default config, and starts the setup wizard if the provider is not configured. Use `evoduck channel add` later to configure `weixin` or `wecom`, or edit `~/.evoduck/config/config.yaml` directly.

## 3. Update Existing Installation

```bash
evoduck update
```

Useful options:

```bash
evoduck update --check
evoduck update --version v0.1.1
evoduck update --force
evoduck update --install-dir ~/.local/bin
```

Environment variables:

- `EVODUCK_VERSION`: release version, default `latest`
- `EVODUCK_INSTALL_DIR`: override install directory
- `EVODUCK_REPO`: override GitHub repository, default `chawuciren/evoduck`

Self-update is compatible with the current process running on all supported platforms. When the target binary is the current process, `update` starts a helper process that waits for the parent process to exit before replacing the file. On Windows, if the EvoDuck service is running, update stops the service, replaces the binary, refreshes the service definition, and restarts it if it was previously running.

## 4. Build From Source

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

Release scripts require matching GitHub Release assets and the standard archive names documented earlier in this guide.

## 5. First Run

After install or build, run:

```bash
evoduck run
```

From a local Windows build directory:

```powershell
.\evoduck.exe run
```

You can also start the setup wizard directly:

```bash
evoduck setup
```

## 6. Default Config Path

When `--config` is not provided, EvoDuck uses:

- Windows: `%USERPROFILE%\.evoduck\config\config.yaml`
- Linux/macOS: `~/.evoduck/config/config.yaml`

Custom path:

```powershell
.\evoduck.exe run --config E:\path\to\config.yaml
```

## 7. First-Run Initialization

On first run, EvoDuck automatically creates:

- config directory
- runtime root
- default config file
- `logs/`
- `shared/skills/`
- built-in Skills
- default agent scaffold files

## 8. Default Runtime Directory

Default runtime layout:

```text
.evoduck/
├── config/
│   └── config.yaml
├── agents/
│   └── admin-bot/
├── logs/
├── scheduler/
├── sessions/
├── shared/
│   └── skills/
└── users/
```

## 9. Automatic Setup Wizard

`evoduck run` starts the first-run wizard when:

- the current config is still the default template
- the default provider is not fully configured

This is the main first-use path.

## 10. Service Mode

EvoDuck uses a PM2-style self-managed daemon that requires no admin privileges.

**Foreground mode (development):**

```powershell
evoduck run
```

**Daemon mode (background):**

```powershell
evoduck service start
evoduck service status
evoduck service stop
evoduck service restart
```

**Autostart configuration:**

```powershell
evoduck install
evoduck uninstall
```

Process structure:
- **Daemon process**: lightweight supervisor that manages the worker
- **Worker process**: actual business logic
- Automatic restart on worker crash (max 5 retries with exponential backoff)

If `--config` is omitted, the daemon uses the current user's default config path under `%USERPROFILE%\.evoduck\config\config.yaml`.

## 11. Next Steps

After install and first run, continue with:

1. [Usage and Setup Wizard](usage.md)
2. [Configuration](configuration.md)
