# Tool 结果压缩与图片处理统一方案

本文档用于统一 EvoDuck 中 oversized tool result、截图、media 暂存、图片入站与 LLM 图片输入的处理方式。

目标有三点：

1. 超大 tool 结果不要先污染 session history，再依赖 compaction 事后补救。
2. 图片统一走 media/store 与通用工具链，不再保留 screenshot 专属 provider replay 特化。
3. 图片相关返回统一带路径与大小信息，后续是否读取内容由 LLM 自行决定调用通用工具。

## 1. 已确认决策

1. tool 结果在进入 history 前统一处理。
2. tool 返回摘要阈值使用固定值，不再按上下文占比动态计算。
3. 图片压缩阈值也使用固定值。
4. 两个阈值都放入 config。
5. 不增加额外字符数/行数并行规则。
6. 命中摘要后只保留压缩后的文本，不保留 raw fallback。
7. screenshot 保留当前 screenshot -> media/store 主流程，但去掉 provider deferred replay 特化。
8. 新增独立的 media 暂存 tool。
9. 新增独立的 image 压缩 tool。
10. 上传、截图、图片入站等路径统一支持压缩与大小返回。
11. 如果图片压缩后，相关 tool 返回文本仍超过摘要阈值，则继续按统一规则摘要。

## 2. 配置项

建议新增并统一复用：

- `ToolResultCondenseLimit`
  - 控制 tool 返回文本进入 history 前是否需要摘要。
- `ImageAutoCompressLimit`
  - 控制图片在截图、上传、入站时是否先触发自动压缩。

第一版建议都按字节大小判断，例如默认 `32 * 1024`。

## 3. 要改的核心位置

### 3.1 Tool 结果统一压缩

主要改动点：

- `internal/agent/runtime.go`

方案：

1. 在 tool result 写入 session 前统一判断大小。
2. 若 `len(tool_result_text) > ToolResultCondenseLimit`，则改写为摘要文本再写入 history。
3. 摘要格式保持简短英文，例如：

```text
[tool output condensed]
Reason: Raw tool output was condensed because it exceeded the active size limit.
Summary:
...
```

## 4. Screenshot 改动方案

### 4.1 目标

`screenshot` 不再走“截图后由 provider 隐式补图”的专属特化。

改动后：

1. `browser_screenshot` 继续截图。
2. 结果继续进入现有 media/store。
3. tool 返回补全路径与大小信息。
4. 后续如果 LLM 还要继续看图，由 LLM 显式调用通用读取/图片处理工具。

### 4.2 主要改动点

- `internal/tools/browser_tools.go`
- `internal/agent/runtime.go`
- `internal/agent/prompt.go`
- `internal/llm/provider.go`
- `internal/llm/openai.go`
- `internal/llm/openai_compatible.go`

### 4.3 返回内容

`screenshot` 返回至少包含：

- `summary`
- `media_url`
- `path`
- `original_size`
- `final_size`
- `compressed`

示例：

```text
Screenshot captured.
Path: <path>
Media URL: <url>
Original size: <bytes> bytes
Final size: <bytes> bytes
Compressed: yes|no
```

### 4.4 压缩规则

1. 截图完成后先检查文件大小。
2. 超过 `ImageAutoCompressLimit` 则先压缩。
3. 返回中同时给出原始大小和最终大小。
4. 若最终 tool 返回文本仍超过 `ToolResultCondenseLimit`，则再摘要。

### 4.5 需要去掉的特化

应移除或停用：

1. `sess.SetPendingToolReplay(...)` 驱动的 screenshot replay。
2. `PromptBuilder.buildPendingToolReplayMessage(...)` 的额外 user message 注入。
3. `RequiresDeferredToolImageReplay()` 这条 screenshot 专属 provider 补图机制。

判断依据：

1. 现在已经决定由通用工具链负责后续读图。
2. 继续保留 replay，只会让 screenshot 成为特殊公民。
3. 去掉后，截图、上传、用户图片入站都能统一成“普通 media + 普通工具 + 普通 provider 读图”。

## 5. Media 暂存 Tool

### 5.1 目标

把现有 HTTP media 暂存能力显式暴露为 tool，作为 screenshot、上传、图片处理中间结果的统一落盘出口。

### 5.2 主要改动点

- `internal/gateway/media.go`
- `internal/mediautil/store.go`
- `internal/tools/` 新增 tool

### 5.3 输入参数建议

- `path`：本地文件路径
- `data`：base64 原始字节
- `name`
- `mime_type`
- `compress`：是否允许自动压缩，默认 true
- `max_bytes`：默认使用 `ImageAutoCompressLimit`

约束：

- `path` 与 `data` 至少提供一个。

### 5.4 输出建议

返回至少包含：

- `url`
- `path`
- `mime_type`
- `original_size`
- `final_size`
- `compressed`

说明：

- 当前 media store 归一化后已经能补出真实本地路径，不只是 `/media/<id>` URL。

### 5.5 HTTP 接口补充

`/api/media/upload` 当前只有 multipart 上传。

建议补充：

1. 增加 base64 JSON 入口。
2. 增加 `compress` 参数。
3. 返回补上 `path`、`original_size`、`final_size`、`compressed`。

## 6. Image 压缩 Tool

### 6.1 目标

提供统一图片压缩能力，避免 screenshot、上传、用户入站各自实现一套。

### 6.2 主要改动点

- `internal/tools/` 新增 tool
- 如有需要，在 `internal/mediautil/` 或独立 helper 中抽公共压缩逻辑

### 6.3 输入参数建议

- `input_path`
- `output_path`（可选）
- `name`（可选）
- `max_bytes`（默认 `ImageAutoCompressLimit`）
- `quality`（可选）
- `overwrite`（可选）

### 6.4 输出建议

返回至少包含：

- `input_path`
- `output_path`
- `original_size`
- `final_size`
- `compressed`

## 7. 各图片路径统一规则

### 7.1 Screenshot

- 截图后先判大小
- 超阈值先压缩
- 再进入 media/store
- 返回路径和大小
- 返回文本超阈值再摘要

### 7.2 HTTP 上传 / WebSocket 入站

当前相关位置：

- `internal/gateway/media.go`
- `internal/gateway/websocket.go:313`
- `internal/gateway/websocket.go:510`
- `internal/gateway/gateway.go:1283`

统一方案：

1. 图片先判大小。
2. 超过 `ImageAutoCompressLimit` 则先压缩。
3. 再进入 `NormalizeOutgoingMedia(...)`。
4. 返回补上 `path`、`original_size`、`final_size`、`compressed`。

### 7.3 FileRead / FileWrite

相关位置：

- `internal/tools/file_read.go`
- `internal/tools/file_write.go`

说明：

1. `file_read` 读取图片这类二进制文件时会返回 base64，也是大 payload 风险点。
2. `file_write` 支持 `content_base64`，属于图片/二进制写回路径。
3. 因此它们也自然受 `ToolResultCondenseLimit` 约束。

## 8. 各 Channel 图片入站现状

### 8.1 WebChat / Gateway / WebSocket

当前是已经接通的主链路：

1. 前端可传 `media[]`。
2. gateway 调用 `normalizeIncomingMedia(...)`。
3. `NormalizeOutgoingMedia(...)` 会把 `path` 或 `data` 写入 media store，并补全：
   - `url`
   - `path`
   - `mime_type`
   - `file_size`
4. agent 再把这些 media 挂到 `models.Message.Media` 上。

结论：

- 已支持入站图片 -> media store -> LLM
- 当前不会自动压缩

### 8.2 WeCom

结论：

- 当前出站图片支持较完整
- 当前入站图片没有真正统一接入 `NormalizedMessage.Media`
- 因此当前不会触发统一压缩，也不会自然进入 LLM 图片输入

相关位置：

- `internal/channels/wecom/wecom.go`

### 8.3 Weixin

结论：

- 当前出站图片支持存在
- 当前入站侧基本只处理文本
- 当前没有真正统一接入 `NormalizedMessage.Media`
- 因此当前不会触发统一压缩，也不会自然进入 LLM 图片输入

相关位置：

- `internal/channels/weixin/weixin.go`

## 9. 图片最终怎么进 LLM

统一入口已经存在：

1. agent 把图片挂到 `models.Message.Media`。
2. provider 在构造请求时通过 `internal/llm/provider_helpers.go` 统一读取图片。
3. 读取来源是：
   - `media.Data`
   - `media.Path`
4. 然后各 provider 再编码成各自需要的格式。

这说明：

- provider 层本身已经具备统一读图能力
- 真正需要统一治理的是“入站阶段如何标准化、压缩、落盘”
- 不需要继续在 provider 层为 screenshot 保留特化

## 10. 分阶段实施

### 当前进度

- 已完成：阶段一、阶段二、阶段三、阶段四、阶段五

### 阶段一

状态：已完成

1. 在 `internal/agent/runtime.go` 加入统一 tool result 摘要入口。
2. 接入 `ToolResultCondenseLimit` 配置。
3. 已补充针对普通 tool result 与 screenshot 归一化路径的测试。

### 阶段二

状态：已完成

1. 已新增 `media_store` tool，并复用 `mediautil.StoreMedia(...)` 统一 path/base64 入参。
2. `/api/media/upload` 已支持 multipart 与 JSON base64 两种入口，已补充 `compress`、`max_bytes`、路径和大小返回。
3. 已接入 `ImageAutoCompressLimit` 作为上传默认阈值来源。
4. 已补充 gateway、tool、runtime 相关测试。

### 阶段三

状态：已完成

1. 已新增 `image_compress` tool，并在 `internal/mediautil/` 落地 JPEG/PNG 公共压缩 helper。
2. `media_store`、`/api/media/upload`、`browser_screenshot` 入库阶段已复用统一压缩入口。
3. `normalizeIncomingMedia(...)` 与共享 `NormalizeOutgoingMediaWithOptions(...)` 已接入 `ImageAutoCompressLimit`，覆盖 WebSocket、session outgoing、以及带本地 path/base64 的 channel media 归一化。
4. 已补充 gateway 压缩场景测试。

### 阶段四

状态：已完成

1. `browser_screenshot` 归一化后的 tool result 已补全路径、media URL、原始大小、最终大小、compressed 信息。
2. 已去掉 deferred replay / prompt 注入这条 screenshot 特化路径，保留普通 tool media 链路。

### 阶段五

状态：已完成

1. 已补齐 WeCom / Weixin 入站图片消息到 `NormalizedMessage.Media` 的映射，并补充 channel 侧回归测试。
2. gateway 在处理 channel 消息进入 agent 前，现已统一调用 `normalizeIncomingMedia(...)`，因此若 channel 已提供本地 path/base64，会自动复用统一压缩与落盘链路。
3. Weixin 现已在 channel 侧下载远端 CDN 密文，完成 AES-ECB 解密后写回 `OutgoingMedia.Data`，再进入 gateway 统一压缩/落盘链路。
4. WeCom 现已优先接收回调中的 `url` + `aeskey`，下载远端密文并完成 AES-ECB 解密后写回 `OutgoingMedia.Data`，再进入 gateway 统一压缩/落盘链路。
5. 已补充 Weixin / WeCom 远端媒体下载解密回归测试。
