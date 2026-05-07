# Skill 与 MCP

Skill 和 MCP 在 EvoDuck 里是两条不同的扩展线：

- Skill 负责按需注入知识、流程、使用时机和工具编排说明
- MCP 负责接入外部工具和执行能力

Skill 是普通 Markdown 指令包，不是函数模板。需要强类型参数、网络调用、凭据处理或实际执行动作时，应优先做 Tool、MCP server 或插件。

## 1. Skill 从哪里加载

当前代码只从 EvoDuck 自有路径加载 Skill：

1. `agents/<agent-id>/skills/<skill-name>/SKILL.md`
2. `shared/skills/<skill-name>/SKILL.md`

也就是说：

- Agent 可以有自己的私有 Skill
- 也可以共享一套全局 Skill
- 当前暂不加载 `.claude/skills`、`.opencode/skills` 或 `.agents/skills` 等兼容路径

## 2. Skill 最小结构

```markdown
---
name: order-query
description: Use when the user asks about order progress, shipping status, or package delivery.
license: MIT
compatibility: evoduck
metadata:
  evoduck:
    role: customer
    tags: [order, support]
---

# Order Query

## When To Use

Use when the user asks about order progress, shipping status, or package delivery.

## Instructions

1. Clarify the order identifier if it is missing.
2. Use the available order lookup tool or backend API only when authorized.
3. Explain the current status and the next expected event.
```

常用 frontmatter：

- `name`: 必填，kebab-case，必须和目录名一致
- `description`: 必填，用于判断何时使用该 Skill
- `license`: 可选，许可证标记
- `compatibility`: 可选，例如 `evoduck`
- `metadata`: 可选，扩展信息
- `metadata.evoduck.role`: 可选，角色限制
- `metadata.evoduck.tags`: 可选，分类标签

旧字段 `requires.role`、顶层 `tags` 和 `parameters` 已废弃。`parameters` 不再参与运行时渲染。

## 3. Skill 包结构

最小结构：

```text
<skill-name>/
  SKILL.md
```

推荐结构：

```text
<skill-name>/
  SKILL.md
  README.md
  LICENSE
  skill.json
  examples/
  templates/
  scripts/
  assets/
  tests/
```

运行时默认只加载 `SKILL.md`。附属文件不会自动注入 prompt，Skill 可以在正文中指导 agent 按需读取它们。

## 4. BaseDir

`SKILL.md` 正文可以使用 `{baseDir}` 引用当前 Skill 目录：

```markdown
Before writing the final report, inspect `{baseDir}/examples/report.md`.

Use `{baseDir}/templates/changelog.md` as the output skeleton when the user asks for a changelog.
```

`{baseDir}` 只表示当前 Skill 目录，不是通用模板变量系统。

## 5. skill.json

`skill.json` 是可选分发清单，用于安装、校验、打包和后续更新。

示例：

```json
{
  "schemaVersion": "1.0",
  "name": "git-release",
  "version": "1.0.0",
  "description": "Create consistent releases and changelogs from repository history.",
  "license": "MIT",
  "compatibility": ["evoduck"],
  "entry": "SKILL.md",
  "files": [
    "SKILL.md",
    "README.md",
    "LICENSE",
    "examples/**",
    "templates/**"
  ]
}
```

职责划分：

- `SKILL.md` frontmatter：运行时发现和模型使用
- `skill.json`：安装、分发、版本、来源、文件列表和打包元数据

## 6. 共享 Skill 目录配置

```yaml
shared:
  skills_dir: ./shared/skills
```

如果不配置，程序会使用运行根目录下的 `shared/skills`。

## 7. Skills CLI

当前支持的安装来源包括：

- 本地 skill 目录
- 本地 `.zip` 包
- git 或仓库地址

安装到 shared：

```bash
evoduck skills install ./path/to/skill --scope shared
evoduck skills install ./skill.zip --scope shared
evoduck skills install https://github.com/org/repo.git --scope shared --path skills/git-release --ref v1.0.0
```

安装到指定 agent：

```bash
evoduck skills install ./path/to/skill --scope agent --agent admin-bot
```

覆盖已有同名 Skill：

```bash
evoduck skills install ./path/to/skill --scope shared --force
```

查看和校验：

```bash
evoduck skills list --scope shared
evoduck skills detail git-release --scope shared
evoduck skills verify git-release --scope shared
```

删除：

```bash
evoduck skills remove git-release --scope shared
```

打包：

```bash
evoduck skills pack ./skills/git-release
evoduck skills pack ./skills/git-release --output ./git-release.zip
```

## 8. 导入外部 Skill

一个实用的第三方 Skill 使用流程通常是：

1. 找到 Skill 来源，例如仓库地址、本地目录或 zip 包。
2. 复制这个来源地址或路径。
3. 让 agent 先检查这个 Skill，再安装到 EvoDuck。
4. 如果这个 Skill 原本是为其他 agent 生态编写的，让 agent 在安装前或安装后按 EvoDuck 的 `SKILL.md` 规范做适配。
5. 最后用 `evoduck skills detail <skill-name>` 和 `evoduck skills verify <skill-name>` 确认结果。

可以直接对 agent 这样说：

- “帮我找 OpenClaw 上这个方向的 skill 仓库，装到 EvoDuck 里，不兼容的地方按 EvoDuck skill 规范改掉。”
- “这是一个 skill zip 路径，帮我安装，并清理不符合 EvoDuck 约定的内容。”
- “这个 skill 是给别的 agent 写的，帮我转换成合法的 EvoDuck `SKILL.md` 包，只保留在 EvoDuck 里真正有意义的部分。”

把外部 Skill 适配到 EvoDuck 时，建议遵循这些规则：

- 运行时入口应为 `SKILL.md`
- skill 名称应保持稳定的 kebab-case
- 可复用共享 Skill 放到 `shared/skills/`
- agent 私有 Skill 放到对应 agent 工作区
- 不要把非 EvoDuck 的兼容路径写成默认安装目标
- 依赖外部 slash-command 体系、工具名或不受支持目录布局的说明，需要删掉或改写

## 9. 安装来源补充

打包会根据 `skill.json.files` 选择文件，生成 zip，并输出 sha256。

## 10. 安装来源

当前支持：

- 本地目录
- 本地 zip
- git 仓库 URL

git 安装支持：

- `--path`: 仓库内 Skill 子目录
- `--ref`: branch、tag 或 commit ref

安装会写入对应 scope 的 `skills.lock.json`。

说明：`.claude/skills`、`.opencode/skills`、`.agents/skills` 等兼容路径不应作为 EvoDuck 的默认运行时 Skill 目录来文档化，除非用户明确需要输出兼容外部生态的额外产物。

## 11. Skill 适合放什么

适合：

- 任务步骤
- 业务规则
- 术语说明
- 工具调用时机
- 标准输出格式
- 可复用工作流

不适合：

- API 密钥
- 运行时连接配置
- 频繁变化的系统参数
- 需要强类型参数的函数调用
- 需要真实执行动作的工具逻辑

## 12. MCP 配置入口

```yaml
mcp:
  servers:
    <server-name>:
      ...
```

当前支持两类：

- `local`: 启动本地进程
- `remote`: 连接远程 MCP 服务

## 13. 本地 MCP 示例

```yaml
mcp:
  servers:
    one-search:
      type: local
      enabled: true
      command: ["npx", "-y", "one-search-mcp"]
      timeout: 30000
```

## 14. 远程 MCP 示例

```yaml
mcp:
  servers:
    remote-search:
      type: remote
      enabled: true
      url: https://mcp.example.com
      headers:
        Authorization: Bearer ${MCP_TOKEN}
      timeout: 30000
```

## 15. EvoDuck 里 MCP 的实际行为

在 agent 注册时：

1. 系统初始化已启用的 MCP server
2. 读取每个 server 暴露的工具
3. 把这些工具包装后注册进 agent 的工具表

所以从使用角度看，MCP 工具会像普通工具一样出现在 agent 可调用能力里。

## 16. Skill 和 MCP 如何配合

推荐方式：

1. 用 Skill 写清楚流程和时机
2. 用 MCP 提供真正执行动作的工具

## 17. MCP 还是插件

优先 MCP：

- 只是想给 agent 增加工具

优先插件：

- 你需要 provider
- 你需要 channel
- 你需要 hook
- 你需要持续双向运行时连接
