# Channel 配置

本文档只讲当前实现里的渠道接入方式和配置入口。

## 1. 当前有哪些渠道

当前代码里对用户最重要的是这三类：

- `webchat`: 内置 Web 入口
- `weixin`: 微信个人号
- `wecom`: 企业微信 AI Bot

此外，WS 插件也可以注册自己的 channel bridge，但那属于插件扩展能力，不是内置渠道。

## 2. `webchat` 的定位

`webchat` 是内置入口。

- 默认模板里已经带上它
- 首次向导里也把它视为内置项
- 它不是普通外部桥接，因此不需要你单独补一套外部凭据

最小示例：

```yaml
channels:
  webchat:
    type: webchat
    role: admin
    agent: admin-bot
```

## 3. 首次向导里的渠道配置

Setup Wizard 在最后一步会询问是否要配置额外渠道。

当前内置可选渠道主要是：

- `weixin`
- `wecom`

你可以当场跳过，后面再配。代码里的提示也是：

- 跳过后，后续再用 `evoduck channel add`
- 或直接手工编辑配置文件

## 4. 日常 CLI 入口

查看支持的渠道类型：

```powershell
.\evoduck.exe channel types
```

查看某个渠道的说明：

```powershell
.\evoduck.exe channel info weixin
.\evoduck.exe channel info wecom
```

列出当前渠道：

```powershell
.\evoduck.exe channel list
```

添加渠道：

```powershell
.\evoduck.exe channel add
.\evoduck.exe channel add weixin
.\evoduck.exe channel add wecom
```

删除渠道：

```powershell
.\evoduck.exe channel remove --channel-id weixin-cs
```

重连渠道：

```powershell
.\evoduck.exe channel reconnect --channel-id wecom-sales
```

## 5. `weixin` 配置

`weixin` 走二维码登录流程，成功后会拿到：

- `token`
- `user_id`
- `api_base_url`

配置示例：

```yaml
channels:
  weixin-cs:
    type: weixin
    name: 客服微信
    token: your-token
    role: customer
    agent: customer-service
    user_id: wang_xiaoming
    api_base_url: https://your-weixin-api.example.com
```

## 6. `wecom` 配置

`wecom` 当前关键字段是：

- `bot_id`
- `secret`

配置示例：

```yaml
channels:
  wecom-sales:
    type: wecom
    name: 销售机器人
    role: employee
    agent: sales-bot
    bot_id: your-bot-id
    secret: ${WECOM_SECRET}
```

## 7. 通用字段

所有 channel 通常都要关心：

- `type`
- `name`
- `role`
- `agent`

校验层还会检查：

- `role` 必须是 `admin` / `employee` / `customer`
- `agent` 如果填写，必须存在于 `agents` 配置中

## 8. 命名建议

建议保留渠道前缀，便于排查：

- `weixin-cs`
- `weixin-hr`
- `wecom-sales`

## 9. 一条使用建议

先用默认 `webchat` 跑通，再加 `weixin` 或 `wecom`。

这样能先把模型、Agent、工作区和整体运行链路确认清楚，再处理外部渠道接入问题。

## 10. 进一步阅读

- [Weixin 渠道细节](../features/weixin-channel.md)
- [首次引导与日常使用](usage.md)
- [配置结构](configuration.md)
