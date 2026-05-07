package profile

func DefaultHourlyMemoryCurationPrompt() string {
	return `Run hourly memory_curation. Curate recent ordinary user sessions into accurate memory artifacts.

**Core Objective:**
- Capture session continuity in daily logs.
- **CLASSIFY AND ROUTE** durable information to the **CORRECT** long-lived memory file based on its nature.
- Treat all user memory files and source-agent bootstrap files as **equal destinations** for their respective data types.
- Do not leave meaningful durable information trapped in daily logs.

**Scope & Input:**
- Analyze recent user sessions and existing memory files.
- Curate one source agent/user pair at a time.
- All curated artifacts live under the user's '.evoduck' data directory.
- File-tool paths are rooted at that global '.evoduck' data directory.
- Do not write relative to the current workspace.
- Do not prefix paths with 'data/' if file tools are already rooted at the '.evoduck' data directory.
- For source-agent user-level and agent-level artifacts, use file tools with explicit paths.
- Do not use memory_write or memory_edit for arbitrary source-agent artifacts because those tools route by the current curator agent namespace rather than the source-agent namespace.
- Use memory_* tools only for the curator's own namespace when explicitly needed.
- **ALWAYS read existing target files before writing** to consolidate and avoid duplicates.

**Directory Layout:**
- User-level artifacts live under 'users/<source_agent_id>_user_<user_id>/':
  - 'USER.md'
  - 'MEMORY.md'
  - 'memory/YYYY-MM-DD.md'
- Agent-level bootstrap artifacts live under 'agents/<source_agent_id>/':
  - 'AGENTS.md'
  - 'SOUL.md'
  - 'TOOLS.md'
  - 'IDENTITY.md'
  - 'HEARTBEAT.md'
  - 'BOOTSTRAP.md'

User-side files and their roles:
- 'memory/YYYY-MM-DD.md'
- 'MEMORY.md'
- 'USER.md'

Source-agent bootstrap files and their roles:
- 'AGENTS.md'
- 'SOUL.md'
- 'TOOLS.md'
- 'IDENTITY.md'
- 'HEARTBEAT.md'
- 'BOOTSTRAP.md'

**File Roles & Triggers (Equal Importance):**

1. **users/<source_agent_id>_user_<user_id>/memory/YYYY-MM-DD.md** (Daily Log):
   - **Boundary:** Same-day notes, casual continuity, temporary context, research notes, tests, and other useful daily facts that may or may not outlive the day.
   - **Trigger:** Every meaningful user-facing session requires a summary here.

2. **users/<source_agent_id>_user_<user_id>/MEMORY.md** (User Long-term Memory):
   - **Boundary:** Durable facts, recurring context, preferences, constraints, decisions, collaboration rules, and important project context that should outlive the day.
   - **Trigger:** User preferences, constraints, recurring context, key project decisions, technical facts, or collaboration rules that matter beyond today.

3. **users/<source_agent_id>_user_<user_id>/USER.md** (User Profile):
   - **Boundary:** Confirmed profile information about this specific user, such as preferred name, background, relationship with the agent, response preferences, and durable boundaries.
   - **Trigger:** Confirmed profile info (name, background, response style, boundaries). Update only when explicitly confirmed or refined.

4. **agents/<source_agent_id>/AGENTS.md** (Agent Operating Rules):
   - **Boundary:** Durable operating rules for how the source agent should collaborate, behave, decide, remember, or persist information across future conversations.
   - **Trigger:** Session reveals or confirms a durable operating rule (e.g., "Always do X when Y", "Collaborate with Z tool").

5. **agents/<source_agent_id>/SOUL.md** (Agent Identity):
   - **Boundary:** Durable agent identity, such as name, role, mission, tone, and stable boundaries or taboos.
   - **Trigger:** Session reveals or confirms agent identity details (name, role, mission, tone, boundaries).

6. **agents/<source_agent_id>/TOOLS.md** (Agent Tool Rules):
   - **Boundary:** Durable tool-usage rules, tool preferences, and tool boundaries for the source agent.
   - **Trigger:** Session reveals or confirms specific tool usage patterns, preferences, or limitations.

7. **agents/<source_agent_id>/IDENTITY.md**, **HEARTBEAT.md**, **BOOTSTRAP.md**:
   - **Boundary:** Structured identity, maintenance cadence, or startup/onboarding rules for the source agent only when that role is already established by existing file content or strong direct evidence.
   - **Trigger:** Update only when the session clearly supports that specific file role. Do not invent overlapping bootstrap structure without evidence.

**Execution Rules:**
- **Equal Treatment:** Do not favor one file type over another. If information matches a file's boundary, it **MUST** be written there.
- **No "Default" Laziness:** Do not rely on 'users/<source_agent_id>_user_<user_id>/memory/YYYY-MM-DD.md' as the sole destination. If information is durable, route it to the specific user-level or agent-level file.
- **Consolidate:** Prefer updating/rewriting existing entries over appending duplicates. Replace superseded content.
- **Skip Noise:** Ignore pure system logs or unsupported inferences.
- **No Shared Knowledge:** Do not touch shared knowledge or skills during this task.
- **Silent Execution:** Do not send user-facing messages.

**Workflow:**
1. Read recent sessions.
2. Derive 'source_agent_id' and 'user_id'.
3. Read the existing target files under 'users/<source_agent_id>_user_<user_id>/' and 'agents/<source_agent_id>/' before modifying them.
4. Log the session summary to 'users/<source_agent_id>_user_<user_id>/memory/YYYY-MM-DD.md'.
5. **Scan session for durable facts.**
6. **Classify facts:**
   - Is it user fact? -> 'users/<source_agent_id>_user_<user_id>/MEMORY.md' or 'USER.md'
   - Is it agent rule? -> 'agents/<source_agent_id>/AGENTS.md'
   - Is it agent identity? -> 'agents/<source_agent_id>/SOUL.md' or 'IDENTITY.md' when that file already serves that role
   - Is it tool rule? -> 'agents/<source_agent_id>/TOOLS.md'
   - Is it maintenance/startup guidance? -> 'agents/<source_agent_id>/HEARTBEAT.md' or 'BOOTSTRAP.md' only when clearly justified
7. **Promote** facts to the correct file(s).
8. Finish with a concise internal summary of files updated.`
}

func DefaultDailyExperienceCurationPrompt() string {
	return `Run daily experience_curation. Synthesize recent completed work across sessions, then route durable patterns into the correct long-lived artifacts.

**Core Objective:**
- **Synthesize:** Merge fragmented hourly/daily notes into coherent cross-session patterns.
- **Route Equally:** Treat user memory, agent bootstrap files, shared knowledge, and shared skills as **equal destinations** based on content nature.
- **Elevate & Prune:** Promote stable conclusions to knowledge, distinct workflows to skills, and durable context to memory files. Remove noise and duplicates.

**Scope & Input:**
- Analyze recent sessions, daily logs (last 24h), and existing memory/knowledge/skill files.
- Curate one source agent/user pair at a time.
- All curated artifacts live under the user's '.evoduck' data directory.
- File-tool paths are rooted at that global '.evoduck' data directory.
- Do not write relative to the current workspace.
- Do not prefix paths with 'data/' if file tools are already rooted at the '.evoduck' data directory.
- For source-agent user-level and agent-level artifacts, use file tools with explicit paths.
- Do not use memory_write or memory_edit for arbitrary source-agent artifacts because those tools route by the current curator agent namespace rather than the source-agent namespace.
- Use memory_* tools only for the curator's own namespace when explicitly needed.
- Always read existing target files before modifying them to consolidate.

**Directory Layout:**
- User-level artifacts live under 'users/<source_agent_id>_user_<user_id>/':
  - 'USER.md'
  - 'MEMORY.md'
  - 'memory/YYYY-MM-DD.md'
- Agent-level bootstrap artifacts live under 'agents/<source_agent_id>/':
  - 'AGENTS.md'
  - 'SOUL.md'
  - 'TOOLS.md'
  - 'IDENTITY.md'
  - 'HEARTBEAT.md'
  - 'BOOTSTRAP.md'

User-side files and their roles:
- 'memory/YYYY-MM-DD.md'
- 'MEMORY.md'
- 'USER.md'

Source-agent bootstrap files and their roles:
- 'AGENTS.md'
- 'SOUL.md'
- 'TOOLS.md'
- 'IDENTITY.md'
- 'HEARTBEAT.md'
- 'BOOTSTRAP.md'

Shared artifacts:
- Shared knowledge:
- Shared skills:
- 'SKILL.md'

**File Roles & Routing Matrix (Equal Importance):**

| Target File | Boundary / Role | Trigger Condition |
|-------------|----------------|-------------------|
| 'users/<source_agent_id>_user_<user_id>/memory/YYYY-MM-DD.md' | Same-day notes, temporary context, research, testing, continuity. | Every meaningful session. Merge duplicates from hourly runs. |
| 'users/<source_agent_id>_user_<user_id>/MEMORY.md' | Durable recurring context, stable facts, preferences, constraints, decisions, project context. | Cross-day patterns, confirmed user constraints, recurring project context. |
| 'users/<source_agent_id>_user_<user_id>/USER.md' | Confirmed user profile, response preferences, durable boundaries. | Explicitly confirmed profile updates or style preferences. |
| 'agents/<source_agent_id>/AGENTS.md' | Durable operating rules, collaboration rules, persistence rules, behavioral guidance. | Session confirms a rule that should shape future agent behavior. |
| 'agents/<source_agent_id>/SOUL.md' | Durable agent identity: name, role, mission, tone, stable boundaries. | Session confirms or refines core identity or persona traits. |
| 'agents/<source_agent_id>/TOOLS.md' | Durable tool-use rules, preferences, boundaries. | Session establishes a repeatable tool workflow or limitation. |
| 'agents/<source_agent_id>/IDENTITY.md', 'HEARTBEAT.md', 'BOOTSTRAP.md' | Structured identity, maintenance cadence, or startup/onboarding rules for the source agent only when the file role already exists or strong direct evidence supports it. | Update only when clearly justified. Do not invent overlapping bootstrap structure without evidence. |
| Shared Knowledge | Stable reusable conclusions, runbooks, architecture/debugging notes, decision records. | Conclusion is validated, supported, and reusable beyond one user/day. |
| Shared Skills | Distinct repeatable workflows with clear triggers, steps, success criteria. | Workflow is distinct, bounded, and ready for reuse. |

**Execution Rules:**
- **No Default Bias:** Do not favor memory over knowledge or agent files. Route strictly by content nature.
- **Daily Synthesis First:** Review the last 24h of logs. Merge overlapping notes, extract recurring themes, and discard noise before promoting.
- **Safe Knowledge Handling:** Read existing entries first. Update if a better home exists; create only if truly new.
- **Safe Skill Handling:** Inspect existing skills first. Update a close existing skill instead of creating an overlapping one. Create or revise 'SKILL.md' only when a **complete, distinct workflow** is identified. When a skill is created or updated, reload skills with 'system_reload' and verify with 'skill_detail'.
- **Consolidate Ruthlessly:** Prefer rewriting/merging over appending. Replace superseded content.
- **Silent Execution:** Do not send user-facing messages.

**Workflow:**
1. Read recent sessions and daily logs (last 24h).
2. Derive 'source_agent_id' and 'user_id'.
3. Read the existing target files under 'users/<source_agent_id>_user_<user_id>/' and 'agents/<source_agent_id>/' before modifying them.
4. **Synthesize:** Merge duplicates, extract cross-session patterns, and prune noise.
5. **Log:** Ensure 'users/<source_agent_id>_user_<user_id>/memory/YYYY-MM-DD.md' has a clean, consolidated daily summary.
6. **Route Durable Facts:**
   - User context -> 'users/<source_agent_id>_user_<user_id>/MEMORY.md' or 'USER.md'
   - Agent context -> 'agents/<source_agent_id>/AGENTS.md', 'SOUL.md', 'TOOLS.md', and only use 'IDENTITY.md', 'HEARTBEAT.md', or 'BOOTSTRAP.md' when clearly justified
   - Reusable conclusions -> Shared Knowledge
   - Repeatable workflows -> Shared Skills
7. **Consolidate:** Merge intelligently and avoid duplicates.
8. Finish with a concise internal summary of artifacts updated, created, or pruned.`
}
