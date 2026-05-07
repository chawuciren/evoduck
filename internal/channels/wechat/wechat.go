package wechat

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chawuciren/evoduck/internal/channels"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
)

// WeChat 微信公众号渠道
type WeChat struct {
	mu      sync.RWMutex
	config  WeChatConfig
	handler func(*models.NormalizedMessage)
	server  *http.Server
}

// WeChatConfig 微信配置
type WeChatConfig struct {
	AppID          string `json:"app_id"`           // 公众号 AppID
	AppSecret      string `json:"app_secret"`       // 公众号 AppSecret
	Token          string `json:"token"`            // 接收消息的 Token
	EncodingAESKey string `json:"encoding_aes_key"` // 接收消息的 EncodingAESKey（可选）
	CallbackPath   string `json:"callback_path"`    // 回调路径
	Port           int    `json:"port"`             // 服务端口
}

// WeChatMessage 微信消息
type WeChatMessage struct {
	ToUserName   string `xml:"ToUserName"`
	FromUserName string `xml:"FromUserName"`
	CreateTime   int64  `xml:"CreateTime"`
	MsgType      string `xml:"MsgType"`
	Content      string `xml:"Content"`
	MsgId        int64  `xml:"MsgId"`
	Event        string `xml:"Event"`    // 事件类型
	EventKey     string `xml:"EventKey"` // 事件 KEY
}

// WeChatTextResponse 微信文本响应
type WeChatTextResponse struct {
	ToUserName   string `xml:"ToUserName"`
	FromUserName string `xml:"FromUserName"`
	CreateTime   int64  `xml:"CreateTime"`
	MsgType      string `xml:"MsgType"`
	Content      string `xml:"Content"`
}

// New 创建微信实例
func New(config WeChatConfig) *WeChat {
	if config.CallbackPath == "" {
		config.CallbackPath = "/wechat/callback"
	}
	if config.Port == 0 {
		config.Port = 8082
	}

	return &WeChat{
		config: config,
	}
}

// Name 返回渠道名称
func (w *WeChat) Name() string {
	return "wechat"
}

// Connect 启动 Webhook 服务器
func (w *WeChat) Connect(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc(w.config.CallbackPath, w.handleCallback)
	mux.HandleFunc("/health", w.handleHealth)

	addr := fmt.Sprintf(":%d", w.config.Port)
	w.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	go func() {
		logger.Info("WeChat webhook server starting", logger.Fields{
			"address": addr,
			"path":    w.config.CallbackPath,
		})
		if err := w.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("WeChat server error", logger.Fields{
				"error": err.Error(),
			})
		}
	}()

	return nil
}

// Disconnect 关闭服务器
func (w *WeChat) Disconnect() error {
	if w.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		logger.Info("WeChat server shutting down")
		return w.server.Shutdown(ctx)
	}
	return nil
}

// OnMessage 注册消息处理器
func (w *WeChat) OnMessage(handler func(*models.NormalizedMessage)) {
	w.handler = handler
}

// Send 发送消息（通过微信客服消息接口）
func (w *WeChat) Send(ctx context.Context, msg *models.OutgoingMessage) error {
	// TODO: 实现通过微信 API 发送消息
	// 需要先获取 access_token，然后调用客服消息接口
	logger.Info("WeChat sending message", logger.Fields{
		"target_id": msg.TargetID,
		"length":    len(msg.Content),
	})

	// 这里暂时只是记录日志，实际需要调用微信 API
	// 1. 获取 access_token: https://api.weixin.qq.com/cgi-bin/token
	// 2. 发送客服消息: https://api.weixin.qq.com/cgi-bin/message/custom/send

	return nil
}

// Broadcast 广播消息（微信不支持广播）
func (w *WeChat) Broadcast(ctx context.Context, content string, excludeTarget string) error {
	// 微信公众号不支持广播消息，需要使用模板消息或群发接口
	return nil
}

// handleCallback 处理微信回调
func (w *WeChat) handleCallback(writer http.ResponseWriter, req *http.Request) {
	// 验证签名
	if req.Method == http.MethodGet {
		w.handleVerification(writer, req)
		return
	}

	// 处理消息
	if req.Method == http.MethodPost {
		w.handleMessage(writer, req)
		return
	}

	http.Error(writer, "Method not allowed", http.StatusMethodNotAllowed)
}

// handleVerification 处理微信验证
func (w *WeChat) handleVerification(writer http.ResponseWriter, req *http.Request) {
	query := req.URL.Query()
	signature := query.Get("signature")
	timestamp := query.Get("timestamp")
	nonce := query.Get("nonce")
	echostr := query.Get("echostr")

	// 验证签名
	if !w.verifySignature(signature, timestamp, nonce) {
		logger.Warn("WeChat signature verification failed")
		http.Error(writer, "Invalid signature", http.StatusUnauthorized)
		return
	}

	// 返回 echostr
	writer.Write([]byte(echostr))
	logger.Info("WeChat verification successful")
}

// handleMessage 处理微信消息
func (w *WeChat) handleMessage(writer http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		logger.Error("WeChat read body failed", logger.Fields{
			"error": err.Error(),
		})
		http.Error(writer, "Read body failed", http.StatusInternalServerError)
		return
	}

	// 验证签名
	query := req.URL.Query()
	signature := query.Get("signature")
	timestamp := query.Get("timestamp")
	nonce := query.Get("nonce")

	if !w.verifySignature(signature, timestamp, nonce) {
		logger.Warn("WeChat message signature verification failed")
		http.Error(writer, "Invalid signature", http.StatusUnauthorized)
		return
	}

	// 解析消息
	var msg WeChatMessage
	if err := xml.Unmarshal(body, &msg); err != nil {
		logger.Error("WeChat unmarshal failed", logger.Fields{
			"error": err.Error(),
		})
		http.Error(writer, "Invalid message", http.StatusBadRequest)
		return
	}

	logger.Info("WeChat message received", logger.Fields{
		"from_user": msg.FromUserName,
		"msg_type":  msg.MsgType,
		"content":   msg.Content,
	})

	// 处理不同类型的消息
	switch msg.MsgType {
	case "text":
		w.handleTextMessage(&msg)
	case "event":
		w.handleEventMessage(&msg)
	default:
		logger.Debug("WeChat unhandled message type", logger.Fields{
			"msg_type": msg.MsgType,
		})
	}

	// 返回成功响应（或不回复）
	writer.WriteHeader(http.StatusOK)
	writer.Write([]byte("success"))
}

// handleTextMessage 处理文本消息
func (w *WeChat) handleTextMessage(msg *WeChatMessage) {
	if w.handler != nil {
		normalized := &models.NormalizedMessage{
			Channel:  "wechat",
			SenderID: msg.FromUserName,
			Content:  msg.Content,
			ThreadID: fmt.Sprintf("wechat_%s", msg.FromUserName),
			IsDM:     true,
			Role:     models.RoleCustomer, // 微信公众号默认为客户角色
		}

		go w.handler(normalized)
	}
}

// handleEventMessage 处理事件消息
func (w *WeChat) handleEventMessage(msg *WeChatMessage) {
	switch msg.Event {
	case "subscribe":
		logger.Info("WeChat user subscribed", logger.Fields{
			"user_id": msg.FromUserName,
		})
	case "unsubscribe":
		logger.Info("WeChat user unsubscribed", logger.Fields{
			"user_id": msg.FromUserName,
		})
	case "CLICK":
		// 菜单点击事件
		logger.Info("WeChat menu clicked", logger.Fields{
			"user_id":   msg.FromUserName,
			"event_key": msg.EventKey,
		})
	}
}

// handleHealth 健康检查
func (w *WeChat) handleHealth(writer http.ResponseWriter, req *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(map[string]interface{}{
		"status":  "healthy",
		"channel": "wechat",
	})
}

// verifySignature 验证签名
func (w *WeChat) verifySignature(signature, timestamp, nonce string) bool {
	// 微信签名验证逻辑
	// signature = sha1(sort(Token, timestamp, nonce))
	params := []string{w.config.Token, timestamp, nonce}
	sort.Strings(params)

	combined := strings.Join(params, "")
	h := sha1.New()
	h.Write([]byte(combined))
	calculated := fmt.Sprintf("%x", h.Sum(nil))

	return calculated == signature
}

// 确保实现 Bridge 接口
var _ channels.Bridge = (*WeChat)(nil)
