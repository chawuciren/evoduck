# Echo Tool Demo Plugin

这是一个最小可运行的本地 demo plugin，用来验证 EvoDuck 当前的 `plugin` 协议和 `tool.execute` 调用链。

它会注册一个工具：

- `echo_tool`

参数：

- `text`: 必填，要回显的文本
- `prefix`: 可选，回显前缀
- `delay_ms`: 可选，延迟多少毫秒后返回，用于测试 timeout/cancel
- `fail`: 可选，传 `true` 时强制返回错误

返回示例：

```text
[demo] hello world
```

## 运行方式

当前它设计为被 EvoDuck 主程序拉起，不需要手工传参。

主程序会自动注入：

- `EVODUCK_PLUGIN_ID`
- `EVODUCK_PLUGIN_TOKEN`
- `EVODUCK_WS_URL`

## 示例配置

可以在 `evoduck.yaml` 中加入：

```yaml
plugins:
  ws_server:
    host: "127.0.0.1"
    port: 19000

  plugins:
    echo-tool:
      enabled: true
      type: "local"
      command: ["go", "run", "./plugins/echo-tool"]
      restart: "never"
      capabilities:
        allow: ["tool"]
```

## 用途

- 验证 plugin 注册是否成功
- 验证 `tool.execute` 请求/响应链路
- 作为后续 `mock-provider` 和更复杂 plugin 的参考实现
