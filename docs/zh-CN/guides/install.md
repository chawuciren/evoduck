# 安装与首次启动

本文档是当前安装、卸载、更新、首次启动，以及配置文件与运行目录位置的权威说明。

## 1. 快速安装

当前仓库已经定义第一阶段的 release 安装目录约定：

1. Linux/macOS: `~/.local/bin/evoduck`
2. Windows: `%USERPROFILE%\\.local\\bin\\evoduck.exe`

运行数据目录继续保持：

1. Linux/macOS: `~/.evoduck`
2. Windows: `%USERPROFILE%\\.evoduck`

Windows PowerShell：

```powershell
irm https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/install.ps1 | iex
```

macOS/Linux：

```sh
curl -fsSL https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/install.sh | sh
```

重复运行安装脚本会进入更新路径：优先调用已安装二进制的 `evoduck update`，旧版本没有该命令或更新失败时，脚本会 fallback 到自行下载并替换。二进制就位后，脚本会调用 `evoduck install` 注册自启动。

要明确区分：

- 安装脚本负责安装或更新二进制
- `evoduck install` 只负责为当前二进制和配置路径注册自启动

快速卸载：

Windows PowerShell：

```powershell
irm https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/uninstall.ps1 | iex
```

macOS/Linux：

```sh
curl -fsSL https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/uninstall.sh | sh
```

卸载脚本会先停止正在运行的服务，再移除自启动、删除已安装二进制、移除安装脚本写入的 PATH 项，并询问是否删除运行数据。

要明确区分：

- 卸载脚本负责完整卸载流程
- `evoduck uninstall` 只负责移除自启动

## 2. 手动安装

如果在线脚本不可用，可以从最新 GitHub Release 手动安装：

```text
https://github.com/chawuciren/evoduck/releases/latest
```

Release 资产命名：

- Windows x64: `evoduck-windows-amd64.zip`
- Windows ARM64: `evoduck-windows-arm64.zip`
- Linux x64: `evoduck-linux-amd64.tar.gz`
- Linux ARM64: `evoduck-linux-arm64.tar.gz`
- macOS Intel: `evoduck-darwin-amd64.tar.gz`
- macOS Apple Silicon: `evoduck-darwin-arm64.tar.gz`

### Windows 手动安装

1. x64 Windows 下载 `evoduck-windows-amd64.zip`，Windows ARM64 下载 `evoduck-windows-arm64.zip`。
2. 解压 zip 文件。
3. 在压缩包根目录找到 `evoduck.exe`。
4. 如果 `%USERPROFILE%\.local\bin` 不存在，先创建这个目录。
5. 把 `evoduck.exe` 移动到 `%USERPROFILE%\.local\bin\evoduck.exe`。
6. 把 `%USERPROFILE%\.local\bin` 加入当前用户的 `Path` 环境变量。
7. 重新打开 PowerShell，运行 `evoduck version` 验证。

### macOS 手动安装

1. Intel Mac 下载 `evoduck-darwin-amd64.tar.gz`，Apple Silicon 下载 `evoduck-darwin-arm64.tar.gz`。
2. 解压 tarball。
3. 在压缩包根目录找到 `evoduck`。
4. 如果 `~/.local/bin` 不存在，先创建这个目录。
5. 把 `evoduck` 移动到 `~/.local/bin/evoduck`。
6. 确认文件有可执行权限。
7. 如果 `~/.local/bin` 还没有加入 shell `PATH`，把它加入 PATH。
8. 重新打开终端，运行 `evoduck version` 验证。

### Linux 手动安装

1. x64 Linux 下载 `evoduck-linux-amd64.tar.gz`，ARM64 Linux 下载 `evoduck-linux-arm64.tar.gz`。
2. 解压 tarball。
3. 在压缩包根目录找到 `evoduck`。
4. 如果 `~/.local/bin` 不存在，先创建这个目录。
5. 把 `evoduck` 移动到 `~/.local/bin/evoduck`。
6. 确认文件有可执行权限。
7. 如果 `~/.local/bin` 还没有加入 shell `PATH`，把它加入 PATH。
8. 重新打开终端，运行 `evoduck version` 验证。

macOS/Linux 常见 PATH 配置文件包括 `~/.profile`、`~/.bashrc` 和 `~/.zshrc`。

手动安装后，运行 `evoduck run`。EvoDuck 会创建 `.evoduck`、写入默认配置；如果 provider 尚未配置，会自动进入 setup wizard。后续可以用 `evoduck channel add` 配置 `weixin` 或 `wecom`，也可以直接编辑 `~/.evoduck/config/config.yaml`。

## 3. 更新已有安装

```bash
evoduck update
```

常用选项：

```bash
evoduck update --check
evoduck update --version v0.1.0
evoduck update --force
evoduck update --install-dir ~/.local/bin
```

环境变量：

- `EVODUCK_VERSION`: 指定 release 版本，默认 `latest`
- `EVODUCK_INSTALL_DIR`: 覆盖安装目录
- `EVODUCK_REPO`: 覆盖 GitHub 仓库，默认 `chawuciren/evoduck`

全平台自更新都会兼容“当前进程正在运行”的情况：当目标二进制就是当前进程时，`update` 会生成独立 helper，等待当前进程退出后再替换文件。Windows 服务正在运行时，更新会先停止服务，替换完成后刷新服务定义，并按原状态重启服务。

## 4. 源码构建

前置要求：

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

这套脚本依赖 GitHub Releases 中存在对应平台的二进制资产，并使用本文前面列出的标准压缩包命名。

## 5. 首次运行

构建完成后直接运行：

```powershell
.\evoduck.exe run
```

也可以先手动触发首次向导：

```powershell
.\evoduck.exe setup
```

## 6. 默认配置路径

如果不传 `--config`，程序会使用默认路径：

- Windows: `%USERPROFILE%\\.evoduck\\config\\config.yaml`
- Linux/macOS: `~/.evoduck/config/config.yaml`

也可以显式指定：

```powershell
.\evoduck.exe run --config E:\path\to\config.yaml
```

## 7. 首次自动初始化内容

第一次运行时，程序会自动：

- 创建配置目录
- 创建运行根目录
- 生成默认配置文件
- 创建 `logs/`
- 创建 `shared/skills/`
- 写入内置 Skill
- 为默认 Agent 创建 scaffold

## 8. 默认运行根目录结构

默认情况下，运行根目录大致如下：

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

## 9. 自动进入首次引导的条件

当程序检测到：

- 当前配置还是默认模板
- 默认 provider 还没有完成必要设置

它会在 `run` 时自动触发首次引导向导。

这也是当前产品真实的“第一次用起来”的主路径。

## 10. 服务模式

EvoDuck 采用 PM2-style 自管理守护进程，无需管理员权限。

**前台模式（开发调试）：**

```powershell
evoduck run
```

**守护进程模式（后台运行）：**

```powershell
evoduck service start
evoduck service status
evoduck service stop
evoduck service restart
```

**开机自启配置：**

```powershell
evoduck install
evoduck uninstall
```

进程结构：
- **守护进程**：轻量级 supervisor，监控 Worker 进程
- **Worker 进程**：实际业务逻辑
- Worker 崩溃时自动重启（最多 5 次，指数退避）

如果不传 `--config`，守护进程会使用默认配置路径：`%USERPROFILE%\.evoduck\config\config.yaml`。

## 11. 下一步

安装和首次启动完成后，继续看：

1. [首次引导与日常使用](usage.md)
2. [配置结构](configuration.md)
