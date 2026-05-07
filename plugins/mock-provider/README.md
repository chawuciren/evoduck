# Mock Provider Demo Plugin

最小 provider demo plugin，用于验证 `provider.chat` 和 `provider.event` 流式事件链路。

当前行为：

- 注册一个 provider：`mock-provider`
- 注册一个 model：`mock-model`
- 收到 `provider.chat` 后按顺序返回：
  - `content`: `mock provider says hello`
  - 可选 `tool_calls`
  - `stop`
  - `response(provider.chat)`

支持的测试参数：

- `delay_ms`: 延迟返回，用于测试 timeout/cancel
- `include_tool_calls`: 返回一组最小 tool calls
