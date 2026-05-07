# 首次引导与日常使用

本文档聚焦日常运行流程，而不是安装细节或字段说明。

## 1. Setup Wizard

你可以显式运行：

```powershell
.\evoduck.exe setup
```

也可以直接运行：

```powershell
.\evoduck.exe run
```

如果当前配置仍是默认模板，`run` 会自动拉起首次引导。

## 2. 向导里会配置什么

当前向导会做这些事情：

1. 选择默认 provider
2. 根据 provider 类型输入必要信息
3. 尝试获取在线模型列表
4. 选择默认模型
5. 设置 `gateway.host`
6. 设置 `gateway.port`
7. 可选配置额外 channel
8. 保存配置

## 3. 首次 provider 选择

当前代码里支持用于首次向导的 provider 包括：

- OpenAI-compatible family
- Ollama / LM Studio / vLLM / LiteLLM
- OpenAI / Gemini / Anthropic
- 多个 OpenAI-compatible vendor
- Bedrock / Vertex AI

也就是说，向导本身已经承担了大部分首次接入工作，不需要先手写完整配置文件。

安装、卸载、二进制更新行为，以及首次运行生成的文件位置，请看 [安装与首次启动](install.md)。

## 4. 日常启动

平时最常用：

```powershell
.\evoduck.exe run
```

使用自定义配置：

```powershell
.\evoduck.exe run --config E:\path\to\config.yaml
```

## 5. 版本和更新

查看当前版本：

```powershell
evoduck version
```

检查并更新到最新 release：

```powershell
evoduck update
```

只检查不安装：

```powershell
evoduck update --check
```

指定版本或安装目录：

```powershell
evoduck update --version v0.1.0
evoduck update --install-dir $HOME\.local\bin
```

如果目标二进制正被当前进程占用，`update` 会启动独立 helper，等待当前进程退出后完成替换。Windows 服务运行中时，会先停止服务，更新后刷新服务定义，并按原状态重启服务。

## 6. 服务化运行

EvoDuck 采用 PM2-style 自管理守护进程，无需管理员权限。守护进程监控 Worker 进程，崩溃时自动重启。

`evoduck install` 和 `evoduck uninstall` 只属于自启动管理，不负责安装或删除二进制本体。

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
- Worker 崩溃时自动重启（最多 5 次，初始延迟 3s，退避因子 2.0）

如果不传 `--config`，守护进程会使用默认配置路径。修改配置后需要重启服务才能让新进程读取最新配置。

## 7. 渠道管理

查看支持的渠道：

```powershell
.\evoduck.exe channel types
```

查看某个渠道的参数：

```powershell
.\evoduck.exe channel info weixin
.\evoduck.exe channel info wecom
```

列出当前已配置渠道：

```powershell
.\evoduck.exe channel list
```

添加渠道：

```powershell
.\evoduck.exe channel add
.\evoduck.exe channel add weixin
.\evoduck.exe channel add wecom
```

删除渠道：

```powershell
.\evoduck.exe channel remove --channel-id weixin-cs
```

重连渠道：

```powershell
.\evoduck.exe channel reconnect --channel-id wecom-sales
```

## 8. 运行后主要看哪些目录

- `agents/<agent-id>/`
- `shared/skills/`
- `logs/`
- `sessions/`
- `users/`

## 9. 一般修改什么

- 想改系统连接、模型、MCP、插件、渠道：改配置
- 想改 Agent 行为：改 `AGENTS.md` / `SOUL.md`
- 想改当前用户画像或长期用户记忆：查看对应 user 目录下的 `USER.md` / `MEMORY.md`
- 想改共享项目知识或研究结论：使用 Knowledge 工具或对应知识目录
- 想加领域能力：新增 `SKILL.md`

补充建议：

- 如果是“关于这个用户”的长期信息，优先走 Memory
- 如果是“关于项目、系统、流程、研究”的可复用信息，优先走 Knowledge
- 如果你不确定某件事以前是否已经沉淀过，先查 Knowledge，再决定是否新增或更新

## 10. 推荐后续阅读

1. [配置结构](configuration.md)
2. [Skill 与 MCP](skills-and-mcp.md)
3. [Channel 配置](channels.md)
4. [插件开发](plugin-development.md)
