# Channels

[English](../guides/channels.md) | [简体中文](../zh-CN/guides/channels.md)

This guide covers the channel integrations and channel CLI entry points implemented today.

## 1. Supported Channel Types

Most users interact with:

- `webchat`: built-in web entry point
- `weixin`: Weixin personal account channel
- `wecom`: WeCom AI Bot channel

WebSocket plugins can also register channel bridges, but those are plugin extensions rather than built-in channels.

## 2. `webchat`

`webchat` is built into the gateway.

- The default template includes it.
- Setup Wizard treats it as built-in.
- It does not require external credentials.

Minimal config:

```yaml
channels:
  webchat:
    type: webchat
    role: admin
    agent: admin-bot
```

## 3. Setup Wizard Channel Step

The setup wizard asks whether to configure extra channels near the end of first-run setup.

Current built-in optional channels:

- `weixin`
- `wecom`

You can skip this step and configure channels later with `evoduck channel add` or by editing config manually.

## 4. Channel CLI

List supported channel types:

```bash
evoduck channel types
```

Show details:

```bash
evoduck channel info weixin
evoduck channel info wecom
```

List configured channels:

```bash
evoduck channel list
```

Add channels:

```bash
evoduck channel add
evoduck channel add weixin
evoduck channel add wecom
```

Remove a channel:

```bash
evoduck channel remove --channel-id weixin-cs
```

Reconnect a channel by restarting the service:

```bash
evoduck channel reconnect --channel-id wecom-sales
```

## 5. `weixin`

`weixin` uses a QR-code login flow and stores:

- `token`
- `user_id`
- `api_base_url`

Example:

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

## 6. `wecom`

`wecom` uses WeCom AI Bot credentials:

- `bot_id`
- `secret`

Example:

```yaml
channels:
  wecom-sales:
    type: wecom
    agent: admin-bot
    role: employee
    bot_id: ${WECOM_BOT_ID}
    secret: ${WECOM_SECRET}
```

## 7. Agent Binding

Every channel should bind to an agent:

```yaml
agent: admin-bot
```

If a channel has no explicit agent, routing falls back to `default_agent` when possible.

## 8. Role Boundary

Channel role controls the role boundary used by channel traffic. Common roles:

- `admin`
- `employee`
- `customer`

Use the narrowest role that matches the channel audience.

## 9. Operational Notes

- `webchat` is best for first-run validation.
- Use `channel list` after edits to verify config.
- Use `channel reconnect` after changing channel credentials.
- Service mode users usually need to restart the service after config changes.
