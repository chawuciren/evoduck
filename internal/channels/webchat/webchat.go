package webchat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/chawuciren/evoduck/internal/channels"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/gorilla/websocket"
)

// WebChat WebSocket WebChat 渠道
type WebChat struct {
	mu       sync.RWMutex
	clients  map[string]*WebChatClient // clientID -> client
	handler  func(*models.NormalizedMessage)
	server   *http.Server
	upgrader websocket.Upgrader
	config   WebChatConfig
	history  *WebChatHistory // 历史消息存储
}

// WebChatConfig WebChat 配置
type WebChatConfig struct {
	Port         int           `json:"port"`
	Path         string        `json:"path"`
	ReadTimeout  time.Duration `json:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout"`
}

// WebChatClient WebChat 客户端
type WebChatClient struct {
	ID          string
	UserID      string
	SessionID   string
	Conn        *websocket.Conn
	ConnectedAt time.Time
}

// WebChatHistory 历史消息存储（内存）
type WebChatHistory struct {
	messages []HistoryMessage
	mu       sync.RWMutex
	maxSize  int
}

// WebChatMessage WebChat 消息格式
type WebChatMessage struct {
	Type      string                 `json:"type"`    // "message", "ping", "pong", "join", "leave"
	Action    string                 `json:"action"`  // "chat", "get_history"
	Content   string                 `json:"content"` // 消息内容
	Message   string                 `json:"message"` // 消息内容（action 格式）
	Media     []models.OutgoingMedia `json:"media,omitempty"`
	UserID    string                 `json:"user_id"`    // 用户 ID
	SessionID string                 `json:"session_id"` // 会话 ID
	Session   string                 `json:"session"`    // 会话 ID（action 格式）
	AgentID   string                 `json:"agent_id"`   // Agent ID
	Timestamp int64                  `json:"timestamp"`  // 时间戳
	Limit     int                    `json:"limit"`      // 历史消息限制
}

// HistoryMessage 历史消息
type HistoryMessage struct {
	Role      string                 `json:"role"`    // "user" or "assistant"
	Content   string                 `json:"content"` // 消息内容
	Media     []models.OutgoingMedia `json:"media,omitempty"`
	Timestamp int64                  `json:"timestamp"` // 时间戳
}

// New 创建 WebChat 实例
func New(config WebChatConfig) *WebChat {
	if config.Port == 0 {
		config.Port = 8080
	}
	if config.Path == "" {
		config.Path = "/ws/chat"
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 60 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 10 * time.Second
	}

	return &WebChat{
		clients: make(map[string]*WebChatClient),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // 允许所有来源（生产环境应限制）
			},
		},
		config: config,
		history: &WebChatHistory{
			messages: make([]HistoryMessage, 0),
			maxSize:  100, // 最多保存 100 条历史消息
		},
	}
}

// Name 返回渠道名称
func (w *WebChat) Name() string {
	return "webchat"
}

// Connect 启动 WebSocket 服务器
func (w *WebChat) Connect(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc(w.config.Path, w.handleWS)
	mux.HandleFunc("/health", w.handleHealth)

	addr := fmt.Sprintf(":%d", w.config.Port)
	w.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  w.config.ReadTimeout,
		WriteTimeout: w.config.WriteTimeout,
	}

	go func() {
		logger.Info("WebChat server starting", logger.Fields{
			"address": addr,
			"path":    w.config.Path,
		})
		if err := w.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("WebChat server error", logger.Fields{
				"error": err.Error(),
			})
		}
	}()

	return nil
}

// Disconnect 关闭服务器
func (w *WebChat) Disconnect() error {
	if w.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		logger.Info("WebChat server shutting down")
		return w.server.Shutdown(ctx)
	}
	return nil
}

// OnMessage 注册消息处理器
func (w *WebChat) OnMessage(handler func(*models.NormalizedMessage)) {
	w.handler = handler
}

// Send 发送消息到指定客户端
func (w *WebChat) Send(ctx context.Context, msg *models.OutgoingMessage) error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	client, ok := w.clients[msg.TargetID]
	if !ok {
		return fmt.Errorf("client not found: %s", msg.TargetID)
	}

	wsMsg := WebChatMessage{
		Type:      "message",
		Content:   msg.Content,
		Media:     append([]models.OutgoingMedia(nil), msg.Media...),
		Timestamp: time.Now().Unix(),
	}

	data, err := json.Marshal(wsMsg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	if err := client.Conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("write message: %w", err)
	}

	// 保存助手消息到历史
	w.addHistory("assistant", summarizeWebChatMessage(msg.Content, msg.Media), msg.Media)

	logger.Debug("WebChat message sent", logger.Fields{
		"client_id": msg.TargetID,
		"length":    len(msg.Content),
	})

	return nil
}

func (w *WebChat) HandleEvent(ctx context.Context, target *models.NormalizedMessage, event *models.ChannelEvent) error {
	if target == nil || event == nil {
		return nil
	}
	w.mu.RLock()
	client, ok := w.clients[target.SenderID]
	w.mu.RUnlock()
	if !ok {
		return fmt.Errorf("client not found: %s", target.SenderID)
	}

	wsMsg := buildWebChatEventMessage(target, event)

	data, err := json.Marshal(wsMsg)
	if err != nil {
		return fmt.Errorf("marshal event message: %w", err)
	}
	if err := client.Conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("write event message: %w", err)
	}

	if event.Done {
		w.addHistory("assistant", summarizeWebChatMessage(wsMsg.Content, nil), nil)
	}
	return nil
}

func buildWebChatEventMessage(target *models.NormalizedMessage, event *models.ChannelEvent) WebChatMessage {
	wsMsg := WebChatMessage{
		Type:      event.Type,
		Content:   event.Content,
		SessionID: target.ThreadID,
		Timestamp: time.Now().Unix(),
	}
	if event.Type == models.ChannelEventRunStart {
		wsMsg.Type = "typing"
	}
	if event.ToolName != "" {
		wsMsg.Message = event.ToolName
	}
	if event.Plan != nil {
		if planText := formatWebChatPlan(event.Plan); planText != "" {
			wsMsg.Content = planText
		}
	}
	return wsMsg
}

// Broadcast 广播消息到所有客户端（排除指定客户端）
func (w *WebChat) Broadcast(ctx context.Context, content string, excludeTarget string) error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	wsMsg := WebChatMessage{
		Type:      "message",
		Content:   content,
		Timestamp: time.Now().Unix(),
	}

	data, err := json.Marshal(wsMsg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	for id, client := range w.clients {
		if id == excludeTarget {
			continue
		}

		if err := client.Conn.WriteMessage(websocket.TextMessage, data); err != nil {
			logger.Error("WebChat broadcast error", logger.Fields{
				"client_id": id,
				"error":     err.Error(),
			})
		}
	}

	return nil
}

// handleWS 处理 WebSocket 连接
func (w *WebChat) handleWS(writer http.ResponseWriter, req *http.Request) {
	conn, err := w.upgrader.Upgrade(writer, req, nil)
	if err != nil {
		logger.Error("WebChat upgrade error", logger.Fields{
			"error": err.Error(),
		})
		return
	}

	// 从 query 参数获取用户信息
	userID := req.URL.Query().Get("user_id")
	if userID == "" {
		userID = "anonymous"
	}

	sessionID := req.URL.Query().Get("session_id")

	// 生成客户端 ID
	clientID := fmt.Sprintf("webchat_%s_%d", userID, time.Now().UnixNano())

	client := &WebChatClient{
		ID:          clientID,
		UserID:      userID,
		SessionID:   sessionID,
		Conn:        conn,
		ConnectedAt: time.Now(),
	}

	w.mu.Lock()
	w.clients[clientID] = client
	w.mu.Unlock()

	logger.Info("WebChat client connected", logger.Fields{
		"client_id":  clientID,
		"user_id":    userID,
		"session_id": sessionID,
	})

	// 发送连接成功消息
	w.sendWelcome(clientID, conn)

	// 读取消息循环
	defer func() {
		w.mu.Lock()
		delete(w.clients, clientID)
		w.mu.Unlock()
		conn.Close()

		logger.Info("WebChat client disconnected", logger.Fields{
			"client_id": clientID,
		})
	}()

	w.readLoop(clientID, conn, userID, sessionID)
}

// handleHealth 健康检查
func (w *WebChat) handleHealth(writer http.ResponseWriter, req *http.Request) {
	w.mu.RLock()
	clientCount := len(w.clients)
	w.mu.RUnlock()

	writer.Header().Set("Content-Type", "application/json")
	json.NewEncoder(writer).Encode(map[string]interface{}{
		"status":       "healthy",
		"channel":      "webchat",
		"client_count": clientCount,
	})
}

// sendWelcome 发送欢迎消息
func (w *WebChat) sendWelcome(clientID string, conn *websocket.Conn) {
	wsMsg := WebChatMessage{
		Type:      "welcome",
		Content:   "Connected to EvoDuck WebChat",
		SessionID: clientID,
		Timestamp: time.Now().Unix(),
	}

	data, _ := json.Marshal(wsMsg)
	conn.WriteMessage(websocket.TextMessage, data)
}

// readLoop 读取消息循环
func (w *WebChat) readLoop(clientID string, conn *websocket.Conn, userID, sessionID string) {
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				logger.Error("WebChat read error", logger.Fields{
					"client_id": clientID,
					"error":     err.Error(),
				})
			}
			break
		}

		// 解析消息
		var wsMsg WebChatMessage
		if err := json.Unmarshal(message, &wsMsg); err != nil {
			// 如果不是 JSON 格式，当作纯文本处理
			wsMsg = WebChatMessage{
				Type:    "message",
				Content: string(message),
			}
		}

		// 调试日志 - 使用 Info 级别确保输出
		logger.Info("WebChat received message", logger.Fields{
			"client_id": clientID,
			"action":    wsMsg.Action,
			"type":      wsMsg.Type,
			"message":   wsMsg.Message,
			"raw":       string(message),
		})

		// 处理不同类型的消息
		// 优先处理 action 字段（新格式）
		if wsMsg.Action != "" {
			logger.Info("Processing action", logger.Fields{"action": wsMsg.Action})
			switch wsMsg.Action {
			case "chat":
				// 聊天消息
				if w.handler != nil {
					content := wsMsg.Message
					if strings.TrimSpace(content) == "" {
						content = wsMsg.Content
					}
					content = summarizeWebChatMessage(content, wsMsg.Media)
					msg := &models.NormalizedMessage{
						Channel:  "webchat",
						SenderID: clientID,
						Content:  content,
						Media:    append([]models.OutgoingMedia(nil), wsMsg.Media...),
						ThreadID: wsMsg.Session,
						IsDM:     true,
						Role:     models.RoleAdmin, // WebChat 默认为 admin 角色（最高权限）
					}

					if userID != "" {
						msg.SenderID = userID
					}
					if wsMsg.Session == "" && sessionID != "" {
						msg.ThreadID = sessionID
					}

					// 保存用户消息到历史
					w.addHistory("user", content, wsMsg.Media)

					w.handler(msg)
				}

			case "get_history":
				// 获取历史消息
				w.sendHistory(conn, clientID, wsMsg.Limit)

			default:
				// 未知 action
				errMsg := WebChatMessage{
					Type:    "error",
					Content: fmt.Sprintf("Unknown action: %s", wsMsg.Action),
				}
				data, _ := json.Marshal(errMsg)
				conn.WriteMessage(websocket.TextMessage, data)
			}
			continue
		}

		// 处理 type 字段（旧格式）
		switch wsMsg.Type {
		case "ping":
			// 响应 pong
			pong := WebChatMessage{Type: "pong", Timestamp: time.Now().Unix()}
			data, _ := json.Marshal(pong)
			conn.WriteMessage(websocket.TextMessage, data)

		case "message":
			// 路由到消息处理器
			if w.handler != nil {
				content := summarizeWebChatMessage(wsMsg.Content, wsMsg.Media)
				msg := &models.NormalizedMessage{
					Channel:  "webchat",
					SenderID: clientID,
					Content:  content,
					Media:    append([]models.OutgoingMedia(nil), wsMsg.Media...),
					ThreadID: sessionID,
					IsDM:     true,
					Role:     models.RoleAdmin, // WebChat 默认为 admin 角色（最高权限）
				}

				if wsMsg.UserID != "" {
					msg.SenderID = wsMsg.UserID
				}
				if wsMsg.SessionID != "" {
					msg.ThreadID = wsMsg.SessionID
				}

				w.addHistory("user", content, wsMsg.Media)
				w.handler(msg)
			}

		case "join":
			// 用户加入
			logger.Info("WebChat user joined", logger.Fields{
				"client_id": clientID,
				"user_id":   wsMsg.UserID,
			})
		}
	}
}

// GetClientCount 获取客户端数量
func (w *WebChat) GetClientCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.clients)
}

// addHistory 添加历史消息
func (w *WebChat) addHistory(role, content string, media []models.OutgoingMedia) {
	if w.history == nil {
		return
	}

	w.history.mu.Lock()
	defer w.history.mu.Unlock()

	msg := HistoryMessage{
		Role:      role,
		Content:   content,
		Media:     append([]models.OutgoingMedia(nil), media...),
		Timestamp: time.Now().Unix(),
	}

	w.history.messages = append(w.history.messages, msg)

	// 限制历史消息数量
	if len(w.history.messages) > w.history.maxSize {
		w.history.messages = w.history.messages[1:]
	}
}

func summarizeWebChatMessage(content string, media []models.OutgoingMedia) string {
	content = strings.TrimSpace(content)
	if len(media) == 0 {
		return content
	}
	parts := make([]string, 0, len(media))
	for _, item := range media {
		mediaType := strings.TrimSpace(item.Type)
		if mediaType == "" {
			mediaType = "media"
		}
		name := strings.TrimSpace(item.Name)
		if name != "" {
			parts = append(parts, fmt.Sprintf("[%s: %s]", mediaType, name))
			continue
		}
		parts = append(parts, fmt.Sprintf("[%s]", mediaType))
	}
	summary := strings.Join(parts, " ")
	if content == "" {
		return summary
	}
	return content + "\n" + summary
}

func formatWebChatPlan(plan *models.TaskPlan) string {
	if plan == nil {
		return ""
	}
	var sb strings.Builder
	if strings.TrimSpace(plan.Intent) != "" {
		sb.WriteString(plan.Intent)
	}
	for i, task := range plan.SubTasks {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("%d. %s [%s]", i+1, strings.TrimSpace(task.Name), strings.TrimSpace(task.Status)))
	}
	return strings.TrimSpace(sb.String())
}

// sendHistory 发送历史消息
func (w *WebChat) sendHistory(conn *websocket.Conn, clientID string, limit int) {
	if w.history == nil {
		return
	}

	w.history.mu.RLock()
	defer w.history.mu.RUnlock()

	// 确定返回的消息数量
	count := limit
	if count <= 0 || count > len(w.history.messages) {
		count = len(w.history.messages)
	}

	// 获取最近的 N 条消息
	start := len(w.history.messages) - count
	if start < 0 {
		start = 0
	}
	messages := w.history.messages[start:]

	// 发送历史消息
	response := map[string]interface{}{
		"type":     "history",
		"messages": messages,
	}

	data, err := json.Marshal(response)
	if err != nil {
		logger.Error("WebChat marshal history error", logger.Fields{
			"client_id": clientID,
			"error":     err.Error(),
		})
		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		logger.Error("WebChat send history error", logger.Fields{
			"client_id": clientID,
			"error":     err.Error(),
		})
	}

	logger.Debug("WebChat history sent", logger.Fields{
		"client_id": clientID,
		"count":     len(messages),
	})
}

// 确保实现 Bridge 接口
var _ channels.Bridge = (*WebChat)(nil)
var _ channels.EventBridge = (*WebChat)(nil)
