package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
)

const imageDescribeSystemPrompt = `你是一个专业的图像分析助手。用户会提供一张图片和一个问题，请基于图片内容给出准确、详细的回答。直接输出分析结果，不要赘述无关内容。`

const maxImageBytes = 20 * 1024 * 1024 // 单张图片体积上限 20MB（下载/读取后）

// ImageDescribeTool 读图工具
// 让没有视觉能力的主模型通过调用有视觉能力的模型来"看图"。
// 单次只处理一张图片，防止 token 爆炸。
type ImageDescribeTool struct {
	llmReg *llm.Registry
	cfg    config.ImageDescribeConfig
}

// NewImageDescribeTool 创建读图工具
func NewImageDescribeTool(llmReg *llm.Registry, cfg config.ImageDescribeConfig) *ImageDescribeTool {
	return &ImageDescribeTool{
		llmReg: llmReg,
		cfg:    cfg,
	}
}

func (t *ImageDescribeTool) Name() string { return "image_describe" }

// IsTimeoutExempt 自管超时（内部按 cfg.Timeout 控制），豁免 Registry 全局兜底
func (t *ImageDescribeTool) IsTimeoutExempt() bool { return true }

func (t *ImageDescribeTool) Description() string {
	return `Read and analyze a single image using a vision-capable model, returning a text description. Use this when you (the main model) cannot see images directly (i.e. you are a text-only model) and the user has sent an image, a screenshot, a photo, a chart, or any visual that needs to be understood.

**When to use:**
- The user sent an image / screenshot / photo / chart and asked about its content.
- You need to read text embedded in an image (error messages, UI, code, documents).
- You need to describe or identify what is in an image.

**When NOT to use:**
- There is no image involved in the user's request.
- You already have enough context to answer without seeing the image.

## Parameters
- source (required): The image to read. Accepts ONE of:
  1. An image URL (http/https) — the tool will download it.
  2. A local file path (absolute or relative to the workspace).
  3. A base64 string of the image bytes (with or without a "data:image/...;base64," prefix).
- question (required): What you want to know about this image. Be specific — e.g. "Describe this image in Chinese", "Extract all error text", "What code is shown?". The answer quality depends heavily on how precise the question is.

## Notes
- Only ONE image per call. Call the tool again for additional images.
- Supports PNG / JPEG / WebP / GIF.
- The result is plain text from the vision model.`
}

func (t *ImageDescribeTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"source": map[string]interface{}{
				"type":        "string",
				"description": "The image to read. Accepts an image URL (http/https), a local file path, or a base64 string (optionally with a 'data:image/...;base64,' prefix).",
			},
			"question": map[string]interface{}{
				"type":        "string",
				"description": "What you want to know about this image. Be specific (e.g. 'Describe this image in Chinese', 'Extract all error text', 'What code is shown?').",
			},
		},
		"required": []string{"source", "question"},
	}
}

func (t *ImageDescribeTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("image_describe requires execution context")
}

// ExecuteWithRole 实现 ToolWithContext（与 fusion 一致的入口风格）
func (t *ImageDescribeTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return t.run(ctx, args)
}

func (t *ImageDescribeTool) run(ctx context.Context, args map[string]interface{}) (string, error) {
	source := strings.TrimSpace(stringArg(args, "source"))
	question := strings.TrimSpace(stringArg(args, "question"))
	if source == "" {
		return "", fmt.Errorf("'source' is required (image URL / local path / base64)")
	}
	if question == "" {
		return "", fmt.Errorf("'question' is required (what you want to know about the image)")
	}

	timeout := t.parseTimeout()

	// 1) 把 source 本地化为 OutgoingMedia（provider 只认 Data/Path）
	media, err := resolveImageSource(source)
	if err != nil {
		return "", fmt.Errorf("resolve image source: %w", err)
	}

	// 2) 解析视觉模型 provider/model
	resolvedProvider, resolvedModel, err := t.llmReg.ResolveProviderModel(t.cfg.Provider, t.cfg.Model)
	if err != nil {
		return "", fmt.Errorf("resolve vision provider/model failed: %w", err)
	}
	provider, err := t.llmReg.Get(resolvedProvider)
	if err != nil {
		return "", fmt.Errorf("vision provider not found: %s", resolvedProvider)
	}

	// 3) 构造调用视觉模型的消息：system + user(text=question, media=image)
	messages := []models.Message{
		{Role: "system", Content: imageDescribeSystemPrompt},
		{Role: "user", Content: question, Media: []models.OutgoingMedia{media}},
	}

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	opts := llm.ChatOptions{Model: resolvedModel}
	content, err := streamChatCollectForImageDescribe(callCtx, provider, messages, opts)
	if err != nil {
		logger.Warn("image_describe vision call failed", logger.Fields{
			"provider": resolvedProvider,
			"model":    resolvedModel,
			"error":    err.Error(),
			"elapsed":  time.Since(start).String(),
		})
		return "", fmt.Errorf("vision model call failed: %w", err)
	}

	logger.Info("image_describe completed", logger.Fields{
		"provider": resolvedProvider,
		"model":    resolvedModel,
		"elapsed":  time.Since(start).String(),
		"chars":    len(content),
	})

	return content, nil
}

// parseTimeout 解析单次调用超时
func (t *ImageDescribeTool) parseTimeout() time.Duration {
	if t.cfg.Timeout != "" {
		if d, err := time.ParseDuration(t.cfg.Timeout); err == nil && d > 0 {
			return d
		}
	}
	return 60 * time.Second
}

// ---- 图片 source 本地化 ----

// resolveImageSource 把用户传入的 source 统一转成 OutgoingMedia（含 Path 或 Data）。
// provider 层只消费 OutgoingMedia.Data(base64) 或 OutgoingMedia.Path(本地文件)，不认 URL，
// 因此 URL / 纯 base64 都需要本地化到临时文件，再交给 provider 读取。
func resolveImageSource(source string) (models.OutgoingMedia, error) {
	trimmed := strings.TrimSpace(source)

	// 情况1: data URL —— data:image/png;base64,xxxx
	if strings.HasPrefix(trimmed, "data:") {
		mimeType, b64, ok := parseDataURL(trimmed)
		if !ok {
			return models.OutgoingMedia{}, fmt.Errorf("invalid data URL format")
		}
		if !supportedImageMime(mimeType) {
			return models.OutgoingMedia{}, fmt.Errorf("unsupported mime type from data URL: %s (allowed: png/jpeg/webp/gif)", mimeType)
		}
		path, err := writeBase64Temp(mimeType, b64)
		if err != nil {
			return models.OutgoingMedia{}, err
		}
		return mediaFromPath(path, mimeType), nil
	}

	// 情况2: 纯 base64（无 data: 前缀，无 http 前缀，且长度足够长 & 仅含 base64 字符）
	if looksLikeBase64(trimmed) {
		path, mimeType, err := writeRawBase64Temp(trimmed)
		if err != nil {
			return models.OutgoingMedia{}, err
		}
		return mediaFromPath(path, mimeType), nil
	}

	// 情况3: HTTP(S) URL —— 下载到临时文件
	if strings.HasPrefix(strings.ToLower(trimmed), "http://") || strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		path, mimeType, err := downloadImage(trimmed)
		if err != nil {
			return models.OutgoingMedia{}, err
		}
		return mediaFromPath(path, mimeType), nil
	}

	// 情况4: 本地文件路径
	if _, err := os.Stat(trimmed); err == nil {
		mimeType := guessMimeByExt(trimmed)
		if !supportedImageMime(mimeType) {
			return models.OutgoingMedia{}, fmt.Errorf("unsupported image type (by extension): %s (allowed: png/jpeg/webp/gif)", mimeType)
		}
		return mediaFromPath(trimmed, mimeType), nil
	}

	return models.OutgoingMedia{}, fmt.Errorf("unrecognized source (not a URL, local path, or base64): %s", truncateForLog(trimmed, 80))
}

func mediaFromPath(path, mimeType string) models.OutgoingMedia {
	return models.OutgoingMedia{
		Name:     filepath.Base(path),
		MimeType: mimeType,
		Path:     path,
	}
}

// parseDataURL 解析 "data:image/png;base64,xxxx" -> (mime, base64, ok)
func parseDataURL(s string) (string, string, bool) {
	// data:image/png;base64,XXXX
	if !strings.HasPrefix(s, "data:") {
		return "", "", false
	}
	body := s[len("data:"):]
	commaIdx := strings.Index(body, ",")
	if commaIdx < 0 {
		return "", "", false
	}
	head := body[:commaIdx]
	data := body[commaIdx+1:]
	mime := "image/png"
	isBase64 := false
	parts := strings.Split(head, ";")
	for _, p := range parts {
		if p == "base64" {
			isBase64 = true
		} else if strings.HasPrefix(p, "image/") {
			mime = p
		}
	}
	if !isBase64 {
		// 非 base64 的 data URL 不支持
		return "", "", false
	}
	return mime, data, true
}

// writeBase64Temp 把 base64 数据写入临时文件（带正确扩展名），返回路径
func writeBase64Temp(mimeType, b64 string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		// 尝试 RawStdEncoding（去 padding）
		data, err = base64.RawStdEncoding.DecodeString(strings.TrimSpace(b64))
		if err != nil {
			return "", fmt.Errorf("decode base64: %w", err)
		}
	}
	if err := checkImageSize(data); err != nil {
		return "", err
	}
	ext := extForMime(mimeType)
	tmp, err := os.CreateTemp("", "evoduck-img-*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmp.Close()
	if _, err := tmp.Write(data); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("write temp file: %w", err)
	}
	return tmp.Name(), nil
}

// writeRawBase64Temp 把纯 base64 字符串（无前缀）写入临时文件。
// MIME 类型无法从内容确定，写为 png（视觉模型按扩展名/mime 识别，png 通用兼容性最好）。
func writeRawBase64Temp(b64 string) (string, string, error) {
	path, err := writeBase64Temp("image/png", b64)
	if err != nil {
		return "", "", err
	}
	return path, "image/png", nil
}

// downloadImage 下载 URL 到临时文件，按 Content-Type 或扩展名确定 MIME
func downloadImage(url string) (string, string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", "EvoDuck-ImageDescribe/1.0")
	resp, err := httpClientForImage().Do(req)
	if err != nil {
		return "", "", fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("download image: HTTP %d", resp.StatusCode)
	}

	mimeType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	// Content-Type 可能带 charset，如 "image/png; charset=utf-8"（异常但兜底）
	if i := strings.Index(mimeType, ";"); i >= 0 {
		mimeType = strings.TrimSpace(mimeType[:i])
	}
	if !supportedImageMime(mimeType) {
		// 回退到按 URL 扩展名判断
		mimeType = guessMimeByExt(url)
	}
	if !supportedImageMime(mimeType) {
		return "", "", fmt.Errorf("unsupported image type: %s (allowed: png/jpeg/webp/gif)", mimeType)
	}

	ext := extForMime(mimeType)
	tmp, err := os.CreateTemp("", "evoduck-img-*"+ext)
	if err != nil {
		return "", "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmp.Close()

	// 限制读取体积，防止恶意大图撑爆内存/磁盘
	limited := &io.LimitedReader{R: resp.Body, N: maxImageBytes + 1}
	n, err := io.Copy(tmp, limited)
	if err != nil {
		os.Remove(tmp.Name())
		return "", "", fmt.Errorf("write downloaded image: %w", err)
	}
	if n > maxImageBytes {
		os.Remove(tmp.Name())
		return "", "", fmt.Errorf("image exceeds size limit (%d bytes)", maxImageBytes)
	}
	return tmp.Name(), mimeType, nil
}

func httpClientForImage() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

func supportedImageMime(mime string) bool {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/png", "image/jpeg", "image/jpg", "image/webp", "image/gif":
		return true
	default:
		return false
	}
}

func extForMime(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func guessMimeByExt(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return ""
	}
}

func checkImageSize(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("image data is empty")
	}
	if len(data) > maxImageBytes {
		return fmt.Errorf("image exceeds size limit (%d bytes)", maxImageBytes)
	}
	return nil
}

func looksLikeBase64(s string) bool {
	// 足够长（>256）、不含 URL/路径分隔符与空格、仅 base64 字母表
	if len(s) < 256 {
		return false
	}
	if strings.ContainsAny(s, " :/\\\n\r\t") {
		return false
	}
	// 允许的 base64 字符
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=-_"
	for _, ch := range s {
		if !strings.ContainsRune(alphabet, ch) {
			return false
		}
	}
	return true
}

func truncateForLog(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

// ---- 视觉模型流式调用（复用 fusion 的 streamChatCollect 思路）----

// streamWithOptionsProviderImage 与 fusion 中同名接口一致：
// 优先用 ChatStreamWithOptions（并发安全，不改共享 defaultOptions），否则 fallback。
type streamWithOptionsProviderImage interface {
	ChatStreamWithOptions(ctx context.Context, messages []models.Message, tools []models.ToolDefinition, opts llm.ChatOptions) (<-chan models.StreamEvent, error)
}

func streamChatCollectForImageDescribe(ctx context.Context, provider llm.Provider, messages []models.Message, opts llm.ChatOptions) (string, error) {
	if swop, ok := provider.(streamWithOptionsProviderImage); ok {
		streamCh, err := swop.ChatStreamWithOptions(ctx, messages, nil, opts)
		if err != nil {
			return "", fmt.Errorf("stream open: %w", err)
		}
		return collectStreamContent(ctx, streamCh)
	}
	// fallback：非并发安全路径（仅用于未实现 ChatStreamWithOptions 的 provider）
	provider.SetDefaultOptions(opts)
	streamCh, err := provider.ChatStream(ctx, messages, nil)
	if err != nil {
		return "", fmt.Errorf("stream open: %w", err)
	}
	return collectStreamContent(ctx, streamCh)
}
