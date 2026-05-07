package profile

func DefaultHourlyMemoryCurationPrompt() string {
	return `Run hourly memory_curation.

Curate recent ordinary user sessions into accurate user memory artifacts. Keep this task lightweight, but do not be so conservative that meaningful user-facing sessions leave no trace.

Primary goal:
- Maintain user-side and source-agent Markdown memory artifacts only.
- Update daily memory by default.
- Promote clearly durable user or agent-specific information into the correct long-lived Markdown file when justified.
- Do not create or update shared knowledge or shared skills during hourly memory curation.

Scope:
- Use recent sessions and existing user memory as primary input.
- Curate one source agent/user pair at a time. Derive source_agent_id and user_id from session keys and existing users/ directories.
- Use file tools rooted at the global data directory for source-agent user memory under users/<source_agent_id>_user_<user_id>/.
- Do not use memory_write for source-agent memory because it writes the curator namespace.
- Inspect existing relevant files before writing.

User-side files and their roles:
- memory/YYYY-MM-DD.md: same-day session notes, casual continuity, temporary context, research notes, tests, and other useful daily facts that may or may not outlive the day.
- MEMORY.md: durable facts, recurring context, preferences, constraints, decisions, collaboration rules, and important project context that should outlive the day.
- USER.md: confirmed profile information about this specific user, such as preferred name, background, relationship with the agent, response preferences, and durable boundaries.

Source-agent bootstrap files and their roles:
- AGENTS.md: durable operating rules for how the source agent should collaborate, behave, decide, remember, or persist information across future conversations.
- SOUL.md: durable agent identity, such as name, role, mission, tone, and stable boundaries or taboos.
- TOOLS.md: durable tool-usage rules, tool preferences, and tool boundaries for the source agent.
- IDENTITY.md: structured durable identity details for the source agent when that file already serves that role.
- HEARTBEAT.md: durable maintenance cadence, self-check, or recurring operational commitments for the source agent when that file already serves that role.
- BOOTSTRAP.md: durable startup, initialization, or onboarding rules for the source agent when that file already serves that role.

Rules:
- Update memory/YYYY-MM-DD.md by default for each meaningful recent ordinary user-facing session, including casual chat when it helps continuity.
- Promote to MEMORY.md when information is clearly useful beyond the day even if it is not a formal user profile field.
- Update USER.md when the session confirms or refines user-specific profile information or durable preferences.
- Update AGENTS.md when the session reveals or confirms durable operating rules for the source agent.
- Update SOUL.md when the session reveals or confirms durable agent identity, role, mission, tone, or boundaries.
- Update TOOLS.md, IDENTITY.md, HEARTBEAT.md, or BOOTSTRAP.md only when the file already exists or when the session clearly supports that specific file role; do not invent overlapping bootstrap structure without evidence.
- Prefer updating, consolidating, or rewriting existing content over appending duplicates.
- Replace superseded content instead of keeping conflicting old and new versions.
- Skip writes only for pure system/tool noise, unsupported inference, or points already accurately captured in the correct file.
- Do not create or update shared knowledge during hourly memory curation.
- Do not create or update skills during hourly memory curation.
- Do not send user-facing messages.

Decision guidance:
- If something is mainly about today, keep it in memory/YYYY-MM-DD.md.
- If it should still matter next week, consider MEMORY.md, USER.md, or one of the source-agent bootstrap files.
- Put user profile facts in USER.md.
- Put durable user or project continuity in MEMORY.md.
- Put source-agent operating rules in AGENTS.md.
- Put source-agent identity in SOUL.md.
- Put source-agent tool rules in TOOLS.md.
- Use IDENTITY.md, HEARTBEAT.md, and BOOTSTRAP.md only when their scope is already established by existing file content or by strong direct evidence.

Finish with a concise internal summary listing files updated, files skipped, and why.`
}

func DefaultDailyExperienceCurationPrompt() string {
	return `Run daily experience_curation.

Curate recent completed work into user memory first, then promote stable reusable conclusions into the correct long-lived artifacts, including user-side Markdown files, source-agent bootstrap files, shared knowledge, and shared skills.

Primary goal:
- Ensure user-facing work is captured in daily memory.
- Promote durable user, agent, and project context into the correct user-side or source-agent Markdown files.
- Promote only stable shared conclusions into knowledge.
- Promote only distinct repeatable workflows into skills.

Scope:
- Inspect recent work traces, relevant memories, existing user-side Markdown files, existing source-agent bootstrap files, existing knowledge, and existing skills.
- Curate one source agent/user pair at a time. Use file tools rooted at the global data directory for source-agent user memory under users/<source_agent_id>_user_<user_id>/.
- Do not use memory_write for source-agent memory because it writes the curator namespace.
- Inspect existing relevant files before writing or creating anything.

User-side files and their roles:
- memory/YYYY-MM-DD.md: same-day session notes, temporary context, research, testing, and continuity notes.
- MEMORY.md: durable recurring context, stable facts, preferences, constraints, decisions, and project context that should outlive the day.
- USER.md: confirmed user profile information and durable response preferences or boundaries.

Source-agent bootstrap files and their roles:
- AGENTS.md: durable source-agent operating rules, collaboration rules, persistence rules, and behavioral guidance.
- SOUL.md: durable source-agent identity, name, role, mission, tone, and stable boundaries.
- TOOLS.md: durable source-agent tool-use rules, tool preferences, and tool boundaries.
- IDENTITY.md: structured durable source-agent identity details when that file already serves that role.
- HEARTBEAT.md: durable source-agent maintenance cadence, self-check, or recurring operational commitments when that file already serves that role.
- BOOTSTRAP.md: durable source-agent startup, initialization, or onboarding rules when that file already serves that role.

Shared artifacts:
- Shared knowledge: stable reusable conclusions, runbooks, architecture notes, debugging notes, decision records, and research summaries useful beyond one user or one conversation.
- Shared skills: distinct repeatable workflows with clear trigger conditions, steps, decisions, success criteria, and boundaries. Each skill is maintained through its SKILL.md entry file and must remain non-overlapping with existing skills.

Rules:
- Ensure recent ordinary user-facing sessions have daily memory coverage in memory/YYYY-MM-DD.md, including brief casual-chat notes when useful for continuity and not already captured.
- Promote stable information into the correct user-side or source-agent file instead of forcing everything into MEMORY.md.
- Update USER.md for confirmed user profile facts and durable response preferences.
- Update AGENTS.md for durable operating rules that should shape future behavior of the source agent.
- Update SOUL.md for durable agent identity or persona information.
- Update TOOLS.md for durable tool-use rules.
- Update IDENTITY.md, HEARTBEAT.md, or BOOTSTRAP.md only when the file already exists or the session clearly supports that specific file role; do not invent overlapping bootstrap structure without evidence.
- Update MEMORY.md for durable continuity that is neither merely same-day context nor a structured USER.md or source-agent bootstrap field.
- Search and read existing knowledge before writing; update an existing entry when it is the best home.
- Inspect existing skills before writing; update a close existing skill instead of creating an overlapping one.
- Use shared knowledge only for conclusions that are stable, supported, and reusable beyond one user or one day.
- Use shared skills only for workflows that are distinct, repeatable, and well-bounded.
- Prefer updating, consolidating, or rewriting existing artifacts over appending duplicates.
- Replace superseded content instead of preserving conflicting old and new versions.
- Skip low-confidence or duplicate artifacts, but do not skip daily memory solely because a session was casual or short.
- Do not send user-facing messages.

Decision guidance:
- If the value is continuity for this user or this source agent, prefer user-side or source-agent files first.
- If the value is broadly reusable across users or future tasks, consider shared knowledge or shared skills.
- A reusable conclusion belongs in knowledge.
- A reusable procedure belongs in a skill.
- Agent identity belongs in SOUL.md or, when already structured that way, IDENTITY.md.
- Agent operating rules belong in AGENTS.md.
- Agent tool rules belong in TOOLS.md.
- Agent recurring maintenance commitments belong in HEARTBEAT.md when that scope already exists.
- Agent startup or initialization rules belong in BOOTSTRAP.md when that scope already exists.
- User profile facts belong in USER.md.
- Durable cross-day context belongs in MEMORY.md.

For reusable workflows, load skill-creator, decide whether to update an existing skill or create a new one, write SKILL.md with file tools, call system_reload with scope="skills", then verify with skill_detail.

Finish with a concise internal summary listing artifacts created, artifacts updated, and candidates skipped with reasons.`
}
