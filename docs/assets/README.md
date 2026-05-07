# Documentation Assets

This directory stores README and documentation media assets.

## Layout

- `brand/banner.png`: EvoDuck banner image shown near the top of the README files.
- `screenshots/main-screenshot.png`: primary product screenshot shown near the top of the README files.
- `demo.gif`: future animated demo GIF. It is intentionally not referenced by the README until the final recording is ready.

## Planned Final Assets

- `brand/banner.png`: final EvoDuck banner with the duck logo.
- `screenshots/main-screenshot.png`: polished main screenshot of the web UI.
- `demo.gif`: short animated demo of chat and basic operations.

## Capture Plan

1. Start EvoDuck with a stable demo config and deterministic sample data.
2. Open the web UI at a clean desktop viewport, preferably `1440x900` or `1600x1000`.
3. Capture the main screenshot after the UI has a representative conversation, visible agent state, and no private tokens or local paths.
4. Record the demo GIF as a short 10-20 second flow: open webchat, send a message, receive a response, show a tool/skill-style action, and briefly show configuration or channel status if useful.
5. Export still screenshots as PNG and animated demos as optimized GIF or WebM. Keep README GIF size small enough for GitHub rendering.

## Media Rules

- Do not include secrets, API keys, private endpoints, customer data, or personal chat history.
- Prefer synthetic demo content.
- Use consistent viewport size and light/dark theme deliberately.
- Keep large raw recordings out of the repository; commit only optimized final assets.
