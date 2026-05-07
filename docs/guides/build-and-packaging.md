# Build and Packaging

[English](../guides/build-and-packaging.md) | [简体中文](../zh-CN/guides/build-and-packaging.md)

This guide records the build and delivery assets that exist in the repository today.

## 1. Current Delivery Forms

Confirmed delivery paths:

1. Source-built single executable.
2. Local deployment with the default runtime directory.
3. GitHub Release binary assets and online install scripts.
4. CLI self-update.
5. System service mode.
6. Plugin demo config for local integration.

## 2. Direct Build

Basic build:

```bash
go build -o evoduck ./cmd/evoduck
```

Windows PowerShell:

```powershell
go build -o evoduck.exe .\cmd\evoduck
```

## 3. Makefile Targets

The repository includes a `Makefile` with:

- `make build`
- `make run`
- `make test`
- `make clean`
- `make install-deps`

Current behavior:

- `build`: outputs to `build/evoduck`
- `run`: builds first, then runs `build/evoduck run`
- `test`: runs `go test -v ./...`

## 4. Release Asset Contract

Release asset names must match install scripts and `evoduck update`:

- `evoduck-windows-amd64.zip`
- `evoduck-windows-arm64.zip`
- `evoduck-linux-amd64.tar.gz`
- `evoduck-linux-arm64.tar.gz`
- `evoduck-darwin-amd64.tar.gz`
- `evoduck-darwin-arm64.tar.gz`

The archive root must contain `evoduck` or `evoduck.exe` directly.

Minimal manually assembled delivery directory:

```text
evoduck/
├── evoduck.exe           # or evoduck
├── README.md
└── docs/
```

EvoDuck generates default config and runtime directories on first run, so a fixed prebuilt runtime directory is not required.

## 5. Ready-to-Run Delivery

Recommended delivery flow:

1. Provide the `evoduck` executable.
2. Ask users to run `evoduck run`.
3. Let EvoDuck initialize default directories and config.
4. Let Setup Wizard complete first-time configuration.

The product distribution model is executable plus first-run setup, not a large preconfigured runtime bundle.

## 6. Service Mode Delivery

For long-running deployments, EvoDuck uses a PM2-style self-managed daemon:

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
- No admin privileges required

Service mode is useful for:

- local always-on runtime
- internal service processes
- long-running deployments

## 7. Plugin Demo Config

The repository includes:

- `examples/plugin-demo.yaml`

It uses:

- `ollama` as the default provider
- `webchat` as the default channel
- local `echo-tool` plugin enabled

This is intended for plugin integration and local development, not as a production default config.

## 8. Demo Plugins

Demo plugins in the repository:

- `plugins/echo-tool`
- `plugins/mock-provider`
- `plugins/mock-channel`
- `plugins/mock-hook`

They are best treated as:

- development examples
- integration fixtures
- extension references

## 9. Online Update

Installed users can update with:

```bash
evoduck update
```

Install scripts can also be rerun to update an existing installation:

```powershell
irm https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/install.ps1 | iex
```

```sh
curl -fsSL https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/install.sh | sh
```

`update` downloads the matching release asset, extracts it, and replaces the user-level binary. Self-replacement uses a helper process on all platforms. Windows service updates stop the service first, refresh service definition after replacement, and restart it if it was previously running.

## 10. Documentation Boundary

This guide only documents current build and delivery assets. If the project later adds installers, prebuilt runtime bundles, or platform-specific packages, add a dedicated distribution guide.
