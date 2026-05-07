# 构建与分发准备

本文档只记录当前仓库已经存在的构建和交付资产，不预设未来尚未落地的发布方式。

## 1. 当前已存在的交付形态

按仓库现状，可确认的交付方式主要是：

1. 源码构建出的单个可执行文件
2. 配合默认运行目录的本地部署
3. GitHub Release 二进制资产和在线安装脚本
4. CLI 自更新
5. 作为系统服务运行
6. 用示例配置进行插件联调

## 2. 直接构建

最直接方式：

```bash
go build -o evoduck ./cmd/evoduck
```

Windows PowerShell：

```powershell
go build -o evoduck.exe .\cmd\evoduck
```

## 3. Makefile 入口

仓库当前已有 `Makefile`，支持这些目标：

- `make build`
- `make run`
- `make test`
- `make clean`
- `make install-deps`

当前 `Makefile` 的行为是：

- `build`: 输出到 `build/evoduck`
- `run`: 先构建，再执行 `build/evoduck run`
- `test`: 运行 `go test -v ./...`

## 4. 当前更像“分发包”的最小内容

正式 release asset 命名需要和安装脚本、`evoduck update` 保持一致：

- `evoduck-windows-amd64.zip`
- `evoduck-windows-arm64.zip`
- `evoduck-linux-amd64.tar.gz`
- `evoduck-linux-arm64.tar.gz`
- `evoduck-darwin-amd64.tar.gz`
- `evoduck-darwin-arm64.tar.gz`

归档根目录需要包含 `evoduck` 或 `evoduck.exe`。

如果现在要手工整理一份可交付目录，比较现实的最小集合通常是：

```text
evoduck/
├── evoduck.exe           # 或 evoduck
├── README.md
└── docs/
```

原因：

- 程序首次运行会自动生成默认配置和运行目录
- 不需要你额外预放一份固定模板配置文件

## 5. 如果要交付“可直接启动”的版本

当前最稳妥的交付说明应该是：

1. 提供 `evoduck` 可执行文件
2. 让用户运行 `evoduck run`
3. 由程序自动初始化默认目录和默认配置
4. 由 Setup Wizard 完成第一次配置

也就是说，当前产品的分发重点不是“附赠一大堆预配置文件”，而是“可执行文件 + 首次引导”。

## 6. 服务模式交付

如果要面向长期运行场景，EvoDuck 采用 PM2-style 自管理守护进程：

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
- 无需管理员权限

这更适合：

- 本机常驻运行
- 内部服务进程
- 长期运行场景

## 7. 插件联调示例配置

仓库里当前有一份现成示例：

- `examples/plugin-demo.yaml`

它的特点是：

- 默认 provider 使用 `ollama`
- 默认 channel 使用 `webchat`
- 默认启用本地 `echo-tool` 插件
- 适合验证插件链路和本地开发场景

如果要做“开发者示例包”，这份文件很适合作为联调起点。

## 8. 插件 demo 资产

仓库中已有的 demo plugin：

- `plugins/echo-tool`
- `plugins/mock-provider`
- `plugins/mock-channel`
- `plugins/mock-hook`

这些内容更适合作为：

- 开发示例
- 联调样例
- 扩展参考

而不是面向普通最终用户的默认安装内容。

## 9. 在线更新

已安装用户可以通过以下命令更新：

```bash
evoduck update
```

安装脚本也支持重复执行更新已有安装：

```powershell
irm https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/install.ps1 | iex
```

```sh
curl -fsSL https://raw.githubusercontent.com/chawuciren/evoduck/main/scripts/install.sh | sh
```

`update` 下载与当前平台匹配的 release asset，解压后替换用户级安装目录中的二进制。当前进程自替换在全平台通过 helper 完成：helper 等待父进程退出后再替换目标文件。Windows 服务场景会先停止服务，更新后刷新服务定义并按原状态重启。

## 10. 当前文档边界

这篇文档只陈述当前已经存在的构建和交付资产。

如果你后续确定了正式分发策略，例如：

- zip 包
- 安装器
- 预置运行目录
- 平台化发布

再单独补一篇真正意义上的“发布与分发文档”会更合适。
