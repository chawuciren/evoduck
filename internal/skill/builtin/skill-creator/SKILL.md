---
name: skill-creator
description: Guide for creating, updating, validating, and packaging EvoDuck skills.
license: MIT
compatibility: evoduck
metadata:
  evoduck:
    role: admin
    tags: [meta, skill-creation, workflow]
---

# Skill Creator Guide

Use this guide when transforming a repeated workflow, domain procedure, or accumulated operating knowledge into a reusable EvoDuck skill. Prefer updating an existing close skill over creating a new overlapping one.

## What A Skill Is

A skill is a directory package with `SKILL.md` as its runtime entrypoint. It is an instruction package, not a function, not a tool, and not a template engine.

Use a skill to teach the agent:

- When a workflow applies
- What steps to follow
- Which tools to use and when
- What decisions to make when context varies
- What output shape or quality bar to satisfy

Use a Tool, MCP server, or plugin instead when you need executable behavior, strong typed inputs, network calls, credentials, or persistent runtime services.

## When To Create Or Update

Create or update a skill when you notice:

- The same workflow appears across multiple sessions
- The workflow has stable steps and clear trigger conditions
- The agent repeatedly needs the same tool-use playbook
- A correction or hard-won lesson should guide future work
- Existing `AGENTS.md` or knowledge content has grown into a procedure

Do not create a skill for:

- One-off tasks
- Generic behavior such as “be helpful” or “use best practices”
- Highly volatile runtime configuration
- Secrets, API keys, tokens, or passwords
- Logic that belongs in a tool or MCP server

Before writing anything, choose one action:

- Skip when the pattern is weak, speculative, or already covered
- Update when a nearby skill already owns the workflow
- Create when the workflow is distinct, reusable, and concrete

## Current EvoDuck Skill Locations

EvoDuck currently loads skills only from its own paths:

```text
agents/<agent-id>/skills/<skill-name>/SKILL.md
shared/skills/<skill-name>/SKILL.md
```

Do not write OpenCode, Claude Code, or AgentSkills compatibility paths unless the user explicitly asks for external compatibility artifacts. EvoDuck does not load those paths in the current design.

## Skill Package Layout

Minimum package:

```text
<skill-name>/
  SKILL.md
```

Recommended package:

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

Directory meanings:

- `SKILL.md`: required runtime entrypoint
- `README.md`: optional human-facing explanation
- `LICENSE`: optional license text
- `skill.json`: optional distribution and install manifest
- `examples/`: optional example inputs, outputs, or workflows
- `templates/`: optional reusable document skeletons
- `scripts/`: optional helper scripts; never assume they can run without user/tool approval
- `assets/`: optional static files, schemas, or images
- `tests/`: optional validation examples or test notes

Only `SKILL.md` is loaded by default. Supporting files should be read only when the skill instructions call for them.

## SKILL.md Format

Use YAML frontmatter followed by Markdown instructions:

```markdown
---
name: skill-name
description: Clear one-line summary that says when to use this skill.
license: MIT
compatibility: evoduck
metadata:
  evoduck:
    role: admin
    tags: [tag1, tag2]
---

# Skill Title

Use this skill when ...

## Instructions

1. Inspect the current context.
2. Follow the workflow.
3. Verify the result.
```

Frontmatter fields:

| Field | Required | Description |
| --- | --- | --- |
| `name` | Yes | Unique kebab-case skill name. Must match the directory name. |
| `description` | Yes | One-line trigger description used for discovery. |
| `license` | No | License identifier, such as `MIT`, `Apache-2.0`, or `Proprietary`. |
| `compatibility` | No | Compatibility marker such as `evoduck`. String or string array. |
| `metadata` | No | Extension metadata. EvoDuck-specific fields live under `metadata.evoduck`. |

EvoDuck metadata fields:

| Field | Description |
| --- | --- |
| `metadata.evoduck.role` | Optional role restriction: `admin`, `employee`, or `customer`. |
| `metadata.evoduck.tags` | Optional categorization tags. |
| `metadata.evoduck.userInvocable` | Reserved for future slash/manual invocation control. |
| `metadata.evoduck.modelInvocable` | Reserved for future model invocation control. |

## Name Rules

Skill names must satisfy:

```text
^[a-z0-9]+(-[a-z0-9]+)*$
```

Rules:

- Use lowercase letters, digits, and single hyphens only
- Do not start or end with a hyphen
- Do not use consecutive hyphens
- Keep names stable and specific
- Ensure `frontmatter.name` equals the directory name

## No Parameters Or Templates

Do not use `parameters` in skill frontmatter. Do not write Go template-style double-brace placeholders in `SKILL.md`.

Skills are plain Markdown instruction packages. If the user provides a target, version, risk level, module name, or other context, instruct the agent to interpret that natural-language context and ask clarifying questions when needed.

Preferred pattern:

```markdown
When the user specifies a target environment, adapt the checklist to that environment. If the target is unclear, ask one clarifying question before proceeding.
```

Avoid this deprecated pattern:

```markdown
Deploy to <target-placeholder>.
```

If strong typed inputs or executable behavior are required, create a Tool, MCP server, or plugin instead of a parameterized skill.

## Supporting Files And BaseDir

Use supporting files for examples, templates, or reference material that should not always be loaded into the prompt.

In skill instructions, refer to files with `{baseDir}`:

```markdown
Before writing the final report, inspect `{baseDir}/examples/report.md`.

Use `{baseDir}/templates/changelog.md` as the output skeleton when the user asks for a changelog.
```

`{baseDir}` is a path placeholder for the current skill directory. It is not a general template variable system.

## Optional skill.json

Use `skill.json` when preparing a skill for installation or distribution:

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

Use `SKILL.md` frontmatter for runtime discovery. Use `skill.json` for install, version, source, file-list, and distribution metadata.

## Creation Workflow

### Step 1: Inspect Existing Skills

Use `skill_list` first. Use `skill_detail` on likely matches. Update an existing skill when the trigger, workflow, or output criteria overlap.

Check:

- Does an existing skill already cover the same intent?
- Would this fit as a section in an existing skill?
- Are the tool steps and decision points mostly the same?
- Would two similar skills confuse future agents?

### Step 2: Draft The Skill

When creating a new skill, write to one of the current EvoDuck paths:

```text
agents/<agent-id>/skills/<skill-name>/SKILL.md
shared/skills/<skill-name>/SKILL.md
```

Use shared skills only when the workflow should be available across agents and the current role has permission to manage shared runtime state.

### Step 3: Self-Check

Before writing or updating:

- The skill name is kebab-case and matches the directory name
- The description says when to use the skill
- The body includes trigger conditions, workflow steps, decision points, success criteria, and boundaries
- The skill does not duplicate tool descriptions or generic agent rules
- The skill does not contain secrets
- The skill does not use `parameters` or template-style double-brace placeholders
- Supporting examples or templates live in supporting files when they are long

### Step 4: Reload And Verify

After creating or editing a skill:

1. Call `system_reload` with `scope="skills"` if available
2. Use `skill_detail` to verify the skill is visible
3. Confirm metadata, role, tags, license, compatibility, and content preview
4. Confirm the skill does not overlap confusingly with another skill

Skills are loaded into an in-memory loader cache. Do not assume a new or edited `SKILL.md` is visible until skills have been reloaded.

## Example Skill

```markdown
---
name: go-project-setup
description: Initialize a Go project with a standard structure, tests, and optional CI guidance.
license: MIT
compatibility: evoduck
metadata:
  evoduck:
    role: employee
    tags: [go, setup, project-init]
---

# Go Project Setup

Use this skill when the user asks to initialize or standardize a Go project.

## Instructions

1. Confirm the module path if it is not clear from the user request.
2. Inspect the current directory before creating files.
3. Run `go mod init` only when no `go.mod` already exists.
4. Create or verify standard directories such as `cmd/`, `internal/`, and `pkg/` when they fit the project.
5. Add or update `.gitignore` with Go-specific patterns.
6. Add a basic test target or explain how to run `go test ./...`.
7. If the user asks for CI, add a GitHub Actions workflow; otherwise mention it as an optional next step.

## Success Criteria

- The project has a valid `go.mod`.
- The directory structure matches the user's intended project shape.
- The user receives clear commands for testing and next steps.
```

## Anti-Patterns

- Do not create skills that only say to be polite or careful
- Do not duplicate existing skills
- Do not encode secrets, API keys, or user-specific memory
- Do not use deprecated `parameters`
- Do not use template-style double-brace placeholders
- Do not create a skill when a Tool, MCP server, or plugin is the correct abstraction
- Do not make skills excessively long; move examples and templates to supporting files when needed
