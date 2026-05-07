# Memory and Knowledge Routing

[English](../guides/memory-knowledge-routing.md) | [简体中文](../zh-CN/guides/memory-knowledge-routing.md)

This guide explains where information should be stored.

## 1. Memory

Memory is for durable context about users, preferences, repeated interactions, and agent-local operating context.

Use Memory for:

- stable user preferences
- recurring user facts
- personal workflow context
- long-running relationship context

Do not use Memory for shared project policy that should be visible to multiple users or agents.

## 2. Knowledge

Knowledge is for reusable shared information.

Use Knowledge for:

- project decisions
- operating procedures
- research notes
- product facts
- team-wide policies
- reusable troubleshooting notes

## 3. Files

Files are for concrete workspace artifacts.

Use files for:

- source code
- config files
- markdown documents
- generated outputs
- explicit user-provided artifacts

## 4. Routing Rule of Thumb

Ask:

1. Is this about a specific user? Use Memory.
2. Is this reusable across users or agents? Use Knowledge.
3. Is this an artifact that should exist in the workspace? Use Files.
4. Is this temporary conversation context? Keep it in the session.

## 5. Avoiding Duplication

Before writing new shared information, search Knowledge first. Update existing entries when possible.

Before storing user-specific information, check whether it is stable enough to persist.

## 6. Privacy Boundary

Do not store secrets, raw credentials, or unnecessary personally identifiable information in Memory or Knowledge.
