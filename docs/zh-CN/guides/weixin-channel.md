# Weixin 渠道细节

本文档聚焦 `weixin` 与 `wecom` 的差异，以及当前 CLI 与配置方式。

## 1. 当前定位

- `weixin`: 微信个人号，一个机器人通常服务一个固定用户
- `wecom`: 企业微信 AI Bot，适合企业内部接入

## 2. 当前 CLI 入口

当前版本统一通过 `channel` 子命令管理渠道。

```powershell
.\evoduck.exe channel types
.\evoduck.exe channel info weixin
.\evoduck.exe channel add weixin
.\evoduck.exe channel list
```

## 3. `weixin` 的实际接入方式

`weixin` 当前走二维码登录流程。

登录成功后，系统会拿到并保存这些字段：

- `token`
- `user_id`
- `api_base_url`

## 4. `wecom` 的实际接入方式

`wecom` 当前关键字段是：

- `bot_id`
- `secret`

## 5. 配置示例

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

  wecom-sales:
    type: wecom
    name: 销售培训机器人
    role: employee
    agent: sales-training
    bot_id: your-bot-id
    secret: ${WECOM_SECRET}
```

## 6. 两者差异

| 维度 | `weixin` | `wecom` |
|------|----------|---------|
| 典型场景 | 个人号登录与固定用户服务 | 企业内部应用或机器人接入 |
| 关键字段 | `token`、`user_id`、`api_base_url` | `bot_id`、`secret` |
| CLI 体验 | 二维码登录流程 | 常规参数输入流程 |

## 7. 命名建议

- `weixin-cs`
- `weixin-hr`
- `wecom-sales`

## 8. 进一步阅读

- [Channel 配置](channels.md)
- [首次引导与日常使用](usage.md)
