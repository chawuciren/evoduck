# Release Installation Contract

[English](../guides/release-installation-plan.md) | [简体中文](../zh-CN/guides/release-installation-plan.md)

This guide defines the current release install contract used by install scripts and `evoduck update`.

## 1. Install Locations

Windows:

```text
%USERPROFILE%\.local\bin\evoduck.exe
```

Linux/macOS:

```text
~/.local/bin/evoduck
```

Runtime data:

```text
%USERPROFILE%\.evoduck
~/.evoduck
```

## 2. Release Assets

Expected GitHub Release assets:

```text
evoduck-windows-amd64.zip
evoduck-windows-arm64.zip
evoduck-linux-amd64.tar.gz
evoduck-linux-arm64.tar.gz
evoduck-darwin-amd64.tar.gz
evoduck-darwin-arm64.tar.gz
checksums.txt
```

Archive root must directly contain `evoduck.exe` or `evoduck`.

## 3. Install Scripts

Script entry points:

```powershell
irm https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/install.ps1 | iex
```

```sh
curl -fsSL https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/install.sh | sh
```

Scripts are install-or-update:

1. Ensure install directory exists.
2. If no binary exists, download and install latest release asset.
3. If a binary exists, prefer `evoduck update`.
4. If built-in update is missing or fails, fall back to script-managed replacement.
5. Ensure user PATH where possible.

## 4. Self-Update

```bash
evoduck update
```

Supported options:

```bash
evoduck update --check
evoduck update --version v0.1.1
evoduck update --force
evoduck update --install-dir ~/.local/bin
```

Environment variables:

- `EVODUCK_VERSION`
- `EVODUCK_INSTALL_DIR`
- `EVODUCK_REPO`

## 5. Service Compatibility

Windows service install uses top-level commands:

```powershell
evoduck install
evoduck service start
evoduck service status
evoduck service stop
evoduck uninstall
```

When `--config` is omitted, service install resolves the current user's default config path and keeps runtime data under the current user's `.evoduck` directory.

## 6. Release Workflow

Tag names should start with `v`, for example:

```bash
git tag -a v0.1.1 -m "Release v0.1.1"
git push origin v0.1.1
```

The release workflow builds all supported platform assets and uploads checksums.
