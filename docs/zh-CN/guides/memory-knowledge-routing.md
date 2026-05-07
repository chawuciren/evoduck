# Memory 与 Knowledge 路由

本文档记录 EvoDuck 当前 memory、knowledge 和 file 工具的职责边界。

## 1. 当前结构

Agent 级固定文件：

- `AGENTS.md`
- `SOUL.md`
- `TOOLS.md`
- `IDENTITY.md`
- `HEARTBEAT.md`
- `BOOTSTRAP.md`

User 级固定文件：

- `USER.md`
- `MEMORY.md`
- `memory/YYYY-MM-DD.md`

共享知识：

- `knowledge/**/*.md`

## 2. 文件职责

### Agent 文件

`SOUL.md` 保存 agent identity：

- Name
- Role
- Mission
- Tone
- Boundaries

`AGENTS.md` 保存 agent operating rules：

- 如何协作
- 如何回答
- 如何决策
- 如何提问
- 其他长期有效的工作规则

其他 agent 级固定文件用于 bootstrap、identity、heartbeat 或工具使用说明。

### User `USER.md`

保存当前用户画像：

- Preferred Name
- Background
- Relationship With Agent
- Response Preferences
- Boundaries

### User `MEMORY.md`

保存当前用户相关、长期要记住的事实：

- 用户长期偏好
- 用户长期限制
- 与该用户协作有关的重要决策
- 关于该用户的持久事实

### User Daily Memory

`memory/YYYY-MM-DD.md` 保存近期上下文或当天任务相关记忆。

普通 turn 只在 `MEMORY_INVENTORY` 中列出 daily memory 元数据，不注入正文。session start 只注入今天和昨天的 daily memory 正文。

### Knowledge

`knowledge/**/*.md` 保存共享可复用知识：

- 项目知识
- 架构说明
- 研究结论
- 操作手册
- 可复用文档
- 排障记录
- 稳定工作流

## 3. 工具职责

### Memory 工具

- `memory_search`: 搜索当前用户可见的 memory Markdown 文件。
- `memory_read`: 读取 memory Markdown 文件或行范围。

Memory 工具不负责写入。写入和编辑通过 `file_write` / `file_edit` 完成。

### Knowledge 工具

- `knowledge_tree`: 查看知识库结构和条目元数据。
- `knowledge_search`: 搜索共享 knowledge Markdown 文件。
- `knowledge_read`: 读取 knowledge Markdown 文件或行范围。

Knowledge 工具不负责写入、移动或删除。创建和编辑通过 file 工具完成。

### File 工具

- `file_write`: 完整创建或完整覆盖文件。
- `file_edit`: append、prepend、replace_text、insert_before、insert_after、replace_between。
- `file_patch`: 独立补丁工具。

## 4. 路由规则

写用户画像、偏好、长期事实：

- 优先写当前用户 `USER.md` 或 `MEMORY.md`。
- 用 `memory_search` / `memory_read` 先查现有内容。
- 用 `file_edit` 更新已有内容，避免重复追加。

写近期用户上下文：

- 写 `memory/YYYY-MM-DD.md`。
- 只保存近期内可能复用的上下文，不保存闲聊和一次性工具过程。

写共享项目知识：

- 优先 `knowledge_search` / `knowledge_read` 查重。
- 更新已有条目优先于创建新条目。
- 新条目使用稳定路径，例如 `knowledge/decisions/`、`knowledge/architecture/`、`knowledge/debugging/`、`knowledge/workflows/`、`knowledge/checklists/`、`knowledge/research/`、`knowledge/product/`。

## 5. 群聊与后台任务

群聊或共享 channel session 中，private user memory 只绑定当前发言用户：

- gateway 写入 `actor_user_id` metadata。
- prompt 和 Active Memory 优先使用 `actor_user_id`。
- 不扫描群内其他用户 memory。

后台整理任务使用 ephemeral `experience-curator` runtime：

- 一小时 memory curation。
- 一天 experience curation。
- pre-compaction curation。

这些调用不写入 session manager，不生成持久后台 session，并设置 `ephemeral=true`、`memory_policy=ignore`。
