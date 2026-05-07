# WebChat 用户图片上传与视觉理解方案

本文档用于规划 EvoDuck WebChat 的用户侧图片上传能力，以及图片如何进入 LLM 多模态请求链路。

## 1. 背景

当前 WebChat 已经具备以下能力：

1. AI 可以向用户返回图片、音频、视频和文件等媒体内容。
2. 网关已经具备统一的媒体上传、落盘与访问能力。
3. 各 LLM Provider 已经有 `SupportsVision` 能力标记。

当前缺失的能力是：

1. WebChat 用户端没有上传图片入口。
2. 用户上传的图片虽然可以进入消息结构，但不会被编码为 LLM 的视觉输入。
3. 现有 LLM 消息模型仍以纯文本 `Content` 为主，缺少统一的多模态消息部件表示。

因此，本次方案的目标不是单纯补一个前端上传按钮，而是打通完整链路：

`用户上传图片 -> WebChat 消息携带 media -> 运行时保留结构化图片输入 -> Provider 按目标模型协议编码 -> LLM 看图`

## 2. 目标

本阶段只实现图片理解能力，聚焦 WebChat 渠道。

具体目标：

1. WebChat 用户可以通过文件选择、拖拽、粘贴截图三种方式上传图片。
2. 前端先上传文件，再在发送消息时引用已上传媒体，避免直接通过 WebSocket 传大块 base64。
3. 用户发送的图片在会话中以结构化形式保存，而不是仅转成文本摘要。
4. OpenAI、Anthropic、Gemini 三类 Provider 可以把图片编码为对应的视觉输入格式。
5. 当模型不支持视觉时，前后端都能给出明确限制提示。

## 3. 非目标

当前阶段明确不做以下内容：

1. 不做任意文件理解平台。
2. 不做 PDF、Word、Excel、压缩包等复杂文件解析。
3. 不做音频识别、视频理解。
4. 不做跨渠道统一开放，只先落地 WebChat。
5. 不在第一阶段处理外链远程抓图能力。
6. 不引入对象存储、CDN、图片转码服务等额外基础设施。

## 4. 现状分析

### 4.1 前端现状

当前聊天前端已经可以渲染消息中的媒体内容，包括图片和文件链接。

问题在于：

1. 没有上传控件。
2. 没有附件预览区。
3. 没有待发送附件队列。
4. 没有基于模型能力决定是否允许上传图片的交互逻辑。

### 4.2 网关现状

当前网关已经存在通用媒体能力：

1. `/api/media/upload` 用于上传文件。
2. `/media/{id}` 用于访问已存储媒体。
3. `normalizeIncomingMedia` 可以把输入媒体规范化并转存。

这说明媒体存储与访问链路已经具备，本次不需要从零设计文件存储。

### 4.3 消息模型现状

当前 `models.Message` 主要以文本 `Content` 为中心。

虽然 `NormalizedMessage` 与 `OutgoingMessage` 都已经支持 `Media` 字段，但这些媒体目前主要用于：

1. 渠道展示。
2. 会话摘要文本拼接。

问题在于，图片没有进入 Provider 请求构建流程，因此模型实际上看不到图片内容。

### 4.4 Provider 现状

当前 OpenAI、Anthropic、Gemini Provider 的消息转换逻辑基本仍是文本模式：

1. OpenAI 走 chat completions 文本消息。
2. Anthropic 走 text/thinking/tool_use 块。
3. Gemini 走 text/functionCall/functionResponse parts。

缺少统一的图片消息块构造逻辑。

## 5. 总体设计

总体设计采用两段式：

1. **上传阶段**：前端先把图片传到服务端媒体存储，获得稳定的媒体引用。
2. **发送阶段**：消息发送时只携带文本和媒体引用，由运行时在发给 LLM 前按 Provider 协议把图片转成视觉输入块。

这样做的好处：

1. WebSocket 消息更轻，不需要传输大块 base64。
2. 前端上传和消息发送解耦，便于失败重试。
3. 媒体落盘后可以复用现有预览与历史展示能力。
4. Provider 适配集中在后端，前端不需要感知不同模型协议差异。

## 6. 前端设计

### 6.1 交互入口

WebChat 上传区支持三种入口：

1. 文件按钮选择图片。
2. 将图片拖拽到输入区域。
3. 直接粘贴截图到输入框。

这是成熟 WebChat 产品较常见的交互组合，覆盖桌面端主要使用习惯。

### 6.2 待发送附件区

在输入框上方或下方增加附件托盘，展示待发送图片：

1. 缩略图预览。
2. 文件名。
3. 上传状态。
4. 删除按钮。

建议状态包括：

1. `uploading`
2. `uploaded`
3. `failed`

发送按钮只有在：

1. 文本非空，或
2. 已存在至少一个上传成功的图片

时才可用。

### 6.3 上传流程

前端处理流程如下：

1. 用户选择或粘贴图片。
2. 前端先做基础校验：类型、大小、数量。
3. 使用 `multipart/form-data` 调用 `/api/media/upload`。
4. 服务端返回 `{id, name, mime_type, size, url}`。
5. 前端把返回结果加入当前待发送消息的 `media[]`。
6. 用户点击发送时，通过 WebSocket 发送文本与 `media[]`。

建议 WebSocket 消息格式：

```json
{
  "action": "chat",
  "message": "帮我看下这张图",
  "media": [
    {
      "type": "image",
      "name": "screenshot.png",
      "mime_type": "image/png",
      "url": "/media/med_xxx"
    }
  ]
}
```

### 6.4 模型能力联动

前端应根据当前会话绑定模型能力控制上传入口：

1. 模型支持视觉：显示上传入口。
2. 模型不支持视觉：禁用上传入口并提示当前模型不支持图片理解。

前端控制只用于提升体验，后端仍需兜底校验。

## 7. 后端消息模型设计

### 7.1 设计目标

后端需要把“文本 + 图片”表示为统一的结构化消息，而不是只保留字符串摘要。

### 7.2 建议的数据结构

建议扩展 `models.Message`，引入统一的消息部件结构。

示意如下：

```go
type MessagePart struct {
    Type     string // text | image | file
    Text     string
    Name     string
    MimeType string
    URL      string
    Path     string
}

type Message struct {
    Role              string
    Content           string
    Parts             []MessagePart
    ThinkingContent   string
    ReasoningMetadata *ReasoningReplay
    ToolCalls         []ToolCall
    ToolCallID        string
    Timestamp         time.Time
}
```

### 7.3 兼容策略

为了降低改动范围，采用渐进兼容策略：

1. 纯文本消息：继续只使用 `Content`。
2. 图文消息：同时保留 `Content` 与 `Parts`。
3. 老历史消息没有 `Parts` 时，仍按文本逻辑运行。
4. 渠道展示和日志仍可使用摘要文本，但 Provider 构造必须优先读取 `Parts`。

### 7.4 文本摘要的角色变化

现有的 `summarizeWebChatMessage` 仍可保留，但用途应收敛为：

1. UI 简要展示。
2. 日志预览。
3. 不支持多模态时的回退说明。

不能再把摘要文本当作视觉输入的唯一来源。

## 8. 运行时与会话处理设计

### 8.1 入口处理

WebChat 消息进入后端后，应完成以下动作：

1. 对传入 `media[]` 调用现有规范化逻辑。
2. 根据 `media` 生成 `MessagePart`。
3. 如果用户还输入了文本，则生成 text part。
4. 将结构化结果写入 session message。

### 8.2 会话历史

会话历史中需要保存结构化消息，以支持后续多轮对话继续引用图片上下文。

当前阶段先按以下约束处理：

1. 历史消息保存图片引用信息。
2. Provider 每次构造请求时，根据 `Path` 或存储记录读取文件。
3. 第一阶段不做去重缓存优化。

### 8.3 Slash 命令与工具链影响

对于 slash 命令和现有文本命令链路，保持以下规则：

1. 如果消息是 slash 命令，仍按现有文本优先逻辑处理。
2. 第一阶段不要求 slash 命令消费图片附件。
3. 工具调用链不需要理解图片本体，只要模型调用前能看到图片即可。

## 9. Provider 多模态适配设计

核心原则是：前端和会话层只维护统一消息结构，Provider 层各自负责协议映射。

### 9.1 OpenAI / OpenAI-compatible

OpenAI 用户消息建议编码为 content array。

示意：

```json
{
  "role": "user",
  "content": [
    {"type": "text", "text": "帮我看下这张图"},
    {
      "type": "image_url",
      "image_url": {
        "url": "data:image/png;base64,..."
      }
    }
  ]
}
```

实现建议：

1. 优先从本地存储文件读取图片。
2. 转成 data URL 后写入消息。
3. 对 OpenAI-compatible Provider 保持同样策略，避免依赖服务端能直接访问本地 `/media/...` 地址。

### 9.2 Anthropic

Anthropic 用户消息建议编码为 content blocks。

示意：

```json
{
  "role": "user",
  "content": [
    {"type": "text", "text": "帮我看下这张图"},
    {
      "type": "image",
      "source": {
        "type": "base64",
        "media_type": "image/png",
        "data": "..."
      }
    }
  ]
}
```

实现建议：

1. 增加 image block 构造方法。
2. 按 Anthropic 要求写入 `media_type` 与 base64 数据。
3. 仅在 user role 下写入图片输入块。

### 9.3 Gemini / Gemini-compatible

Gemini 用户消息建议编码为 parts。

示意：

```json
{
  "role": "user",
  "parts": [
    {"text": "帮我看下这张图"},
    {
      "inline_data": {
        "mime_type": "image/png",
        "data": "..."
      }
    }
  ]
}
```

实现建议：

1. 为 Gemini part 增加 `inline_data` 结构。
2. 与原有 text、functionCall、functionResponse 并存。
3. 第一阶段只支持图片，不扩展其他文件类型。

### 9.4 统一辅助层

建议新增统一辅助层，而不是把图片处理逻辑散落在各 Provider 中。

建议职责：

1. 从 `MessagePart` 中提取文本与图片。
2. 读取图片文件。
3. 校验 mime type。
4. 生成 provider 所需的 base64 数据。

可以考虑新增类似以下辅助函数：

1. `extractMessagePartsForProvider`
2. `loadImagePartData`
3. `buildOpenAIImageInput`
4. `buildAnthropicImageBlock`
5. `buildGeminiInlineDataPart`

## 10. 支持格式与限制

第一阶段只支持以下图片类型：

1. `image/png`
2. `image/jpeg`
3. `image/webp`
4. 可选 `image/gif`，但按静态图处理

建议限制如下：

1. 单文件最大 10MB 或沿用当前 20MB 上限后再单独收紧。
2. 单条消息最多 4 张图片。
3. 单次会话历史中的图片继续按现有会话策略存储，不新增长期归档机制。
4. 非图片文件先只作为普通附件展示，不送入视觉模型。

## 11. 错误处理与回退策略

### 11.1 前端错误

前端需要处理：

1. 上传失败。
2. 文件类型不支持。
3. 文件过大。
4. 当前模型不支持视觉。

### 11.2 后端错误

后端需要处理：

1. 媒体记录不存在。
2. 文件读取失败。
3. MIME 类型不支持。
4. 模型不支持视觉但收到了图片。

### 11.3 回退原则

本阶段不建议把图片 URL 拼进 prompt 作为伪视觉输入。

原因：

1. 模型通常无法稳定访问本地媒体 URL。
2. 这不是真正的视觉输入。
3. 容易产生“看起来支持，实际上看不到图”的误导体验。

因此当模型不支持视觉时，应明确提示，而不是偷偷退化成文本 URL。

## 12. 安全与边界

需要注意以下安全边界：

1. 继续使用文件名净化，避免目录穿越。
2. 只允许白名单图片 MIME 类型进入视觉链路。
3. 限制上传体积与数量，避免请求膨胀。
4. Provider 请求前再做一次文件读取与类型校验，不完全信任前端传参。
5. 如果后续开放公网访问媒体 URL，再单独评估鉴权与暴露范围。

## 13. 实施拆分

建议按三个阶段落地。

### 阶段一：前端上传交互

只做用户体验闭环：

1. 上传按钮。
2. 拖拽上传。
3. 粘贴截图。
4. 附件预览托盘。
5. 调用 `/api/media/upload`。
6. WebSocket 发送时带上 `media[]`。

完成标志：

1. 用户能够在 WebChat 中选图并发送。
2. 前端能够显示待发送与已发送图片。

### 阶段二：结构化消息落地

只做后端消息模型改造：

1. 扩展 `models.Message` 支持结构化 parts。
2. WebChat 入站消息写入结构化图片信息。
3. Session 历史保留图片引用。

完成标志：

1. 图片输入不再只靠文本摘要保留。
2. Provider 前已经能拿到统一结构化消息。

### 阶段三：Provider 视觉适配

逐个打通主要视觉模型：

1. OpenAI / OpenAI-compatible。
2. Anthropic / Anthropic-compatible。
3. Gemini / Gemini-compatible。
4. 其他 Provider 暂时返回不支持。

完成标志：

1. 用户发送图片后，模型可直接回答图片内容。
2. 不支持视觉的模型有清晰报错。

## 14. 测试建议

### 14.1 前端测试

1. 文件选择上传。
2. 拖拽上传。
3. 粘贴截图上传。
4. 删除待发送附件。
5. 图文混发。
6. 仅图片发送。
7. 上传失败后重试。

### 14.2 后端测试

1. 上传接口成功与失败路径。
2. `media[]` 规范化与落盘。
3. 会话消息是否保留结构化图片输入。
4. 图片过大、格式错误、记录不存在等异常路径。

### 14.3 Provider 测试

1. OpenAI 图文输入成功。
2. Anthropic 图文输入成功。
3. Gemini 图文输入成功。
4. 不支持视觉的模型返回明确错误。
5. 多张图片输入顺序正确。

## 15. 建议的文件改动范围

预估会涉及以下区域：

1. `web/js/chat.js`
2. 相关 WebChat 页面模板与样式文件
3. `internal/channels/webchat/webchat.go`
4. `pkg/models/models.go`
5. session/runtime 相关消息落盘与回放逻辑
6. `internal/llm/openai.go`
7. `internal/llm/openai_compatible.go`
8. `internal/llm/openai_responses_compatible.go`
9. `internal/llm/anthropic.go`
10. `internal/llm/anthropic_compatible.go`
11. `internal/llm/gemini.go`
12. `internal/llm/gemini_compatible.go`
13. 可能新增 `internal/llm` 下的多模态辅助文件

## 16. 最终建议

本次能力应按“先图片、后通用文件；先 WebChat、后其他渠道；先统一消息模型、后 Provider 适配”的顺序推进。

最关键的设计点只有两个：

1. 不要把图片仅作为展示附件处理，必须进入结构化消息模型。
2. 不要把图片 URL 拼进 prompt 冒充视觉输入，必须在 Provider 请求层转成真实多模态块。

按这个方向推进，第一版就能形成真正可用的 WebChat 看图能力，而不是只有上传 UI 的半成品方案。
