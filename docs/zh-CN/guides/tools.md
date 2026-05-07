# 工具系统

本文档描述当前代码中可见的工具来源，以及常见工具类别。

## 1. 工具来源

EvoDuck 中的工具主要来自三类：

1. 内置工具
2. MCP Server 提供的工具
3. WS 插件提供的工具

Skill 不是工具，但会指导 agent 何时使用工具。

## 2. 当前常见内置工具

### 基础工具

- `time`
- `web_fetch`
- `http_call`
- `backend_call`

### 文件工具

- `file_read`
- `file_list`
- `file_write`
- `file_edit`
- `file_patch`

`file_write` 只用于完整创建或完整覆盖文件。局部修改使用 `file_edit`，补丁式修改使用 `file_patch`。

### Memory 工具

- `memory_search`
- `memory_read`

Memory 是 Markdown 文件，不提供专用写入工具。需要写入或修改 memory 时，使用 `file_write` 或 `file_edit` 写对应 Markdown 文件。

### Knowledge 工具

- `knowledge_tree`
- `knowledge_search`
- `knowledge_read`

Knowledge 是共享 Markdown 文件，不提供专用写入或管理工具。需要创建或修改 knowledge 条目时，使用文件工具写 `knowledge/` 下的 Markdown 文件。

### Session 工具

- `sessions_list`
- `sessions_history`
- `sessions_send`
- `sessions_run`

### Schedule 工具

- `schedule_list`
- `schedule_create`
- `schedule_enable`
- `schedule_disable`
- `schedule_delete`
- `schedule_trigger`

### Skill 工具

- `skill_list`
- `skill_detail`
- `skill_use`
- `skill`

### 其他运行时工具

- `task_plan`
- 文件、进程、补丁、代码执行类工具

## 3. Memory 与 Knowledge

### Memory

Memory 面向“人”，保存当前用户相关的画像、长期偏好、限制、事实和近期上下文。

常见文件：

- `USER.md`: 当前用户画像、称呼、背景、回答偏好、边界。
- `MEMORY.md`: 当前用户长期事实、限制、决策和协作偏好。
- `memory/YYYY-MM-DD.md`: 当前用户近期上下文或临时任务记忆。

读取流程：

- 用 `memory_search` 定位相关条目。
- 用 `memory_read` 读取具体文件或行范围。

写入流程：

- 用 `file_write` 创建完整 Markdown 文件。
- 用 `file_edit` 追加、前置、替换或在锚点附近插入内容。

### Knowledge

Knowledge 面向“事”，保存共享可复用的项目知识、架构说明、研究结论、排障记录、runbook 和稳定工作流。

读取流程：

- 用 `knowledge_tree` 查看知识库结构。
- 用 `knowledge_search` 定位相关条目。
- 用 `knowledge_read` 读取具体文件或行范围。

写入流程：

- 用 `file_write` 创建新的 `knowledge/.../*.md` 文件。
- 用 `file_edit` 更新已有知识条目。
- 写入前优先 search/read，避免重复条目。

## 4. 几个典型工具

### `web_fetch`

作用：读取一个已经明确给定的 URL 内容。

### `http_call`

作用：向任意 HTTP 接口发请求。

### `knowledge_search`

作用：搜索共享知识条目，而不是搜索用户记忆。

适合主动先查的场景：

- 项目历史决策
- 架构说明
- 调试结论
- 操作清单
- 研究笔记
- 团队共识
- 可复用工作流

如果问题更像“这件事以前有没有整理过文档或结论”，优先先用 `knowledge_search`。

### `memory_search`

作用：搜索当前用户可见的 memory Markdown 文件。

适合主动先查的场景：

- 用户偏好
- 用户长期限制
- 与当前用户有关的历史决策
- 近期用户上下文

### `skill_list` / `skill_detail` / `skill_use`

作用：

- `skill_list`: 查看当前可见 Skill
- `skill_detail`: 查看某个 Skill 的参数、角色限制和预览
- `skill_use`: 读取并渲染一个 Skill 的完整内容

### `sessions_*`

作用：在允许范围内查看、发送或在别的 session 上下文里运行。

### `schedule_*`

作用：为当前用户和当前 agent 创建、查看、启停、删除或触发计划任务。

## 5. MCP 工具

MCP 工具不是内置代码直接实现，而是由外部 MCP Server 暴露后，被注册进 agent 工具表。

常见示例：

| MCP Server | 工具 | 说明 |
|------------|------|------|
| `one-search` | `one_search`, `one_scrape`, `one_map`, `one_extract` | 搜索、抓取、URL 发现 |
| `exa` | `web_search_exa`, `web_fetch_exa` | 高质量 AI 搜索 |
| `playwright` | `browser_*` | 浏览器自动化 |

## 6. Plugin 工具

如果某个 WS 插件注册了 `tool` capability，它也会像普通工具一样被注入到 agent 工具表。

仓库中的最小示例：

- `plugins/echo-tool`

## 7. 权限与可见性

工具是否可用，不只取决于工具是否存在，还取决于：

- agent 角色
- agent 授权工具清单
- session 工具策略
- backend endpoint 的 `allowed_roles`
- MCP 是否初始化成功
- 插件是否成功注册

## 8. 一条重要说明

如果历史资料里提到 `web_search` 是内置固定工具，请以当前实现为准：搜索能力更常见地来自 MCP，而不是内置固定搜索工具。

## 9. 进一步阅读

- [Skill 与 MCP](skills-and-mcp.md)
- [插件开发](plugin-development.md)
