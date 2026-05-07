# Release 安装方案

本文档定义 EvoDuck 第一阶段的正式分发目标：提供一个不携带 Node.js 和 Python 运行时的轻量 CLI 安装方案。

## 1. 第一阶段目标

当前阶段只做这些事情：

1. 从 GitHub Releases 下载平台对应的 `evoduck` 二进制
2. 安装到用户级可执行目录
3. 自动补齐用户级 `PATH`
4. 保持运行数据目录继续使用 `.evoduck`

当前阶段明确不做：

1. 不打包 Node.js
2. 不打包 Python
3. 不附带系统级安装器
4. 不改变当前首次启动初始化逻辑
5. 不加入自动更新机制

## 2. 安装目录与运行目录

安装目录和运行目录分离：

1. Linux/macOS 安装目录：`~/.local/bin/evoduck`
2. Windows 安装目录：`%USERPROFILE%\.local\bin\evoduck.exe`
3. Linux/macOS 运行目录：`~/.evoduck`
4. Windows 运行目录：`%USERPROFILE%\.evoduck`

这样后续升级只替换二进制，不触碰用户配置、日志、会话和 Skill 数据。

## 3. Release 资产命名

安装脚本按固定命名下载 release 资产：

```text
evoduck-windows-amd64.zip
evoduck-windows-arm64.zip
evoduck-darwin-amd64.tar.gz
evoduck-darwin-arm64.tar.gz
evoduck-linux-amd64.tar.gz
evoduck-linux-arm64.tar.gz
```

压缩包第一阶段建议只包含单个程序文件：

1. Unix 系统：`evoduck`
2. Windows：`evoduck.exe`

这样安装脚本不需要额外推断复杂目录结构。

## 4. 安装入口

当前仓库提供两个安装脚本：

1. `scripts/install.sh`
2. `scripts/install.ps1`

预期使用方式：

Unix:

```sh
curl -fsSL https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/install.ps1 | iex
```

默认会安装 latest release。也可以通过环境变量指定版本：

Unix:

```sh
EVODUCK_VERSION=v0.1.0 sh scripts/install.sh
```

Windows PowerShell:

```powershell
$env:EVODUCK_VERSION = 'v0.1.0'
.\scripts\install.ps1
```

## 5. 安装脚本职责

脚本只负责：

1. 检测操作系统和 CPU 架构
2. 拼接 GitHub Releases 下载地址
3. 下载并解压对应平台包
4. 将二进制放入用户级 bin 目录
5. 检查并补齐用户级 `PATH`

脚本不负责：

1. 初始化 `.evoduck`
2. 预写配置文件
3. 自动运行 `evoduck setup`
4. 安装系统服务

`.evoduck` 仍然由程序在首次运行时按现有逻辑自动初始化。

## 6. 文档与发布要求

要让这套方案真正可用，后续需要补齐这些配套：

1. GitHub Actions release workflow
2. 多平台交叉编译产物
3. `checksums.txt`
4. README 的正式安装说明

当前仓库内脚本和文档已经按这套目录约定落地，下一步只需要把 release 构建链路接上。
