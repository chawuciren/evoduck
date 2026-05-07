# Mock Channel Demo Plugin

最小 channel demo plugin，用于验证：

- `channel.message` 入站事件
- `channel.send` 出站请求

当前行为：

- 注册一个 channel bridge：`mock-channel`
- 启动后主动发送一条入站消息：`hello from mock channel`
- 收到 `channel.send` 时返回 `ok: true`
