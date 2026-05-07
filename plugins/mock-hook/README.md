# mock-hook

Minimal hook plugin for `before_tool_call`, `after_tool_call`, and `after_llm_complete`.

## Behavior

- Registers one `hook` capability for `before_tool_call`, `after_tool_call`, and `after_llm_complete`
- Writes the incoming hook payload to `MOCK_HOOK_EVENT_FILE`
- Can block a tool call when `MOCK_HOOK_BLOCK_TOOL_NAME` matches the tool name
- Responds with `{ "ok": true }`
