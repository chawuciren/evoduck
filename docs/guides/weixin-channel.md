# Weixin Channel Details

[English](../guides/weixin-channel.md) | [简体中文](../zh-CN/guides/weixin-channel.md)

This guide summarizes the difference between `weixin` and `wecom` channel configuration.

## 1. `weixin`

`weixin` is for Weixin personal-account style integration.

It uses a QR-code login flow and stores:

- `token`
- `user_id`
- `api_base_url`

CLI setup:

```bash
evoduck channel add weixin
```

## 2. `wecom`

`wecom` is for WeCom AI Bot integration.

It uses:

- `bot_id`
- `secret`

CLI setup:

```bash
evoduck channel add wecom
```

## 3. Config Examples

Weixin:

```yaml
channels:
  weixin-cs:
    type: weixin
    agent: admin-bot
    role: admin
    token: ${WEIXIN_TOKEN}
    user_id: ${WEIXIN_USER_ID}
    api_base_url: ${WEIXIN_API_BASE_URL}
```

WeCom:

```yaml
channels:
  wecom-sales:
    type: wecom
    agent: admin-bot
    role: employee
    bot_id: ${WECOM_BOT_ID}
    secret: ${WECOM_SECRET}
```

## 4. Operational Notes

- Use `webchat` first to validate the agent.
- Use `channel list` after adding channels.
- Restart or reconnect channels after credential changes.
- Keep channel roles aligned with the target audience.
