# Skills and MCP

[English](../guides/skills-and-mcp.md) | [简体中文](../zh-CN/guides/skills-and-mcp.md)

This guide explains how EvoDuck loads Skills and how MCP fits into the extension model.

## 1. Skills

Skills package reusable operational knowledge and behavior. A Skill is usually a directory containing `SKILL.md` and optional metadata.

Current EvoDuck loading locations:

- shared Skills: `shared/skills/<skill-name>/SKILL.md`
- agent-specific Skills: `agents/<agent-id>/skills/<skill-name>/SKILL.md`

Built-in Skills are written during first-run initialization. EvoDuck loads `SKILL.md` as the runtime entrypoint by default. Supporting files such as examples, templates, or assets are optional and should only be referenced when the skill instructions need them.

## 2. Skill Scope

Typical scopes:

- shared: available to multiple agents
- agent: available to one agent

Use shared Skills for reusable team or project practices. Use agent Skills for role-specific behavior.

## 3. Installing and Managing Skills

Supported install sources include:

- local skill directory
- local `.zip` package
- git or repository source

Install a Skill from a local directory:

```bash
evoduck skills install ./path/to/skill
```

Install a Skill from a local zip package:

```bash
evoduck skills install ./path/to/skill.zip
```

List Skills:

```bash
evoduck skills list
```

Show details:

```bash
evoduck skills detail skill-name
```

Verify a Skill package:

```bash
evoduck skills verify skill-name
```

Pack a Skill:

```bash
evoduck skills pack ./path/to/skill
```

## 4. Importing an External Skill

A practical user flow for third-party skills is:

1. Find a skill source such as a repository, local folder, or zip package.
2. Copy the source address or path.
3. Ask an agent to inspect the skill and install it into EvoDuck.
4. If the source was written for another agent ecosystem, ask the agent to adapt it to EvoDuck's `SKILL.md`-based format before or after installation.
5. Verify the result with `evoduck skills detail <skill-name>` and `evoduck skills verify <skill-name>`.

Example requests you can give an agent:

- "Find the OpenClaw skill repo for X, install it into EvoDuck, and adapt it to EvoDuck skill format if needed."
- "Here is a skill zip path. Install it and clean up anything that does not fit EvoDuck's skill conventions."
- "This skill was written for another agent. Convert it into a valid EvoDuck `SKILL.md` package and keep only the parts that make sense in EvoDuck."

When adapting an external skill for EvoDuck, keep these rules in mind:

- the runtime entrypoint should be `SKILL.md`
- the skill name should be stable kebab-case
- shared reusable skills belong under `shared/skills/`
- agent-specific skills belong under the target agent workspace
- avoid documenting non-EvoDuck compatibility paths as the default install target
- remove or rewrite instructions that depend on foreign slash-command systems, tool names, or unsupported compatibility layouts

## 5. Skill Content Guidance

A good Skill should:

- describe when it should be used
- provide specific workflows
- include constraints and safety boundaries
- avoid storing secrets
- stay focused on one reusable capability

## 6. MCP

MCP connects agents to external tools and services through Model Context Protocol servers.

Config entry:

```yaml
mcp:
  servers: {}
```

Prefer MCP when you want to reuse existing external tool ecosystems.

## 7. Skill vs MCP vs Plugin

Use Skills when you need reusable instructions, workflows, or project knowledge.

Use MCP when you need external tools exposed through a standard protocol.

Use plugins when you need EvoDuck-native extensions such as:

- custom model providers
- custom channels
- runtime hooks
- lightweight local tool adapters

## 8. Boundaries

- Skills should not require long-running external processes.
- MCP servers own their own external connectivity.
- Plugins connect to EvoDuck's plugin WebSocket server.
- Secrets should come from environment variables or secure config, not committed Skill content.
- OpenCode or Claude Code compatibility paths are not EvoDuck's standard runtime skill locations.
