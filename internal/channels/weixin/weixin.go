package weixin

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chawuciren/evoduck/internal/channels"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

// WeixinBridge 微信个人号渠道桥接
// 一个 Bridge = 一个微信机器人 = 服务一个用户
type WeixinBridge struct {
	mu      sync.RWMutex
	config  WeixinConfig
	api     weixinAPI
	decider *proxy.Decider
	handler func(*models.NormalizedMessage)

	// 从 channel 配置获取
	channelID string      // channel ID，如 "weixin-cs"
	userID    string      // 配置的用户ID（一个机器人只服务一个用户）
	role      models.Role // 角色

	updateBuf string
	running   bool
	cancel    context.CancelFunc
}

type weixinAPI interface {
	SetAuth(token, accountID string)
	SendMessage(ctx context.Context, toUserID, contextToken string, items []MessageItem) error
	GetConfig(ctx context.Context, userID, contextToken string) (*GetConfigResponse, error)
	SendTyping(ctx context.Context, userID, ticket string, status int) error
	GetUpdates(ctx context.Context, buf string) (*GetUpdatesResponse, error)
	GetUploadURL(ctx context.Context, req *GetUploadURLRequest) (*GetUploadURLResponse, error)
	UploadEncryptedMedia(ctx context.Context, uploadURL string, encrypted []byte) (string, error)
}

// New 创建微信 Bridge
func New(config WeixinConfig, decider *proxy.Decider) *WeixinBridge {
	// 只填充缺失的默认值，不覆盖已设置的值
	if config.APIBaseURL == "" {
		config.APIBaseURL = "https://liteapp.weixin.qq.com"
	}
	if config.GetUpdatesTimeout == 0 {
		config.GetUpdatesTimeout = 35 * time.Second
	}
	if config.SendTimeout == 0 {
		config.SendTimeout = 10 * time.Second
	}

	return &WeixinBridge{
		config:  config,
		api:     NewAPIClient(config, decider),
		decider: decider,
	}
}

// Name 返回渠道名称
func (w *WeixinBridge) Name() string {
	return w.channelID
}

// SetChannelConfig 设置渠道配置
func (w *WeixinBridge) SetChannelConfig(channelID, userID string, role models.Role) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.channelID = channelID
	w.userID = userID
	w.role = role
}

// Connect 连接微信
func (w *WeixinBridge) Connect(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		return fmt.Errorf("weixin bridge already running")
	}

	if w.config.Token == "" {
		logger.Warn("Weixin token not configured, skipping connection", logger.Fields{
			"channel_id": w.channelID,
		})
		return fmt.Errorf("weixin token not configured, please login first")
	}

	logger.Info("Weixin bridge connecting...", logger.Fields{
		"channel_id": w.channelID,
		"user_id":    w.userID,
		"role":       w.role,
		"token_len":  len(w.config.Token),
	})

	w.api.SetAuth(w.config.Token, w.config.AccountID)

	ctx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.running = true

	go w.pollLoop(ctx)

	logger.Info("Weixin bridge connected successfully", logger.Fields{
		"channel_id": w.channelID,
		"user_id":    w.userID,
		"role":       w.role,
	})

	return nil
}

// Disconnect 断开连接
func (w *WeixinBridge) Disconnect() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return nil
	}

	if w.cancel != nil {
		w.cancel()
	}

	w.running = false
	logger.Info("Weixin bridge disconnected", logger.Fields{
		"channel_id": w.channelID,
	})

	return nil
}

// OnMessage 注册消息处理器
func (w *WeixinBridge) OnMessage(handler func(*models.NormalizedMessage)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handler = handler
}

// Send 发送消息
func (w *WeixinBridge) Send(ctx context.Context, msg *models.OutgoingMessage) error {
	w.mu.RLock()
	running := w.running
	api := w.api
	w.mu.RUnlock()

	if !running {
		return fmt.Errorf("weixin bridge not running")
	}

	content := strings.TrimSpace(msg.Content)
	if content == "" && len(msg.Media) == 0 {
		return fmt.Errorf("weixin message has no content or media")
	}

	// WeChat personal bot accepts the request payload here, but mixed item_list
	// messages can still fail to render media client-side. Send text first and
	// then send each media item as its own message for compatibility.
	if content != "" {
		if err := api.SendMessage(ctx, msg.TargetID, msg.ContextToken, []MessageItem{{
			Type: 1,
			TextItem: &TextItem{
				Text: msg.Content,
			},
		}}); err != nil {
			return err
		}
	}

	for _, media := range msg.Media {
		item, err := w.buildOutgoingMediaItem(ctx, api, "", msg.TargetID, media)
		if err != nil {
			return err
		}
		if err := api.SendMessage(ctx, msg.TargetID, msg.ContextToken, []MessageItem{item}); err != nil {
			return err
		}
	}

	return nil
}

func (w *WeixinBridge) HandleEvent(ctx context.Context, target *models.NormalizedMessage, event *models.ChannelEvent) error {
	if target == nil || event == nil {
		return nil
	}

	switch event.Type {
	case models.ChannelEventRunStart:
		if err := w.setTypingForEvent(ctx, target, true); err != nil {
			return err
		}
		return nil
	case models.ChannelEventThinking,
		models.ChannelEventPlan,
		models.ChannelEventPlanUpdate,
		models.ChannelEventToolStart,
		models.ChannelEventToolEnd,
		models.ChannelEventContentChunk:
		return nil
	case models.ChannelEventFinal:
		defer w.setTypingOffForEvent(target)
		return w.Send(ctx, &models.OutgoingMessage{
			Channel:      target.Channel,
			TargetID:     target.SenderID,
			Content:      strings.TrimSpace(event.Content),
			ThreadID:     target.ThreadID,
			ContextToken: target.ContextToken,
			ResponseURL:  target.ResponseURL,
		})
	case models.ChannelEventError, models.ChannelEventCancelled:
		defer w.setTypingOffForEvent(target)
		return w.Send(ctx, &models.OutgoingMessage{
			Channel:      target.Channel,
			TargetID:     target.SenderID,
			Content:      strings.TrimSpace(event.Content),
			ThreadID:     target.ThreadID,
			ContextToken: target.ContextToken,
			ResponseURL:  target.ResponseURL,
		})
	default:
		return nil
	}
}

func (w *WeixinBridge) setTypingForEvent(ctx context.Context, msg *models.NormalizedMessage, active bool) error {
	typingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return w.SetTyping(typingCtx, msg, active)
}

func (w *WeixinBridge) setTypingOffForEvent(msg *models.NormalizedMessage) {
	if msg == nil {
		return
	}
	if err := w.setTypingForEvent(context.Background(), msg, false); err != nil {
		logger.Debug("Failed to update channel typing state", logger.Fields{
			"channel":    msg.Channel,
			"account_id": msg.AccountID,
			"active":     false,
			"error":      err.Error(),
		})
	}
}

func (w *WeixinBridge) buildOutgoingItems(ctx context.Context, api weixinAPI, uploadBaseURL string, msg *models.OutgoingMessage) ([]MessageItem, error) {
	items := make([]MessageItem, 0, len(msg.Media)+1)
	if strings.TrimSpace(msg.Content) != "" {
		items = append(items, MessageItem{
			Type: 1,
			TextItem: &TextItem{
				Text: msg.Content,
			},
		})
	}

	for _, media := range msg.Media {
		item, err := w.buildOutgoingMediaItem(ctx, api, uploadBaseURL, msg.TargetID, media)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("weixin message has no content or media")
	}
	return items, nil
}

func buildOutgoingItems(msg *models.OutgoingMessage) ([]MessageItem, error) {
	bridge := &WeixinBridge{}
	return bridge.buildOutgoingItems(context.Background(), nil, "", msg)
}

func (w *WeixinBridge) buildOutgoingMediaItem(ctx context.Context, api weixinAPI, uploadBaseURL, toUserID string, media models.OutgoingMedia) (MessageItem, error) {
	ref, _, err := w.resolveOutgoingMedia(ctx, api, uploadBaseURL, toUserID, media)
	if err != nil {
		return MessageItem{}, err
	}

	switch strings.ToLower(strings.TrimSpace(media.Type)) {
	case "image":
		return MessageItem{
			Type: 2,
			ImageItem: &ImageItem{
				Media:   ref,
				MidSize: EncryptedMediaSize(media.FileSize),
			},
		}, nil
	case "audio", "voice":
		return MessageItem{
			Type: 3,
			VoiceItem: &VoiceItem{
				Media: ref,
			},
		}, nil
	case "file":
		return MessageItem{
			Type: 4,
			FileItem: &FileItem{
				Media:    ref,
				FileName: media.Name,
				Len:      fmt.Sprintf("%d", media.FileSize),
			},
		}, nil
	case "video":
		return MessageItem{
			Type: 5,
			VideoItem: &VideoItem{
				Media:     ref,
				VideoSize: EncryptedMediaSize(media.FileSize),
			},
		}, nil
	default:
		return MessageItem{}, fmt.Errorf("weixin media type %q is not supported", media.Type)
	}
}

func (w *WeixinBridge) resolveOutgoingMedia(ctx context.Context, api weixinAPI, uploadBaseURL, toUserID string, media models.OutgoingMedia) (*CDNMedia, *CDNMedia, error) {
	if strings.TrimSpace(media.EncryptQueryParam) != "" && strings.TrimSpace(media.AESKey) != "" {
		return &CDNMedia{
			EncryptQueryParam: media.EncryptQueryParam,
			AESKey:            media.AESKey,
			EncryptType:       1,
		}, nil, nil
	}
	if api == nil {
		return nil, nil, fmt.Errorf("weixin media %q requires upload api client", media.Type)
	}

	name, data, err := resolveOutgoingMediaBytes(media)
	if err != nil {
		return nil, nil, err
	}
	media.FileSize = int64(len(data))
	return w.uploadMedia(ctx, api, uploadBaseURL, toUserID, media, name, data)
}

func resolveOutgoingMediaBytes(media models.OutgoingMedia) (string, []byte, error) {
	if strings.TrimSpace(media.Path) != "" {
		data, err := os.ReadFile(media.Path)
		if err != nil {
			return "", nil, fmt.Errorf("read media file %q: %w", media.Path, err)
		}
		name := strings.TrimSpace(media.Name)
		if name == "" {
			name = filepath.Base(media.Path)
		}
		return name, data, nil
	}
	if strings.TrimSpace(media.Data) != "" {
		name := strings.TrimSpace(media.Name)
		if name == "" {
			name = "media"
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(media.Data))
		if err != nil {
			return "", nil, fmt.Errorf("decode media data as base64: %w", err)
		}
		return name, decoded, nil
	}
	return "", nil, fmt.Errorf("weixin media %q requires either encrypted reference or path/data", media.Type)
}

func (w *WeixinBridge) uploadMedia(ctx context.Context, api weixinAPI, uploadBaseURL, toUserID string, media models.OutgoingMedia, fileName string, data []byte) (*CDNMedia, *CDNMedia, error) {
	mediaType, err := toWeixinUploadMediaType(media.Type)
	if err != nil {
		return nil, nil, err
	}
	aesKey, err := GenerateMediaAESKey()
	if err != nil {
		return nil, nil, err
	}
	encrypted, err := EncryptMediaForUpload(data, aesKey)
	if err != nil {
		return nil, nil, err
	}
	fileKey := fmt.Sprintf("evoduck/%d/%s", time.Now().UnixMilli(), fileName)
	req := &GetUploadURLRequest{
		FileKey:     fileKey,
		MediaType:   mediaType,
		ToUserID:    toUserID,
		RawSize:     int64(len(data)),
		RawFileMD5:  MediaMD5Hex(data),
		FileSize:    int64(len(encrypted)),
		NoNeedThumb: true,
		AESKey:      EncodeUploadAESKey(aesKey),
	}

	uploadResp, err := api.GetUploadURL(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	uploadURL := strings.TrimSpace(uploadResp.UploadFullURL)
	if uploadURL == "" {
		uploadURL, err = BuildUploadURL(uploadBaseURL, strings.TrimSpace(uploadResp.UploadParam), fileKey)
		if err != nil {
			return nil, nil, err
		}
	}
	encryptQueryParam, err := api.UploadEncryptedMedia(ctx, uploadURL, encrypted)
	if err != nil {
		return nil, nil, err
	}
	mediaRef := &CDNMedia{
		EncryptQueryParam: encryptQueryParam,
		AESKey:            EncodeMediaAESKey(mediaType, aesKey),
		EncryptType:       1,
	}

	return mediaRef, nil, nil
}

func toWeixinUploadMediaType(mediaType string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image":
		return 1, nil
	case "video":
		return 2, nil
	case "file":
		return 3, nil
	case "audio", "voice":
		return 4, nil
	default:
		return 0, fmt.Errorf("weixin media type %q is not supported for upload", mediaType)
	}
}

// Broadcast 广播消息（微信不支持）
func (w *WeixinBridge) Broadcast(ctx context.Context, content string, excludeTarget string) error {
	return fmt.Errorf("weixin does not support broadcast")
}

func (w *WeixinBridge) SetTyping(ctx context.Context, msg *models.NormalizedMessage, active bool) error {
	if msg == nil {
		return fmt.Errorf("weixin typing message is nil")
	}

	w.mu.RLock()
	running := w.running
	userID := w.userID
	w.mu.RUnlock()

	if !running {
		return fmt.Errorf("weixin bridge not running")
	}
	if strings.TrimSpace(msg.ContextToken) == "" {
		return nil
	}

	cfg, err := w.api.GetConfig(ctx, userID, msg.ContextToken)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.TypingTicket) == "" {
		return nil
	}

	status := 0
	if active {
		status = 1
	}
	return w.api.SendTyping(ctx, userID, cfg.TypingTicket, status)
}

// pollLoop 轮询消息
func (w *WeixinBridge) pollLoop(ctx context.Context) {
	logger.Info("Weixin poll loop started", logger.Fields{
		"channel_id": w.channelID,
		"api_base":   w.config.APIBaseURL,
	})

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	pollCount := 0

	for {
		select {
		case <-ctx.Done():
			logger.Info("Weixin poll loop stopped", logger.Fields{
				"channel_id": w.channelID,
			})
			return
		case <-ticker.C:
			// 定期打印心跳日志
			logger.Debug("Weixin poll heartbeat", logger.Fields{
				"channel_id": w.channelID,
				"poll_count": pollCount,
			})
		default:
		}

		resp, err := w.api.GetUpdates(ctx, w.updateBuf)
		pollCount++

		if err != nil {
			logger.Error("Weixin getupdates error", logger.Fields{
				"channel_id": w.channelID,
				"error":      err.Error(),
			})

			time.Sleep(2 * time.Second)
			continue
		}

		w.updateBuf = resp.GetUpdatesBuf

		if len(resp.Msgs) > 0 {
			logger.Info("Weixin received messages", logger.Fields{
				"channel_id": w.channelID,
				"msg_count":  len(resp.Msgs),
			})
			w.processMessages(resp.Msgs)
		}
	}
}

// processMessages 处理消息
func (w *WeixinBridge) processMessages(msgs []WeixinMessage) {
	w.mu.RLock()
	handler := w.handler
	w.mu.RUnlock()

	if handler == nil {
		logger.Warn("Weixin message handler not set", logger.Fields{
			"channel_id": w.channelID,
			"msg_count":  len(msgs),
		})
		return
	}

	logger.Info("Weixin processing messages", logger.Fields{
		"channel_id": w.channelID,
		"msg_count":  len(msgs),
	})

	for _, msg := range msgs {
		logger.Info("Weixin received raw message", logger.Fields{
			"channel_id":   w.channelID,
			"message_id":   msg.MessageID,
			"message_type": msg.MessageType,
			"from_user_id": msg.FromUserID,
			"session_id":   msg.SessionID,
			"item_count":   len(msg.ItemList),
		})

		normalized := w.normalizeMessage(msg)
		if normalized != nil {
			logger.Info("Weixin normalized message", logger.Fields{
				"channel_id": w.channelID,
				"sender_id":  normalized.SenderID,
				"content":    truncateString(normalized.Content, 50),
				"thread_id":  normalized.ThreadID,
			})
			go handler(normalized)
		}
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// normalizeMessage 归一化消息
// 微信个人号：忽略消息中的 from_user_id，使用配置中的 user_id
func (w *WeixinBridge) normalizeMessage(msg WeixinMessage) *models.NormalizedMessage {
	if msg.MessageType != 1 {
		logger.Debug("Weixin skip non-user message", logger.Fields{
			"channel_id":   w.channelID,
			"message_type": msg.MessageType,
		})
		return nil
	}

	content := ""
	for _, item := range msg.ItemList {
		if item.Type == 1 && item.TextItem != nil {
			content = item.TextItem.Text
			break
		}
	}

	if content == "" {
		logger.Debug("Weixin skip empty message", logger.Fields{
			"channel_id": w.channelID,
			"message_id": msg.MessageID,
		})
		return nil
	}

	// 微信个人号：使用配置的用户ID，忽略消息中的 from_user_id
	// 因为一个微信机器人只服务一个用户
	return &models.NormalizedMessage{
		Channel:      "weixin",
		AccountID:    w.channelID, // channel ID，用于 session 隔离
		SenderID:     w.userID,    // 配置的用户ID
		UserID:       w.userID,    // 业务用户ID
		Content:      content,
		ThreadID:     msg.SessionID,
		IsDM:         true,
		Role:         w.role,
		ContextToken: msg.ContextToken,
	}
}

// SetAuth 设置认证信息
func (w *WeixinBridge) SetAuth(token, accountID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.config.Token = token
	w.config.AccountID = accountID
}

var _ channels.Bridge = (*WeixinBridge)(nil)
var _ channels.TypingBridge = (*WeixinBridge)(nil)
