package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/internal/agent"
	"github.com/chawuciren/evoduck/internal/command"
	"github.com/chawuciren/evoduck/internal/knowledge"
	"github.com/chawuciren/evoduck/internal/scheduler"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/gorilla/websocket"
)

var logEntryLinePattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}) (\d{2}:\d{2}:\d{2})\.\d{3}\s+([A-Z]+)\s+(?:\[[^\]]+\]\s+)?(.*)$`)
var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type WSMessage struct {
	Action       string                 `json:"action"`   // "chat", "stream", "get_history", "ping", "get_agents", "get_skills", "get_skill_detail", "get_sessions", "get_settings", "get_logs"
	Type         string                 `json:"type"`     // for backward compatibility (ping/pong)
	AgentID      string                 `json:"agent_id"` // optional, use default if empty
	UserID       string                 `json:"user_id"`  // optional, for user isolation
	Message      string                 `json:"message"`
	Content      string                 `json:"content,omitempty"`
	Media        []models.OutgoingMedia `json:"media,omitempty"`
	Session      string                 `json:"session"` // optional session key
	Limit        int                    `json:"limit"`   // for get_history, get_logs
	Level        string                 `json:"level"`   // for get_logs (filter by level)
	Name         string                 `json:"name,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Schedule     string                 `json:"schedule,omitempty"`
	Prompt       string                 `json:"prompt,omitempty"`
	Enabled      *bool                  `json:"enabled,omitempty"`
	ScheduleID   string                 `json:"schedule_id,omitempty"`
	LegacyTaskID string                 `json:"task_id,omitempty"`
	ConfigYAML   string                 `json:"config_yaml,omitempty"`
	Settings     map[string]any         `json:"settings,omitempty"`
	Path         string                 `json:"path,omitempty"`
	Tags         []string               `json:"tags,omitempty"`
	FromPath     string                 `json:"from_path,omitempty"`
	Directory    string                 `json:"directory,omitempty"`
}

type WSResponse struct {
	Type            string                 `json:"type"` // "content", "tool_start", "tool_end", "iteration", "done", "error", "plan", "plan_update", "thinking"
	Content         string                 `json:"content,omitempty"`
	Media           []models.OutgoingMedia `json:"media,omitempty"`
	ThinkingContent string                 `json:"thinking_content,omitempty"`
	ToolID          string                 `json:"tool_id,omitempty"`
	ToolName        string                 `json:"tool_name,omitempty"`
	ToolParams      string                 `json:"tool_params,omitempty"` // 工具参数 JSON 字符串
	ToolResult      string                 `json:"tool_result,omitempty"`
	Iteration       int                    `json:"iteration,omitempty"`
	Plan            *models.TaskPlan       `json:"plan,omitempty"` // 任务计划数据
	Done            bool                   `json:"done"`
}

func shouldIncludeHistoryMessage(msg models.Message) bool {
	if strings.TrimSpace(msg.Content) != "" {
		return true
	}
	if len(msg.ToolCalls) > 0 {
		return true
	}
	// Keep reasoning-only assistant turns internal by default. They are persisted for
	// provider replay, but not shown in chat history unless we add an explicit UX for it.
	return false
}

func historyMessageContent(msg models.Message) string {
	content := msg.Content
	if content == "" && len(msg.ToolCalls) > 0 {
		toolNames := make([]string, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			toolNames = append(toolNames, tc.Function.Name)
		}
		return "Calling: " + strings.Join(toolNames, ", ")
	}
	return content
}

func (g *Gateway) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("WebSocket upgrade error", logger.Fields{
			"error": err.Error(),
		})
		g.AddLog("error", "WebSocket upgrade failed: "+err.Error())
		return
	}
	defer conn.Close()

	connID := r.RemoteAddr + "_" + time.Now().Format("20060102150405")
	logger.Info("WebSocket connection established", logger.Fields{
		"conn_id": connID,
		"remote":  r.RemoteAddr,
		"path":    r.URL.Path,
	})
	g.AddLog("info", "WebSocket connected: "+connID)
	g.wsConnMu.Lock()
	g.wsConns[connID] = &WSConnection{
		ConnID: connID,
		Conn:   conn,
	}
	g.wsConnMu.Unlock()

	// 清理连接
	defer func() {
		fields := g.wsConnFields(conn)
		g.wsConnMu.Lock()
		delete(g.wsConns, connID)
		g.wsConnMu.Unlock()
		fields["conn_id"] = connID
		logger.Info("WebSocket connection closed", fields)
		g.AddLog("info", "WebSocket closed: "+connID)
	}()

	for {
		_, rawMsg, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				fields := g.wsConnFields(conn)
				fields["conn_id"] = connID
				fields["error"] = err.Error()
				logger.Error("WebSocket read error", fields)
				g.AddLog("error", "WebSocket read error: "+err.Error())
			}
			break
		}

		var wsMsg WSMessage
		if err := json.Unmarshal(rawMsg, &wsMsg); err != nil {
			g.sendWSErrorWithFields(conn, "Invalid message format", logger.Fields{
				"conn_id": connID,
				"action":  "unmarshal",
			})
			g.AddLog("warn", "Invalid message format from "+connID)
			continue
		}

		// 处理消息
		switch wsMsg.Action {
		case "chat":
			g.handleWSChat(conn, connID, wsMsg)
		case "stream":
			g.handleWSStream(conn, connID, wsMsg)
		case "cancel":
			g.handleWSCancel(conn, wsMsg)
		case "get_history":
			g.handleWSHistory(conn, connID, wsMsg)
		case "get_agents":
			g.handleWSAgents(conn)
		case "get_capabilities":
			g.handleWSCapabilities(conn)
		case "get_skills":
			g.handleWSSkills(conn)
		case "get_skill_detail":
			g.handleWSSkillDetail(conn, wsMsg)
		case "get_sessions":
			g.handleWSSessions(conn)
		case "get_settings":
			g.handleWSSettings(conn)
		case "get_settings_full":
			g.handleWSSettingsFull(conn)
		case "validate_settings":
			g.handleWSValidateSettings(conn, wsMsg)
		case "save_settings":
			g.handleWSSaveSettings(conn, wsMsg)
		case "reload_settings":
			g.handleWSReloadSettings(conn)
		case "get_schedule_list", "get_schedules", "get_scheduled_tasks":
			g.handleWSScheduledTasks(conn, wsMsg)
		case "get_schedule_runs", "get_scheduled_task_runs":
			g.handleWSScheduleRuns(conn, wsMsg)
		case "create_schedule", "create_scheduled_task":
			g.handleWSCreateSchedule(conn, wsMsg)
		case "set_schedule_enabled", "set_scheduled_task_enabled":
			g.handleWSSetScheduleEnabled(conn, wsMsg)
		case "trigger_schedule", "trigger_scheduled_task":
			g.handleWSTriggerSchedule(conn, wsMsg)
		case "delete_schedule", "delete_scheduled_task":
			g.handleWSDeleteSchedule(conn, wsMsg)
		case "get_logs":
			g.handleWSLogs(conn, wsMsg)
		case "get_knowledge":
			g.handleWSKnowledge(conn, wsMsg)
		case "get_memory":
			g.handleWSMemory(conn, wsMsg)
		case "get_knowledge_entry":
			g.handleWSKnowledgeEntry(conn, wsMsg)
		case "save_knowledge_entry":
			g.handleWSSaveKnowledgeEntry(conn, wsMsg)
		case "delete_knowledge_entry":
			g.handleWSDeleteKnowledgeEntry(conn, wsMsg)
		case "move_knowledge_entry":
			g.handleWSMoveKnowledgeEntry(conn, wsMsg)
		case "create_knowledge_directory":
			g.handleWSCreateKnowledgeDirectory(conn, wsMsg)
		case "delete_knowledge_directory":
			g.handleWSDeleteKnowledgeDirectory(conn, wsMsg)
		case "get_context_stats":
			g.handleWSContextStats(conn, wsMsg)
		case "compress_context":
			g.handleWSCompressContext(conn, wsMsg)
		case "get_task_status":
			g.handleWSTaskStatus(conn, wsMsg)
		case "ping":
			// 响应 pong
			g.sendWSPong(conn)
			g.AddLog("info", "Ping received from "+connID)
		case "":
			// action 为空，检查是否是旧格式的 ping
			if wsMsg.Type == "ping" {
				g.sendWSPong(conn)
				g.AddLog("info", "Legacy ping received from "+connID)
			} else {
				g.sendWSErrorWithFields(conn, "Unknown action: "+wsMsg.Action, logger.Fields{
					"conn_id": connID,
					"action":  wsMsg.Action,
					"type":    wsMsg.Type,
				})
				g.AddLog("warn", "Unknown action from "+connID)
			}
		default:
			g.sendWSErrorWithFields(conn, "Unknown action: "+wsMsg.Action, logger.Fields{
				"conn_id": connID,
				"action":  wsMsg.Action,
				"user_id": wsMsg.UserID,
			})
			g.AddLog("warn", "Unknown action '"+wsMsg.Action+"' from "+connID)
		}
	}
}

func (g *Gateway) handleWSChat(conn *websocket.Conn, connID string, wsMsg WSMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	var ag *agent.Agent
	var agentID string
	var err error

	if wsMsg.AgentID != "" {
		ag, err = g.router.RouteByAgentID(wsMsg.AgentID)
		if err != nil {
			g.sendWSErrorWithFields(conn, "Agent not found: "+wsMsg.AgentID, logger.Fields{
				"conn_id":  connID,
				"action":   wsMsg.Action,
				"agent_id": wsMsg.AgentID,
				"user_id":  wsMsg.UserID,
			})
			g.AddLog("error", "Chat failed: Agent not found - "+wsMsg.AgentID)
			return
		}
		agentID = wsMsg.AgentID
	} else {
		ag, err = g.router.RouteDefault()
		if err != nil {
			g.sendWSErrorWithFields(conn, "No agent available", logger.Fields{
				"conn_id": connID,
				"action":  wsMsg.Action,
				"user_id": wsMsg.UserID,
			})
			g.AddLog("error", "Chat failed: No agent available")
			return
		}
		agentID = ag.ID
	}

	// 构建或获取 session key
	sessKey := wsMsg.Session

	// 如果前端显式传入了 session，则优先使用它（例如打开 schedule execution session）。
	// 只有未指定 session 时，才回退到默认用户主会话。
	if strings.TrimSpace(sessKey) != "" {
		sessKey = strings.TrimSpace(sessKey)
	} else if wsMsg.UserID != "" {
		sessKey = fmt.Sprintf("agent:%s:user:%s:ws", agentID, wsMsg.UserID)
	} else if sessKey == "" {
		// 无 UserID 且无 Session，拒绝处理
		g.sendWSErrorWithFields(conn, fmt.Sprintf("UserID is required. Message rejected."), logger.Fields{
			"conn_id":     connID,
			"action":      wsMsg.Action,
			"agent_id":    agentID,
			"message_len": len(wsMsg.Message),
		})
		logger.Warn("WebSocket message rejected: no UserID", logger.Fields{
			"conn_id":  connID,
			"agent_id": agentID,
		})
		return
	}

	sess := g.sessionMgr.GetOrCreate(sessKey)
	media, err := g.normalizeIncomingMedia(wsMsg.Media)
	if err != nil {
		g.sendWSErrorWithFields(conn, "Media error: "+err.Error(), logger.Fields{
			"conn_id":    connID,
			"action":     wsMsg.Action,
			"agent_id":   agentID,
			"session_id": sessKey,
			"user_id":    wsMsg.UserID,
		})
		return
	}
	wsMsg.Media = media

	messageContent := strings.TrimSpace(wsMsg.Message)
	if messageContent == "" {
		messageContent = strings.TrimSpace(wsMsg.Content)
	}
	messageContent = sessionOutgoingDisplayContent(&models.OutgoingMessage{Content: messageContent, Media: wsMsg.Media})

	// ⭐ 检测斜杆命令
	if g.slashHandler != nil {
		handled, result, cmdErr := g.slashHandler.Handle(
			conn, connID, sessKey, sess, agentID,
			models.RoleAdmin, // WebChat 默认为 admin 角色
			wsMsg.UserID,     // userID
			messageContent,
		)
		if handled {
			if cmdErr != nil {
				g.sendWSErrorWithFields(conn, cmdErr.Error(), logger.Fields{
					"conn_id":    connID,
					"action":     wsMsg.Action,
					"agent_id":   agentID,
					"session_id": sessKey,
					"user_id":    wsMsg.UserID,
				})
				g.AddLog("warn", "Slash command error: "+cmdErr.Error())
			} else {
				g.sendWSCommandResult(conn, result)
				g.AddLog("info", "Slash command executed: "+wsMsg.Message)
			}
			return
		}
	}

	// 记录请求
	msgPreview := messageContent
	if len(msgPreview) > 50 {
		msgPreview = msgPreview[:50] + "..."
	}
	g.AddLog("info", "Chat request ["+agentID+"] session="+sessKey+": "+msgPreview)

	// 运行 agent
	err = ag.Runtime.RunWithMedia(ctx, sess, messageContent, wsMsg.Media)
	if err != nil {
		g.sendWSErrorWithFields(conn, "Agent error: "+err.Error(), logger.Fields{
			"conn_id":    connID,
			"action":     wsMsg.Action,
			"agent_id":   agentID,
			"session_id": sessKey,
			"user_id":    wsMsg.UserID,
		})
		g.AddLog("error", "Chat failed ["+agentID+"]: "+err.Error())
		return
	}

	// 获取最后一条 assistant 消息
	msgs := sess.GetMessages()
	var lastAssistant string
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && msgs[i].Content != "" {
			lastAssistant = msgs[i].Content
			break
		}
	}

	// 发送响应
	g.sendWSMessage(conn, lastAssistant, nil, true)
	g.AddLog("info", "Chat response ["+agentID+"] session="+sessKey+": completed")

	// 记录连接信息
	g.wsConnMu.Lock()
	g.wsConns[connID] = &WSConnection{
		ConnID:  connID,
		AgentID: agentID,
		UserID:  wsMsg.UserID,
		SessKey: sessKey,
		Conn:    conn,
	}
	g.wsConnMu.Unlock()
}

func (g *Gateway) handleWSStream(conn *websocket.Conn, connID string, wsMsg WSMessage) {
	ctx, cancel := context.WithCancel(context.Background())

	logger.Info("handleWSStream received message", logger.Fields{
		"action":   wsMsg.Action,
		"agent_id": wsMsg.AgentID,
		"user_id":  wsMsg.UserID,
		"session":  wsMsg.Session,
		"message":  wsMsg.Message,
	})

	ag, agentID, sessKey, messageContent, media, err := g.prepareWSStream(conn, connID, wsMsg)
	if err != nil {
		cancel()
		return
	}

	startedAt := time.Now()
	runID := fmt.Sprintf("%s:%d", connID, startedAt.UnixNano())
	g.activeTasksMu.Lock()
	previousTask := g.activeTasks[sessKey]
	g.activeTasks[sessKey] = &ActiveTask{
		RunID:      runID,
		SessionKey: sessKey,
		ConnID:     connID,
		CancelFunc: cancel,
		StartedAt:  startedAt,
	}
	g.activeTasksMu.Unlock()
	if previousTask != nil {
		logger.Debug("Superseding active task with new stream request", logger.Fields{
			"conn_id":          connID,
			"agent_id":         agentID,
			"user_id":          wsMsg.UserID,
			"session_id":       sessKey,
			"previous_conn_id": previousTask.ConnID,
			"previous_run_id":  previousTask.RunID,
			"new_run_id":       runID,
		})
		previousTask.CancelFunc()
	}
	logger.Debug("Active task registered", logger.Fields{
		"conn_id":          connID,
		"agent_id":         agentID,
		"user_id":          wsMsg.UserID,
		"session_id":       sessKey,
		"had_previous":     previousTask != nil,
		"previous_running": previousTask != nil,
		"run_id":           runID,
	})

	go g.runWSStream(conn, connID, wsMsg, ag, agentID, sessKey, messageContent, media, ctx, cancel, startedAt, runID)
}

func (g *Gateway) prepareWSStream(conn *websocket.Conn, connID string, wsMsg WSMessage) (*agent.Agent, string, string, string, []models.OutgoingMedia, error) {
	var ag *agent.Agent
	var agentID string
	var err error

	if wsMsg.AgentID != "" {
		ag, err = g.router.RouteByAgentID(wsMsg.AgentID)
		if err != nil {
			g.sendWSErrorWithFields(conn, "Agent not found: "+wsMsg.AgentID, logger.Fields{
				"conn_id":  connID,
				"action":   wsMsg.Action,
				"agent_id": wsMsg.AgentID,
				"user_id":  wsMsg.UserID,
			})
			g.AddLog("error", "Stream failed: Agent not found - "+wsMsg.AgentID)
			return nil, "", "", "", nil, err
		}
		agentID = wsMsg.AgentID
	} else {
		ag, err = g.router.RouteDefault()
		if err != nil {
			g.sendWSErrorWithFields(conn, "No agent available", logger.Fields{
				"conn_id": connID,
				"action":  wsMsg.Action,
				"user_id": wsMsg.UserID,
			})
			g.AddLog("error", "Stream failed: No agent available")
			return nil, "", "", "", nil, err
		}
		agentID = ag.ID
	}

	sessKey := wsMsg.Session
	if strings.TrimSpace(sessKey) != "" {
		sessKey = strings.TrimSpace(sessKey)
	} else if wsMsg.UserID != "" {
		sessKey = fmt.Sprintf("agent:%s:user:%s:ws", agentID, wsMsg.UserID)
	} else if sessKey == "" {
		g.sendWSErrorWithFields(conn, "UserID is required. Message rejected.", logger.Fields{
			"conn_id":     connID,
			"action":      wsMsg.Action,
			"agent_id":    agentID,
			"message_len": len(wsMsg.Message),
		})
		logger.Warn("WebSocket message rejected: no UserID", logger.Fields{
			"conn_id":  connID,
			"agent_id": agentID,
		})
		return nil, "", "", "", nil, fmt.Errorf("user id required")
	}

	media, err := g.normalizeIncomingMedia(wsMsg.Media)
	if err != nil {
		g.sendWSErrorWithFields(conn, "Media error: "+err.Error(), logger.Fields{
			"conn_id":    connID,
			"action":     wsMsg.Action,
			"agent_id":   agentID,
			"session_id": sessKey,
			"user_id":    wsMsg.UserID,
		})
		return nil, "", "", "", nil, err
	}
	wsMsg.Media = media

	messageContent := strings.TrimSpace(wsMsg.Message)
	if messageContent == "" {
		messageContent = strings.TrimSpace(wsMsg.Content)
	}
	messageContent = sessionOutgoingDisplayContent(&models.OutgoingMessage{Content: messageContent, Media: wsMsg.Media})

	sess := g.sessionMgr.GetOrCreate(sessKey)
	if g.slashHandler != nil {
		handled, result, cmdErr := g.slashHandler.Handle(
			conn, connID, sessKey, sess, agentID,
			models.RoleAdmin,
			wsMsg.UserID,
			messageContent,
		)
		if handled {
			if cmdErr != nil {
				g.sendWSErrorWithFields(conn, cmdErr.Error(), logger.Fields{
					"conn_id":    connID,
					"action":     wsMsg.Action,
					"agent_id":   agentID,
					"session_id": sessKey,
					"user_id":    wsMsg.UserID,
				})
				g.AddLog("warn", "Slash command error: "+cmdErr.Error())
			} else {
				g.sendWSCommandResult(conn, result)
				g.AddLog("info", "Slash command executed: "+wsMsg.Message)
			}
			return nil, "", "", "", nil, fmt.Errorf("slash command handled")
		}
	}

	return ag, agentID, sessKey, messageContent, media, nil
}

func (g *Gateway) runWSStream(conn *websocket.Conn, connID string, wsMsg WSMessage, ag *agent.Agent, agentID, sessKey, messageContent string, media []models.OutgoingMedia, ctx context.Context, cancel context.CancelFunc, startedAt time.Time, runID string) {
	defer cancel()
	defer func() {
		cancelReason := "stream_completed"
		if err := ctx.Err(); err != nil {
			cancelReason = err.Error()
		}
		g.activeTasksMu.Lock()
		currentTask := g.activeTasks[sessKey]
		if currentTask != nil && currentTask.RunID == runID {
			delete(g.activeTasks, sessKey)
		}
		g.activeTasksMu.Unlock()
		logger.Debug("Active task cleaned up", logger.Fields{
			"conn_id":      connID,
			"agent_id":     agentID,
			"user_id":      wsMsg.UserID,
			"session_id":   sessKey,
			"lifetime_sec": time.Since(startedAt).Seconds(),
			"reason":       cancelReason,
			"run_id":       runID,
		})
	}()

	msgPreview := messageContent
	if len(msgPreview) > 50 {
		msgPreview = msgPreview[:50] + "..."
	}
	g.AddLog("info", "Stream request ["+agentID+"] session="+sessKey+": "+msgPreview)

	maxIter := ag.Config.MaxIterations
	if maxIter <= 0 {
		maxIter = 100
	}
	config := models.StreamConfig{MaxIterations: maxIter, SendToolEvents: true}

	logger.Debug("Starting session stream", logger.Fields{
		"conn_id":        connID,
		"agent_id":       agentID,
		"user_id":        wsMsg.UserID,
		"session_id":     sessKey,
		"max_iterations": config.MaxIterations,
	})
	stream, err := g.runSessionInputWithMedia(ctx, agentID, sessKey, messageContent, media, config)
	if err != nil {
		g.sendWSErrorWithFields(conn, "Stream error: "+err.Error(), logger.Fields{
			"conn_id":    connID,
			"action":     wsMsg.Action,
			"agent_id":   agentID,
			"session_id": sessKey,
			"user_id":    wsMsg.UserID,
		})
		g.AddLog("error", "Stream failed ["+agentID+"]: "+err.Error())
		return
	}

	for event := range stream {
		if !g.isCurrentActiveTask(sessKey, runID) {
			logger.Debug("Dropping stale stream event from superseded task", logger.Fields{
				"conn_id":    connID,
				"agent_id":   agentID,
				"user_id":    wsMsg.UserID,
				"session_id": sessKey,
				"run_id":     runID,
				"event_type": event.Type,
			})
			continue
		}
		resp := WSResponse{
			Type:            event.Type,
			Content:         event.Content,
			ThinkingContent: event.ThinkingContent,
			ToolID:          event.ToolID,
			ToolName:        event.ToolName,
			ToolParams:      event.ToolParams,
			ToolResult:      event.ToolResult,
			Iteration:       event.Iteration,
			Plan:            event.Plan,
			Done:            event.Done,
		}
		if event.Error != nil {
			resp.Type = "error"
			resp.Content = event.Error.Error()
			resp.Done = true
		}

		data, _ := json.Marshal(resp)
		logger.Debug("Forwarding stream event", logger.Fields{
			"conn_id":    connID,
			"agent_id":   agentID,
			"user_id":    wsMsg.UserID,
			"session_id": sessKey,
			"event_type": resp.Type,
			"done":       resp.Done,
			"iteration":  resp.Iteration,
		})
		if !g.writeWSJSON(conn, data, logger.Fields{
			"action":     wsMsg.Action,
			"agent_id":   agentID,
			"session_id": sessKey,
			"user_id":    wsMsg.UserID,
			"event_type": resp.Type,
		}) {
			logger.Warn("Stopped forwarding stream events because websocket write failed", logger.Fields{
				"conn_id":    connID,
				"agent_id":   agentID,
				"user_id":    wsMsg.UserID,
				"session_id": sessKey,
				"event_type": resp.Type,
			})
			cancel()
			return
		}
		if event.Done {
			logger.Debug("Stream loop observed terminal event", logger.Fields{
				"conn_id":    connID,
				"agent_id":   agentID,
				"user_id":    wsMsg.UserID,
				"session_id": sessKey,
				"event_type": resp.Type,
			})
			g.AddLog("info", "Stream completed ["+agentID+"] session="+sessKey)
			return
		}
	}
}

func (g *Gateway) sendWSMessage(conn *websocket.Conn, content string, media []models.OutgoingMedia, done bool) {
	resp := WSResponse{
		Type:    "message",
		Content: content,
		Media:   append([]models.OutgoingMedia(nil), media...),
		Done:    done,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": resp.Type})
}

func (g *Gateway) sendWSMessageToSession(sessionKey, content string, media []models.OutgoingMedia, done bool) int {
	if strings.TrimSpace(sessionKey) == "" {
		return 0
	}
	resp := WSResponse{
		Type:    "message",
		Content: content,
		Media:   append([]models.OutgoingMedia(nil), media...),
		Done:    done,
	}
	data, _ := json.Marshal(resp)
	sent := 0
	for _, wsConn := range g.wsSessionConnections(sessionKey) {
		conn, ok := wsConn.Conn.(*websocket.Conn)
		if !ok || conn == nil {
			continue
		}
		if g.writeWSJSON(conn, data, logger.Fields{"response_type": resp.Type, "session_id": sessionKey}) {
			sent++
		}
	}
	return sent
}

func (g *Gateway) sendWSEventToSession(sessionKey string, resp WSResponse) int {
	if strings.TrimSpace(sessionKey) == "" {
		return 0
	}
	data, _ := json.Marshal(resp)
	sent := 0
	for _, wsConn := range g.wsSessionConnections(sessionKey) {
		conn, ok := wsConn.Conn.(*websocket.Conn)
		if !ok || conn == nil {
			continue
		}
		if g.writeWSJSON(conn, data, logger.Fields{"response_type": resp.Type, "session_id": sessionKey}) {
			sent++
		}
	}
	return sent
}

func (g *Gateway) sendWSStream(conn *websocket.Conn, content string, done bool) {
	resp := WSResponse{
		Type:    "stream",
		Content: content,
		Done:    done,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": resp.Type})
}

func (g *Gateway) writeWSJSON(conn *websocket.Conn, data []byte, fields logger.Fields) bool {
	wsConn := g.lookupWSConnection(conn)
	if wsConn != nil {
		wsConn.WriteMu.Lock()
		defer wsConn.WriteMu.Unlock()
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		logFields := g.wsConnFields(conn)
		logFields["error"] = err.Error()
		for k, v := range fields {
			logFields[k] = v
		}
		logger.Error("WebSocket write failed", logFields)
		return false
	}
	return true
}

func (g *Gateway) lookupWSConnection(conn *websocket.Conn) *WSConnection {
	g.wsConnMu.RLock()
	defer g.wsConnMu.RUnlock()
	for _, wsConn := range g.wsConns {
		storedConn, ok := wsConn.Conn.(*websocket.Conn)
		if ok && storedConn == conn {
			return wsConn
		}
	}
	return nil
}

func (g *Gateway) isCurrentActiveTask(sessionKey, runID string) bool {
	g.activeTasksMu.RLock()
	defer g.activeTasksMu.RUnlock()
	task := g.activeTasks[sessionKey]
	return task != nil && task.RunID == runID
}

func (g *Gateway) wsSessionConnections(sessionKey string) []*WSConnection {
	g.wsConnMu.RLock()
	defer g.wsConnMu.RUnlock()
	matches := make([]*WSConnection, 0)
	for _, wsConn := range g.wsConns {
		if wsConn == nil || strings.TrimSpace(wsConn.SessKey) != sessionKey {
			continue
		}
		matches = append(matches, wsConn)
	}
	return matches
}

func (g *Gateway) wsConnFields(conn *websocket.Conn) logger.Fields {
	fields := logger.Fields{}
	wsConn := g.lookupWSConnection(conn)
	if wsConn == nil {
		return fields
	}
	if wsConn.ConnID != "" {
		fields["conn_id"] = wsConn.ConnID
	}
	if wsConn.AgentID != "" {
		fields["agent_id"] = wsConn.AgentID
	}
	if wsConn.UserID != "" {
		fields["user_id"] = wsConn.UserID
	}
	if wsConn.SessKey != "" {
		fields["session_id"] = wsConn.SessKey
	}
	return fields
}

func (g *Gateway) sendWSError(conn *websocket.Conn, errMsg string) {
	g.sendWSErrorWithFields(conn, errMsg, nil)
}

func (g *Gateway) sendWSErrorWithFields(conn *websocket.Conn, errMsg string, fields logger.Fields) {
	logFields := logger.Fields{"error": errMsg}
	for k, v := range fields {
		logFields[k] = v
	}
	logger.Warn("WebSocket request failed", logFields)
	resp := WSResponse{
		Type:    "error",
		Content: errMsg,
		Done:    true,
	}
	data, _ := json.Marshal(resp)
	logFields["response_type"] = resp.Type
	g.writeWSJSON(conn, data, logFields)
}

// sendWSCommandResult 发送斜杆命令结果
func (g *Gateway) sendWSCommandResult(conn *websocket.Conn, result *command.Result) {
	resp := map[string]interface{}{
		"type":    "command",
		"content": result.Content,
		"done":    true,
	}
	if result.ActionType != "" {
		resp["action"] = result.ActionType
	}
	if result.ActionData != nil {
		resp["action_data"] = result.ActionData
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": "command"})
}

func (g *Gateway) sendWSPong(conn *websocket.Conn) {
	resp := WSResponse{
		Type: "pong",
		Done: true,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": resp.Type})
}

// handleWSCancel 取消指定 session 的活动任务
func (g *Gateway) handleWSCancel(conn *websocket.Conn, wsMsg WSMessage) {
	sessKey := strings.TrimSpace(wsMsg.Session)
	if sessKey == "" && wsMsg.UserID != "" && wsMsg.AgentID != "" {
		sessKey = fmt.Sprintf("agent:%s:user:%s:ws", wsMsg.AgentID, wsMsg.UserID)
	}

	logger.Debug("Cancel request received", logger.Fields{
		"action":           wsMsg.Action,
		"raw_session_id":   wsMsg.Session,
		"resolved_session": sessKey,
		"user_id":          wsMsg.UserID,
		"agent_id":         wsMsg.AgentID,
	})

	if sessKey == "" {
		g.sendWSErrorWithFields(conn, "Session or UserID+AgentID is required", logger.Fields{
			"action":   wsMsg.Action,
			"user_id":  wsMsg.UserID,
			"agent_id": wsMsg.AgentID,
		})
		return
	}

	g.activeTasksMu.RLock()
	activeCount := len(g.activeTasks)
	task := g.activeTasks[sessKey]
	g.activeTasksMu.RUnlock()
	logger.Debug("Cancel lookup completed", logger.Fields{
		"session_id":   sessKey,
		"found":        task != nil,
		"active_tasks": activeCount,
		"agent_id":     wsMsg.AgentID,
		"user_id":      wsMsg.UserID,
	})

	if task == nil {
		resp := map[string]interface{}{
			"type":    "error",
			"content": "任务已结束或不存在",
			"done":    true,
		}
		data, _ := json.Marshal(resp)
		g.writeWSJSON(conn, data, logger.Fields{"action": wsMsg.Action, "session_id": sessKey, "response_type": "error"})
		return
	}

	logger.Debug("Invoking active task cancel func", logger.Fields{
		"session_id":   sessKey,
		"agent_id":     wsMsg.AgentID,
		"user_id":      wsMsg.UserID,
		"duration_sec": time.Since(task.StartedAt).Seconds(),
	})
	task.CancelFunc()

	logger.Info("Task cancelled", logger.Fields{
		"session_key": sessKey,
		"duration":    time.Since(task.StartedAt).Seconds(),
	})

	resp := map[string]interface{}{
		"type":    "cancelled",
		"content": "任务已取消",
		"done":    true,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"action": wsMsg.Action, "session_id": sessKey, "response_type": "cancelled"})
}

// HistoryMessage 历史消息
type HistoryMessage struct {
	Role      string                 `json:"role"`    // "user" or "assistant"
	Content   string                 `json:"content"` // 消息内容
	Media     []models.OutgoingMedia `json:"media,omitempty"`
	Timestamp int64                  `json:"timestamp"` // 时间戳
}

func (g *Gateway) handleWSHistory(conn *websocket.Conn, connID string, wsMsg WSMessage) {
	// 构建或获取 session key
	sessKey := wsMsg.Session

	// 与 handleWSStream/handleWSChat 保持一致：如果前端显式传入了 session，优先使用它。
	// 这允许 Web 从 schedule 页面直接打开 execution session 查看历史。
	if strings.TrimSpace(sessKey) != "" {
		sessKey = strings.TrimSpace(sessKey)
	} else if wsMsg.UserID != "" && wsMsg.AgentID != "" {
		sessKey = fmt.Sprintf("agent:%s:user:%s:ws", wsMsg.AgentID, wsMsg.UserID)
	} else if wsMsg.UserID != "" {
		sessKey = fmt.Sprintf("agent:unknown:user:%s:ws", wsMsg.UserID)
	} else if sessKey == "" {
		g.sendWSErrorWithFields(conn, "UserID is required for history. No user identity provided.", logger.Fields{
			"conn_id": connID,
			"action":  wsMsg.Action,
		})
		logger.Warn("WebSocket history rejected: no UserID", logger.Fields{"conn_id": connID})
		return
	}

	logger.Debug("History request received", logger.Fields{
		"conn_id":  connID,
		"req_user": wsMsg.UserID,
		"req_agt":  wsMsg.AgentID,
		"sessKey":  sessKey,
	})

	g.wsConnMu.Lock()
	if existing, ok := g.wsConns[connID]; ok && existing != nil {
		existing.AgentID = strings.TrimSpace(wsMsg.AgentID)
		existing.UserID = strings.TrimSpace(wsMsg.UserID)
		existing.SessKey = sessKey
	}
	g.wsConnMu.Unlock()

	sess := g.sessionMgr.GetOrCreate(sessKey)
	if sess == nil {
		g.sendWSErrorWithFields(conn, "Session not found", logger.Fields{
			"conn_id":    connID,
			"action":     wsMsg.Action,
			"session_id": sessKey,
			"user_id":    wsMsg.UserID,
			"agent_id":   wsMsg.AgentID,
		})
		g.AddLog("error", "History failed: Session not found - "+sessKey)
		return
	}

	// 获取历史消息
	msgs := sess.GetMessages()
	logger.Debug("History: session loaded", logger.Fields{
		"session_key":  sessKey,
		"session_msgs": len(msgs),
		"req_user_id":  wsMsg.UserID,
		"req_agent_id": wsMsg.AgentID,
	})
	limit := wsMsg.Limit
	if limit <= 0 || limit > len(msgs) {
		limit = len(msgs)
	}

	// 转换为历史消息格式
	start := len(msgs) - limit
	if start < 0 {
		start = 0
	}

	history := make([]HistoryMessage, 0, limit)
	skippedCount := 0
	for i := start; i < len(msgs); i++ {
		if !shouldIncludeHistoryMessage(msgs[i]) {
			skippedCount++
			continue
		}

		history = append(history, HistoryMessage{
			Role:      msgs[i].Role,
			Content:   historyMessageContent(msgs[i]),
			Media:     append([]models.OutgoingMedia(nil), msgs[i].Media...),
			Timestamp: msgs[i].Timestamp.Unix(),
		})
	}

	// 发送历史消息
	resp := map[string]interface{}{
		"type":     "history",
		"messages": history,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"action": wsMsg.Action, "session_id": sessKey, "user_id": wsMsg.UserID, "response_type": "history"})

	logger.Info("History sent", logger.Fields{
		"conn_id":    connID,
		"session_id": sessKey,
		"user_id":    wsMsg.UserID,
		"agent_id":   wsMsg.AgentID,
		"total_msgs": len(msgs),
		"skipped":    skippedCount,
		"returned":   len(history),
	})
	g.AddLog("info", fmt.Sprintf("History: session=%s total=%d skipped=%d returned=%d", sessKey, len(msgs), skippedCount, len(history)))
}

// handleWSAgents 返回所有 Agent 列表
func (g *Gateway) handleWSAgents(conn *websocket.Conn) {
	agents := g.agentMgr.List()
	list := make([]AgentInfo, 0, len(agents))
	for _, a := range agents {
		list = append(list, AgentInfo{
			ID:        a.ID,
			Role:      a.Config.Role,
			Provider:  a.Config.Provider,
			Model:     a.Config.Model,
			Workspace: a.Config.Workspace,
			Status:    "active",
		})
	}

	resp := map[string]interface{}{
		"type":   "agents",
		"agents": list,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": "agents"})
	g.AddLog("info", "Agents list sent: "+fmt.Sprintf("%d", len(list))+" agents")
}

// handleWSSkills 返回所有 Skill 列表
func (g *Gateway) handleWSSkills(conn *websocket.Conn) {
	agents := g.agentMgr.List()
	skillMap := make(map[string]SkillInfo)

	for _, a := range agents {
		skills := a.Skills.List()
		for _, s := range skills {
			skillMap[s.Name] = SkillInfo{
				Name:        s.Name,
				Description: s.Description,
				Role:        string(s.Role),
				Tags:        s.Tags,
			}
		}
	}

	list := make([]SkillInfo, 0, len(skillMap))
	for _, s := range skillMap {
		list = append(list, s)
	}

	resp := map[string]interface{}{
		"type":   "skills",
		"skills": list,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": "skills"})
	g.AddLog("info", "Skills list sent: "+fmt.Sprintf("%d", len(list))+" skills")
}

func (g *Gateway) handleWSCapabilities(conn *websocket.Conn) {
	audit := g.GetCapabilityAudit()
	resp := map[string]interface{}{
		"type":         "capabilities",
		"capabilities": audit,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": "capabilities"})
	if audit != nil {
		g.AddLog("info", fmt.Sprintf("Capabilities sent: status=%s agents=%d providers=%d", audit.Status, len(audit.Agents), len(audit.LLM.Providers)))
	}
}

func (g *Gateway) handleWSSkillDetail(conn *websocket.Conn, wsMsg WSMessage) {
	name := strings.TrimSpace(wsMsg.Name)
	if name == "" {
		g.sendWSError(conn, "Skill name is required")
		return
	}

	agents := g.agentMgr.List()
	for _, a := range agents {
		skill, err := a.Skills.Get(name)
		if err != nil || skill == nil {
			continue
		}

		resp := map[string]interface{}{
			"type": "skill_detail",
			"skill": SkillDetailInfo{
				Name:             skill.Name,
				Description:      skill.Description,
				License:          skill.License,
				Compatibility:    skill.Compatibility,
				Metadata:         skill.Metadata,
				Role:             string(skill.Role),
				Tags:             skill.Tags,
				Location:         skill.Location,
				DeprecatedFields: skill.DeprecatedFields,
				Parameters:       []map[string]interface{}{},
				Content:          skill.Content,
			},
		}
		data, _ := json.Marshal(resp)
		g.writeWSJSON(conn, data, logger.Fields{"response_type": "skill_detail", "skill_name": name})
		g.AddLog("info", "Skill detail sent: "+name)
		return
	}

	g.sendWSError(conn, "Skill not found: "+name)
}

// handleWSSessions 返回所有 Session 列表
func (g *Gateway) handleWSSessions(conn *websocket.Conn) {
	sessions := g.sessionMgr.List()

	resp := map[string]interface{}{
		"type":     "sessions",
		"sessions": sessions,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": "sessions"})
	g.AddLog("info", "Sessions list sent: "+fmt.Sprintf("%d", len(sessions))+" sessions")
}

// handleWSSettings 返回系统配置信息
func (g *Gateway) handleWSSettings(conn *websocket.Conn) {
	cfg := g.displayConfig(g.currentConfig())
	llmProvider := cfg.LLM.DefaultProvider
	llmModel := cfg.LLM.DefaultModel
	if llmModel == "" {
		if pCfg, ok := cfg.LLM.Providers[llmProvider]; ok {
			llmModel = pCfg.DefaultModel
			if llmModel == "" && len(pCfg.Models) > 0 {
				llmModel = strings.TrimSpace(pCfg.Models[0].ID)
			}
		}
	}
	if llmModel == "" {
		llmModel = "unknown"
	}

	info := SettingsInfo{
		Gateway: GatewaySettings{
			Host: cfg.Gateway.Host,
			Port: cfg.Gateway.Port,
		},
		LLM: LLMSettings{
			Provider: llmProvider,
			Model:    llmModel,
		},
		System: SystemSettings{
			Version: currentRuntimeVersion(),
			Uptime:  time.Since(g.startTime).String(),
		},
	}

	resp := map[string]interface{}{
		"type":     "settings",
		"settings": info,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": "settings"})
	g.AddLog("info", "Settings sent: provider="+llmProvider+" model="+llmModel)
}

func (g *Gateway) handleWSSettingsFull(conn *websocket.Conn) {
	configPath, err := g.resolvedConfigPath()
	if err != nil {
		configPath = g.configPath
	}
	resp := map[string]any{
		"type":        "settings_full",
		"settings":    g.configSnapshot(),
		"schema":      settingsSchema(),
		"config_path": configPath,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": "settings_full"})
	g.AddLog("info", "Full settings sent")
}

func (g *Gateway) handleWSValidateSettings(conn *websocket.Conn, wsMsg WSMessage) {
	raw, err := g.settingsPayloadToYAML(wsMsg.Settings)
	if err != nil {
		settingsLog.Warn("Configuration validation payload rejected", logger.Fields{"error": err.Error()})
		resp := map[string]interface{}{
			"type":   "settings_validation",
			"valid":  false,
			"error":  err.Error(),
			"issues": []SettingsValidationIssue{},
		}
		data, _ := json.Marshal(resp)
		g.writeWSJSON(conn, data, logger.Fields{"action": wsMsg.Action, "response_type": "settings_validation"})
		return
	}
	_, issues, err := validateConfigYAML(raw, g.configPath)
	if err != nil {
		settingsLog.Error("Configuration validation failed", logger.Fields{"error": err.Error(), "config_path": g.configPath})
		resp := map[string]interface{}{
			"type":   "settings_validation",
			"valid":  false,
			"error":  err.Error(),
			"issues": []SettingsValidationIssue{},
		}
		data, _ := json.Marshal(resp)
		g.writeWSJSON(conn, data, logger.Fields{"action": wsMsg.Action, "response_type": "settings_validation"})
		return
	}
	resp := map[string]interface{}{
		"type":   "settings_validation",
		"valid":  len(issues) == 0,
		"issues": issues,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"action": wsMsg.Action, "response_type": "settings_validation"})
}

func (g *Gateway) handleWSSaveSettings(conn *websocket.Conn, wsMsg WSMessage) {
	raw, err := g.settingsPayloadToYAML(wsMsg.Settings)
	if err != nil {
		settingsLog.Error("Failed to prepare configuration payload from settings", logger.Fields{"error": err.Error()})
		resp := map[string]interface{}{
			"type":  "settings_save_failed",
			"stage": "prepare",
			"error": err.Error(),
		}
		data, _ := json.Marshal(resp)
		g.writeWSJSON(conn, data, logger.Fields{"action": wsMsg.Action, "response_type": "settings_save_failed", "stage": "prepare"})
		return
	}
	result, issues, err := g.saveConfigYAML(raw)
	if err != nil {
		settingsLog.Error("Configuration save request failed", logger.Fields{"error": err.Error()})
		resp := map[string]interface{}{
			"type":  "settings_save_failed",
			"stage": "save",
			"error": err.Error(),
		}
		data, _ := json.Marshal(resp)
		g.writeWSJSON(conn, data, logger.Fields{"action": wsMsg.Action, "response_type": "settings_save_failed", "stage": "save"})
		return
	}
	if len(issues) > 0 {
		settingsLog.Warn("Configuration save blocked by validation issues", logger.Fields{"issue_count": len(issues)})
		resp := map[string]interface{}{
			"type":   "settings_validation",
			"valid":  false,
			"issues": issues,
		}
		data, _ := json.Marshal(resp)
		g.writeWSJSON(conn, data, logger.Fields{"action": wsMsg.Action, "response_type": "settings_validation"})
		return
	}
	resp := map[string]interface{}{
		"type":   "settings_saved",
		"result": result,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"action": wsMsg.Action, "response_type": "settings_saved"})
	g.broadcastSettingsChanged()
}

func (g *Gateway) handleWSReloadSettings(conn *websocket.Conn) {
	result, issues, err := g.reloadConfigFromDisk()
	if err != nil {
		settingsLog.Error("Configuration reload request failed", logger.Fields{"error": err.Error()})
		resp := map[string]interface{}{
			"type":  "settings_save_failed",
			"stage": "reload",
			"error": err.Error(),
		}
		data, _ := json.Marshal(resp)
		g.writeWSJSON(conn, data, logger.Fields{"response_type": "settings_save_failed", "stage": "reload"})
		return
	}
	if len(issues) > 0 {
		settingsLog.Warn("Configuration reload reported validation issues", logger.Fields{"issue_count": len(issues)})
		resp := map[string]interface{}{
			"type":   "settings_validation",
			"valid":  false,
			"issues": issues,
		}
		data, _ := json.Marshal(resp)
		g.writeWSJSON(conn, data, logger.Fields{"response_type": "settings_validation"})
		return
	}
	resp := map[string]interface{}{
		"type":   "settings_reloaded",
		"result": result,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": "settings_reloaded"})
	g.broadcastSettingsChanged()
}

func (g *Gateway) broadcastSettingsChanged() {
	resp := map[string]interface{}{
		"type":     "settings_changed",
		"settings": g.configSnapshot(),
		"schema":   settingsSchema(),
	}
	data, _ := json.Marshal(resp)

	g.wsConnMu.RLock()
	defer g.wsConnMu.RUnlock()
	for _, wsConn := range g.wsConns {
		conn, ok := wsConn.Conn.(*websocket.Conn)
		if !ok || conn == nil {
			continue
		}
		g.writeWSJSON(conn, data, logger.Fields{"response_type": "settings_changed", "agent_id": wsConn.AgentID, "session_id": wsConn.SessKey})
	}
}

func (g *Gateway) handleWSScheduledTasks(conn *websocket.Conn, wsMsg WSMessage) {
	if wsMsg.UserID == "" {
		g.sendWSErrorWithFields(conn, "UserID is required for schedules", logger.Fields{
			"action":   wsMsg.Action,
			"agent_id": wsMsg.AgentID,
		})
		return
	}
	agentID := wsMsg.AgentID
	if agentID == "" {
		agentID = g.GetDefaultAgentID()
	}
	schedules := g.ListSchedules(agentID, wsMsg.UserID)
	resp := map[string]interface{}{
		"type":      "schedules",
		"schedules": schedules,
		"tasks":     schedules,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, nil)
}

func (g *Gateway) handleWSCreateSchedule(conn *websocket.Conn, wsMsg WSMessage) {
	if wsMsg.UserID == "" {
		g.sendWSErrorWithFields(conn, "UserID is required for schedule creation", logger.Fields{
			"action":   wsMsg.Action,
			"agent_id": wsMsg.AgentID,
		})
		return
	}
	created, err := g.CreateSchedule(wsMsg.AgentID, wsMsg.UserID, models.RoleAdmin, command.CreateScheduleRequest{
		Name:             wsMsg.Name,
		Description:      wsMsg.Description,
		Schedule:         wsMsg.Schedule,
		Prompt:           wsMsg.Prompt,
		Enabled:          wsMsg.Enabled,
		OriginSessionKey: g.wsSessionKeyForUser(conn, wsMsg.AgentID, wsMsg.UserID),
	})
	if err != nil {
		g.sendWSErrorWithFields(conn, err.Error(), logger.Fields{
			"action":   wsMsg.Action,
			"agent_id": wsMsg.AgentID,
			"user_id":  wsMsg.UserID,
			"name":     wsMsg.Name,
		})
		return
	}
	resp := map[string]interface{}{
		"type":     "schedule_created",
		"schedule": created,
		"task":     created,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, nil)
}

func (g *Gateway) wsSessionKeyForUser(conn *websocket.Conn, agentID, userID string) string {
	if conn == nil {
		return ""
	}
	resolvedAgentID := strings.TrimSpace(agentID)
	if resolvedAgentID == "" {
		resolvedAgentID = strings.TrimSpace(g.GetDefaultAgentID())
	}
	g.wsConnMu.RLock()
	defer g.wsConnMu.RUnlock()
	for _, wsConn := range g.wsConns {
		if wsConn == nil || wsConn.Conn != conn {
			continue
		}
		if userID != "" && wsConn.UserID != "" && wsConn.UserID != userID {
			continue
		}
		if resolvedAgentID != "" && wsConn.AgentID != "" && wsConn.AgentID != resolvedAgentID {
			continue
		}
		return strings.TrimSpace(wsConn.SessKey)
	}
	if resolvedAgentID != "" && userID != "" {
		return fmt.Sprintf("agent:%s:user:%s:ws", resolvedAgentID, userID)
	}
	return ""
}

func (g *Gateway) handleWSSetScheduleEnabled(conn *websocket.Conn, wsMsg WSMessage) {
	scheduleID := strings.TrimSpace(wsMsg.ScheduleID)
	if scheduleID == "" {
		scheduleID = strings.TrimSpace(wsMsg.LegacyTaskID)
	}
	if wsMsg.UserID == "" {
		g.sendWSErrorWithFields(conn, "UserID is required for schedule update", logger.Fields{
			"action":   wsMsg.Action,
			"agent_id": wsMsg.AgentID,
		})
		return
	}
	if scheduleID == "" {
		g.sendWSErrorWithFields(conn, "schedule_id is required", logger.Fields{
			"action":  wsMsg.Action,
			"user_id": wsMsg.UserID,
		})
		return
	}
	if wsMsg.Enabled == nil {
		g.sendWSErrorWithFields(conn, "enabled is required", logger.Fields{
			"action":      wsMsg.Action,
			"schedule_id": scheduleID,
			"task_id":     scheduleID,
			"user_id":     wsMsg.UserID,
		})
		return
	}
	agentID := wsMsg.AgentID
	if agentID == "" {
		agentID = g.GetDefaultAgentID()
	}
	if err := g.SetScheduleEnabled(agentID, wsMsg.UserID, scheduleID, *wsMsg.Enabled); err != nil {
		g.sendWSErrorWithFields(conn, err.Error(), logger.Fields{
			"action":      wsMsg.Action,
			"agent_id":    agentID,
			"user_id":     wsMsg.UserID,
			"schedule_id": scheduleID,
			"task_id":     scheduleID,
			"enabled":     *wsMsg.Enabled,
		})
		return
	}
	resp := map[string]interface{}{
		"type":        "schedule_updated",
		"schedule_id": scheduleID,
		"task_id":     scheduleID,
		"enabled":     *wsMsg.Enabled,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, nil)
}

func (g *Gateway) handleWSDeleteSchedule(conn *websocket.Conn, wsMsg WSMessage) {
	scheduleID := strings.TrimSpace(wsMsg.ScheduleID)
	if scheduleID == "" {
		scheduleID = strings.TrimSpace(wsMsg.LegacyTaskID)
	}
	if wsMsg.UserID == "" {
		g.sendWSErrorWithFields(conn, "UserID is required for schedule deletion", logger.Fields{
			"action":   wsMsg.Action,
			"agent_id": wsMsg.AgentID,
		})
		return
	}
	if scheduleID == "" {
		g.sendWSErrorWithFields(conn, "schedule_id is required", logger.Fields{
			"action":  wsMsg.Action,
			"user_id": wsMsg.UserID,
		})
		return
	}
	agentID := wsMsg.AgentID
	if agentID == "" {
		agentID = g.GetDefaultAgentID()
	}
	if err := g.DeleteSchedule(agentID, wsMsg.UserID, scheduleID); err != nil {
		g.sendWSErrorWithFields(conn, err.Error(), logger.Fields{
			"action":      wsMsg.Action,
			"agent_id":    agentID,
			"user_id":     wsMsg.UserID,
			"schedule_id": scheduleID,
			"task_id":     scheduleID,
		})
		return
	}
	resp := map[string]interface{}{
		"type":        "schedule_deleted",
		"schedule_id": scheduleID,
		"task_id":     scheduleID,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, nil)
}

func (g *Gateway) handleWSScheduleRuns(conn *websocket.Conn, wsMsg WSMessage) {
	if wsMsg.UserID == "" {
		g.sendWSErrorWithFields(conn, "UserID is required for schedule runs", logger.Fields{
			"action":   wsMsg.Action,
			"agent_id": wsMsg.AgentID,
		})
		return
	}
	scheduleID := strings.TrimSpace(wsMsg.ScheduleID)
	if scheduleID == "" {
		scheduleID = strings.TrimSpace(wsMsg.LegacyTaskID)
	}
	if scheduleID == "" {
		g.sendWSErrorWithFields(conn, "schedule_id is required", logger.Fields{
			"action":  wsMsg.Action,
			"user_id": wsMsg.UserID,
		})
		return
	}
	agentID := wsMsg.AgentID
	if agentID == "" {
		agentID = g.GetDefaultAgentID()
	}
	limit := wsMsg.Limit
	if limit <= 0 {
		limit = 20
	}
	runs, err := g.ListScheduleRuns(agentID, wsMsg.UserID, scheduleID, limit)
	if err != nil {
		g.sendWSErrorWithFields(conn, err.Error(), logger.Fields{
			"action":      wsMsg.Action,
			"agent_id":    agentID,
			"user_id":     wsMsg.UserID,
			"schedule_id": scheduleID,
		})
		return
	}
	resp := map[string]interface{}{
		"type":        "schedule_runs",
		"schedule_id": scheduleID,
		"runs":        runs,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, nil)
}

func (g *Gateway) handleWSTriggerSchedule(conn *websocket.Conn, wsMsg WSMessage) {
	scheduleID := strings.TrimSpace(wsMsg.ScheduleID)
	if scheduleID == "" {
		scheduleID = strings.TrimSpace(wsMsg.LegacyTaskID)
	}
	if wsMsg.UserID == "" {
		g.sendWSErrorWithFields(conn, "UserID is required for schedule trigger", logger.Fields{
			"action":   wsMsg.Action,
			"agent_id": wsMsg.AgentID,
		})
		return
	}
	if scheduleID == "" {
		g.sendWSErrorWithFields(conn, "schedule_id is required", logger.Fields{
			"action":  wsMsg.Action,
			"user_id": wsMsg.UserID,
		})
		return
	}
	agentID := wsMsg.AgentID
	if agentID == "" {
		agentID = g.GetDefaultAgentID()
	}
	if err := g.TriggerSchedule(agentID, wsMsg.UserID, scheduleID, scheduler.TriggerSourceManual); err != nil {
		g.sendWSErrorWithFields(conn, err.Error(), logger.Fields{
			"action":      wsMsg.Action,
			"agent_id":    agentID,
			"user_id":     wsMsg.UserID,
			"schedule_id": scheduleID,
			"task_id":     scheduleID,
		})
		return
	}
	resp := map[string]interface{}{
		"type":           "schedule_triggered",
		"schedule_id":    scheduleID,
		"task_id":        scheduleID,
		"trigger_source": string(scheduler.TriggerSourceManual),
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, nil)
}

// handleWSLogs 返回日志
func (g *Gateway) handleWSLogs(conn *websocket.Conn, wsMsg WSMessage) {
	level := wsMsg.Level
	if level == "" {
		level = wsMsg.Message // 兼容旧格式，用 message 字段传递 level
	}

	limit := wsMsg.Limit
	if limit <= 0 {
		limit = 100
	}

	if err := logger.Flush(); err != nil {
		g.AddLog("warn", "Logs flush failed before read: "+err.Error())
	}

	logs, err := readLogsFromFiles(filepath.Join(g.currentConfig().DataDir, "logs"), level, limit)
	if err != nil {
		g.sendWSErrorWithFields(conn, "failed to read logs: "+err.Error(), logger.Fields{
			"action": wsMsg.Action,
			"level":  level,
			"limit":  limit,
		})
		g.AddLog("error", "Logs read failed: "+err.Error())
		return
	}

	resp := map[string]interface{}{
		"type": "logs",
		"logs": logs,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, nil)
	g.AddLog("info", "Logs sent from file: level="+level+" count="+fmt.Sprintf("%d", len(logs)))
}

// KnowledgeEntry 知识库条目
type KnowledgeEntry struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Content     string   `json:"content"`
	Source      string   `json:"source"`
	Type        string   `json:"type"` // "memory"
	Category    string   `json:"category,omitempty"`
	AgentID     string   `json:"agent_id,omitempty"`
	UserID      string   `json:"user_id,omitempty"`
	Description string   `json:"description,omitempty"`
	Path        string   `json:"path,omitempty"`
	Directory   string   `json:"directory,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// handleWSMemory 返回记忆内容
func (g *Gateway) handleWSMemory(conn *websocket.Conn, wsMsg WSMessage) {
	entries := make([]KnowledgeEntry, 0)

	agentID := wsMsg.AgentID
	if agentID == "" {
		agentID = g.GetDefaultAgentID()
	}

	a, err := g.agentMgr.Get(agentID)
	if err != nil {
		g.sendWSErrorWithFields(conn, "agent not found: "+agentID, logger.Fields{
			"action":   wsMsg.Action,
			"agent_id": agentID,
			"user_id":  wsMsg.UserID,
		})
		return
	}

	for _, name := range agent.AgentPromptFileWhitelist() {
		agentPath := filepath.Join(a.Config.Workspace, name)
		content, err := readFileContent(agentPath)
		if err != nil || content == "" {
			continue
		}
		title := extractTitle(content, strings.TrimSuffix(name, ".md"))
		entries = append(entries, KnowledgeEntry{
			ID:          a.ID + "-agent-" + strings.ToLower(strings.TrimSuffix(name, ".md")),
			Title:       title,
			Content:     content,
			Source:      agentPath,
			Type:        "memory",
			Category:    "agent_memory",
			AgentID:     a.ID,
			Description: "Prompt-whitelisted agent memory file.",
		})
	}

	if wsMsg.UserID != "" {
		userBaseDir := knowledgeUserBaseDir(g.currentConfig().DataDir, a.ID, wsMsg.UserID)
		userFiles := []struct {
			name        string
			path        string
			description string
		}{
			{
				name:        "USER.md",
				path:        filepath.Join(userBaseDir, "USER.md"),
				description: "User-specific profile and collaboration guidance.",
			},
			{
				name:        "MEMORY.md",
				path:        filepath.Join(userBaseDir, "MEMORY.md"),
				description: "User-specific long-term memory index.",
			},
		}
		for _, userFile := range userFiles {
			content, err := readFileContent(userFile.path)
			if err != nil || content == "" {
				continue
			}
			title := extractTitle(content, userFile.name)
			entries = append(entries, KnowledgeEntry{
				ID:          a.ID + "-" + wsMsg.UserID + "-user-" + strings.ToLower(strings.TrimSuffix(userFile.name, ".md")),
				Title:       title,
				Content:     content,
				Source:      userFile.path,
				Type:        "memory",
				Category:    "user_memory",
				AgentID:     a.ID,
				UserID:      wsMsg.UserID,
				Description: userFile.description,
			})
		}

		for _, mediumPath := range listKnowledgeMediumFiles(filepath.Join(userBaseDir, "memory")) {
			content, err := readFileContent(mediumPath)
			if err != nil || content == "" {
				continue
			}
			title := strings.TrimSuffix(filepath.Base(mediumPath), ".md")
			entries = append(entries, KnowledgeEntry{
				ID:          a.ID + "-" + wsMsg.UserID + "-medium-" + title,
				Title:       title,
				Content:     content,
				Source:      mediumPath,
				Type:        "memory",
				Category:    "medium_memory",
				AgentID:     a.ID,
				UserID:      wsMsg.UserID,
				Description: "Recent medium-term memory for the current user.",
			})
		}
	}

	query := strings.ToLower(wsMsg.Message)
	if query != "" {
		filtered := make([]KnowledgeEntry, 0)
		for _, e := range entries {
			if strings.Contains(strings.ToLower(e.Title), query) ||
				strings.Contains(strings.ToLower(e.Content), query) ||
				strings.Contains(strings.ToLower(e.Description), query) ||
				strings.Contains(strings.ToLower(e.Category), query) ||
				strings.Contains(strings.ToLower(e.UserID), query) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	resp := map[string]interface{}{
		"type":   "memory",
		"memory": entries,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, nil)

	if query != "" {
		g.AddLog("info", "Memory search: query=\""+query+"\" results="+fmt.Sprintf("%d", len(entries)))
	} else {
		g.AddLog("info", "Memory list sent: "+fmt.Sprintf("%d", len(entries))+" entries")
	}
}

// handleWSKnowledge 返回共享知识库内容
func (g *Gateway) handleWSKnowledge(conn *websocket.Conn, wsMsg WSMessage) {
	entries, err := knowledge.ListEntries(g.currentConfig().DataDir, wsMsg.Message)
	if err != nil {
		g.sendWSError(conn, "failed to load knowledge: "+err.Error())
		return
	}
	directories, err := knowledge.ListDirectories(g.currentConfig().DataDir)
	if err != nil {
		g.sendWSError(conn, "failed to load knowledge directories: "+err.Error())
		return
	}

	items := make([]KnowledgeEntry, 0, len(entries))
	for _, entry := range entries {
		description := entry.Directory
		if description == "" {
			description = "Shared knowledge note"
		}
		items = append(items, KnowledgeEntry{
			ID:          entry.ID,
			Title:       entry.Title,
			Content:     entry.Content,
			Source:      filepath.Join(knowledge.RootDir(g.currentConfig().DataDir), filepath.FromSlash(entry.Path)),
			Type:        "knowledge",
			Category:    "knowledge",
			Description: description,
			Path:        entry.Path,
			Directory:   entry.Directory,
			UpdatedAt:   entry.UpdatedAt.Format(time.RFC3339),
			Tags:        entry.Tags,
		})
	}

	resp := map[string]interface{}{
		"type":        "knowledge",
		"knowledge":   items,
		"directories": directories,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": "knowledge"})
	g.AddLog("info", "Knowledge list sent: "+fmt.Sprintf("%d", len(items))+" entries")
}

func (g *Gateway) handleWSKnowledgeEntry(conn *websocket.Conn, wsMsg WSMessage) {
	entry, err := knowledge.ReadEntry(g.currentConfig().DataDir, wsMsg.Path)
	if err != nil {
		g.sendWSError(conn, "failed to read knowledge entry: "+err.Error())
		return
	}

	resp := map[string]interface{}{
		"type": "knowledge_entry",
		"entry": KnowledgeEntry{
			ID:        entry.ID,
			Title:     entry.Title,
			Content:   entry.Content,
			Source:    filepath.Join(knowledge.RootDir(g.currentConfig().DataDir), filepath.FromSlash(entry.Path)),
			Type:      "knowledge",
			Category:  "knowledge",
			Path:      entry.Path,
			Directory: entry.Directory,
			UpdatedAt: entry.UpdatedAt.Format(time.RFC3339),
			Tags:      entry.Tags,
		},
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": "knowledge_entry", "path": wsMsg.Path})
}

func (g *Gateway) handleWSSaveKnowledgeEntry(conn *websocket.Conn, wsMsg WSMessage) {
	entry, err := knowledge.WriteEntry(g.currentConfig().DataDir, knowledge.WriteInput{
		Path:    wsMsg.Path,
		Title:   wsMsg.Name,
		Tags:    wsMsg.Tags,
		Content: wsMsg.Message,
	})
	if err != nil {
		g.sendWSError(conn, "failed to save knowledge entry: "+err.Error())
		return
	}

	resp := map[string]interface{}{
		"type": "knowledge_entry_saved",
		"entry": KnowledgeEntry{
			ID:        entry.ID,
			Title:     entry.Title,
			Content:   entry.Content,
			Source:    filepath.Join(knowledge.RootDir(g.currentConfig().DataDir), filepath.FromSlash(entry.Path)),
			Type:      "knowledge",
			Category:  "knowledge",
			Path:      entry.Path,
			Directory: entry.Directory,
			UpdatedAt: entry.UpdatedAt.Format(time.RFC3339),
			Tags:      entry.Tags,
		},
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": "knowledge_entry_saved", "path": entry.Path})
	g.AddLog("info", "Knowledge entry saved: "+entry.Path)
}

func (g *Gateway) handleWSDeleteKnowledgeEntry(conn *websocket.Conn, wsMsg WSMessage) {
	if strings.TrimSpace(wsMsg.Path) == "" {
		g.sendWSError(conn, "knowledge path is required")
		return
	}
	if err := knowledge.DeleteEntry(g.currentConfig().DataDir, wsMsg.Path); err != nil {
		g.sendWSError(conn, "failed to delete knowledge entry: "+err.Error())
		return
	}

	resp := map[string]interface{}{
		"type": "knowledge_entry_deleted",
		"path": wsMsg.Path,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": "knowledge_entry_deleted", "path": wsMsg.Path})
	g.AddLog("info", "Knowledge entry deleted: "+wsMsg.Path)
}

func (g *Gateway) handleWSMoveKnowledgeEntry(conn *websocket.Conn, wsMsg WSMessage) {
	if strings.TrimSpace(wsMsg.FromPath) == "" {
		g.sendWSError(conn, "source knowledge path is required")
		return
	}
	if strings.TrimSpace(wsMsg.Path) == "" {
		g.sendWSError(conn, "target knowledge path is required")
		return
	}

	entry, err := knowledge.MoveEntry(g.currentConfig().DataDir, wsMsg.FromPath, wsMsg.Path)
	if err != nil {
		g.sendWSError(conn, "failed to move knowledge entry: "+err.Error())
		return
	}

	resp := map[string]interface{}{
		"type":      "knowledge_entry_moved",
		"from_path": wsMsg.FromPath,
		"entry": KnowledgeEntry{
			ID:        entry.ID,
			Title:     entry.Title,
			Content:   entry.Content,
			Source:    filepath.Join(knowledge.RootDir(g.currentConfig().DataDir), filepath.FromSlash(entry.Path)),
			Type:      "knowledge",
			Category:  "knowledge",
			Path:      entry.Path,
			Directory: entry.Directory,
			UpdatedAt: entry.UpdatedAt.Format(time.RFC3339),
			Tags:      entry.Tags,
		},
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": "knowledge_entry_moved", "from_path": wsMsg.FromPath, "path": entry.Path})
	g.AddLog("info", "Knowledge entry moved: "+wsMsg.FromPath+" -> "+entry.Path)
}

func (g *Gateway) handleWSCreateKnowledgeDirectory(conn *websocket.Conn, wsMsg WSMessage) {
	if strings.TrimSpace(wsMsg.Path) == "" {
		g.sendWSError(conn, "knowledge directory path is required")
		return
	}

	dir, err := knowledge.CreateDirectory(g.currentConfig().DataDir, wsMsg.Path)
	if err != nil {
		g.sendWSError(conn, "failed to create knowledge directory: "+err.Error())
		return
	}

	resp := map[string]interface{}{
		"type":      "knowledge_directory_created",
		"directory": dir,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": "knowledge_directory_created", "directory": dir})
	g.AddLog("info", "Knowledge directory created: "+dir)
}

func (g *Gateway) handleWSDeleteKnowledgeDirectory(conn *websocket.Conn, wsMsg WSMessage) {
	if strings.TrimSpace(wsMsg.Path) == "" {
		g.sendWSError(conn, "knowledge directory path is required")
		return
	}

	dir, err := knowledge.DeleteDirectory(g.currentConfig().DataDir, wsMsg.Path)
	if err != nil {
		g.sendWSError(conn, "failed to delete knowledge directory: "+err.Error())
		return
	}

	resp := map[string]interface{}{
		"type":      "knowledge_directory_deleted",
		"directory": dir,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": "knowledge_directory_deleted", "directory": dir})
	g.AddLog("info", "Knowledge directory deleted: "+dir)
}

func readLogsFromFiles(logDir, level string, limit int) ([]LogEntry, error) {
	logFiles, err := listLogFiles(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []LogEntry{}, nil
		}
		return nil, err
	}
	if len(logFiles) == 0 {
		return []LogEntry{}, nil
	}

	entries := make([]LogEntry, 0, limit)
	for _, path := range logFiles {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		parsed := parseLogEntries(string(content), level)
		if len(parsed) == 0 {
			continue
		}

		remaining := limit - len(entries)
		if remaining <= 0 {
			break
		}
		if len(parsed) > remaining {
			parsed = parsed[len(parsed)-remaining:]
		}
		entries = append(parsed, entries...)
		if len(entries) >= limit {
			break
		}
	}

	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}
	return entries, nil
}

func listLogFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".log") {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}

	sort.Slice(files, func(i, j int) bool {
		return filepath.Base(files[i]) > filepath.Base(files[j])
	})
	return files, nil
}

func parseLogEntries(content, level string) []LogEntry {
	lines := strings.Split(content, "\n")
	entries := make([]LogEntry, 0)
	level = strings.ToUpper(strings.TrimSpace(level))

	var current *LogEntry
	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		line = stripANSIEscapeCodes(line)
		if line == "" {
			continue
		}

		if entry, ok := parsePrimaryLogLine(line); ok {
			if current != nil && matchesLogLevel(current.Level, level) {
				entries = append(entries, *current)
			}
			current = &entry
			continue
		}

		if current == nil {
			continue
		}

		continuation := strings.TrimSpace(line)
		continuation = strings.TrimPrefix(continuation, "└─ ")
		continuation = strings.TrimPrefix(continuation, "└─")
		if continuation == "" {
			continue
		}
		current.Message += "\n" + continuation
	}

	if current != nil && matchesLogLevel(current.Level, level) {
		entries = append(entries, *current)
	}
	return entries
}

func parsePrimaryLogLine(line string) (LogEntry, bool) {
	matches := logEntryLinePattern.FindStringSubmatch(line)
	if matches == nil {
		return LogEntry{}, false
	}

	return LogEntry{
		Time:    matches[2],
		Level:   strings.ToLower(matches[3]),
		Message: strings.TrimSpace(matches[4]),
	}, true
}

func matchesLogLevel(entryLevel, filterLevel string) bool {
	if filterLevel == "" || filterLevel == "ALL" {
		return true
	}
	return strings.EqualFold(entryLevel, filterLevel)
}

func stripANSIEscapeCodes(value string) string {
	return ansiEscapePattern.ReplaceAllString(value, "")
}

func sanitizeKnowledgeID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\\", "_")
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, ":", "_")
	value = strings.ReplaceAll(value, " ", "_")
	return value
}

func knowledgeUserBaseDir(dataDir, agentID, userID string) string {
	return filepath.Join(dataDir, "users", fmt.Sprintf("%s_user_%s", sanitizeKnowledgeID(agentID), sanitizeKnowledgeID(userID)))
}

func listKnowledgeMediumFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	return files
}

// readFileContent 读取文件内容
func readFileContent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// extractTitle 从内容提取标题
func extractTitle(content, defaultTitle string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// 跳过空行和 YAML front matter
		if line == "" || line == "---" {
			continue
		}
		// Markdown 标题
		if strings.HasPrefix(line, "#") {
			return strings.TrimPrefix(line, "# ")
		}
		// 非标题内容，截取前50字符
		if len(line) > 50 {
			return line[:50] + "..."
		}
		return line
	}
	return defaultTitle
}

// collectSkills 收集 Skills 目录下的所有 SKILL.md
func collectSkills(skillsDir string) []KnowledgeEntry {
	entries := make([]KnowledgeEntry, 0)

	dirs, err := os.ReadDir(skillsDir)
	if err != nil {
		return entries
	}

	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}

		skillPath := filepath.Join(skillsDir, dir.Name(), "SKILL.md")
		if content, err := readFileContent(skillPath); err == nil && content != "" {
			title := extractTitle(content, dir.Name())
			description := extractDescription(content)
			entries = append(entries, KnowledgeEntry{
				ID:          "skill-" + dir.Name(),
				Title:       title,
				Content:     content,
				Source:      skillPath,
				Type:        "skill",
				Description: description,
			})
		}
	}

	return entries
}

// extractDescription 从 SKILL.md 提取描述
func extractDescription(content string) string {
	lines := strings.Split(content, "\n")
	inYAML := false
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// YAML front matter
		if line == "---" {
			inYAML = !inYAML
			continue
		}

		// YAML 内的 description
		if inYAML && strings.HasPrefix(line, "description:") {
			return strings.TrimPrefix(line, "description: ")
		}

		// 跳过标题，找正文
		if strings.HasPrefix(line, "#") {
			continue
		}

		// 第一段正文
		if line != "" && !inYAML {
			if len(line) > 100 {
				return line[:100] + "..."
			}
			return line
		}
	}
	return ""
}

// handleWSContextStats 返回上下文统计信息
func (g *Gateway) handleWSContextStats(conn *websocket.Conn, wsMsg WSMessage) {
	// 构建 session key
	sessKey := wsMsg.Session
	if wsMsg.UserID != "" && wsMsg.AgentID != "" {
		sessKey = fmt.Sprintf("agent:%s:user:%s:ws", wsMsg.AgentID, wsMsg.UserID)
	} else if sessKey == "" {
		g.sendWSErrorWithFields(conn, "Session or UserID+AgentID is required", logger.Fields{
			"action":   wsMsg.Action,
			"user_id":  wsMsg.UserID,
			"agent_id": wsMsg.AgentID,
		})
		return
	}

	sess := g.sessionMgr.GetOrCreate(sessKey)
	if sess == nil {
		g.sendWSErrorWithFields(conn, "Session not found", logger.Fields{
			"action":     wsMsg.Action,
			"session_id": sessKey,
			"user_id":    wsMsg.UserID,
			"agent_id":   wsMsg.AgentID,
		})
		return
	}

	// 获取对应的 Agent
	var ag *agent.Agent
	var err error
	if wsMsg.AgentID != "" {
		ag, err = g.router.RouteByAgentID(wsMsg.AgentID)
	} else {
		ag, err = g.router.RouteDefault()
	}
	if err != nil {
		g.sendWSErrorWithFields(conn, "Agent not found", logger.Fields{
			"action":   wsMsg.Action,
			"agent_id": wsMsg.AgentID,
			"user_id":  wsMsg.UserID,
		})
		return
	}

	// 获取上下文统计
	stats := ag.Runtime.GetContextStats(sess)

	resp := map[string]interface{}{
		"type":          "context_stats",
		"context_stats": stats,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, nil)

	g.AddLog("info", fmt.Sprintf("Context stats sent: session=%s tokens=%d/%d", sessKey, stats.UsedTokens, stats.MaxTokens))
}

// handleWSCompressContext 执行上下文压缩
func (g *Gateway) handleWSCompressContext(conn *websocket.Conn, wsMsg WSMessage) {
	// 构建 session key
	sessKey := wsMsg.Session
	if wsMsg.UserID != "" && wsMsg.AgentID != "" {
		sessKey = fmt.Sprintf("agent:%s:user:%s:ws", wsMsg.AgentID, wsMsg.UserID)
	} else if sessKey == "" {
		g.sendWSErrorWithFields(conn, "Session or UserID+AgentID is required", logger.Fields{
			"action":   wsMsg.Action,
			"user_id":  wsMsg.UserID,
			"agent_id": wsMsg.AgentID,
		})
		return
	}

	sess := g.sessionMgr.GetOrCreate(sessKey)
	if sess == nil {
		g.sendWSErrorWithFields(conn, "Session not found", logger.Fields{
			"action":     wsMsg.Action,
			"session_id": sessKey,
			"user_id":    wsMsg.UserID,
			"agent_id":   wsMsg.AgentID,
		})
		return
	}

	agentID := wsMsg.AgentID
	if strings.TrimSpace(agentID) == "" {
		ag, err := g.router.RouteDefault()
		if err != nil {
			g.sendWSErrorWithFields(conn, "Agent not found", logger.Fields{
				"action":  wsMsg.Action,
				"user_id": wsMsg.UserID,
			})
			return
		}
		agentID = ag.ID
	}
	if strings.TrimSpace(agentID) == "" {
		g.sendWSErrorWithFields(conn, "Agent not found", logger.Fields{
			"action":   wsMsg.Action,
			"agent_id": wsMsg.AgentID,
			"user_id":  wsMsg.UserID,
		})
		return
	}

	beforeTokens := estimateGatewaySessionTokens(sess)
	compactResult, err := g.CompactSession(agentID, sess)
	afterTokens := estimateGatewaySessionTokens(sess)
	freedTokens := beforeTokens - afterTokens
	if freedTokens < 0 {
		freedTokens = 0
	}
	result := map[string]interface{}{
		"success":      err == nil && compactResult != nil && compactResult.Compacted,
		"freed_tokens": freedTokens,
		"compressed":   []map[string]interface{}{},
	}
	if compactResult != nil {
		result["compressed"] = []map[string]interface{}{{
			"name":       "session",
			"original":   compactResult.BeforeMessages,
			"compressed": compactResult.AfterMessages,
			"freed":      compactResult.BeforeMessages - compactResult.AfterMessages,
		}}
		if compactResult.Skipped {
			result["success"] = false
			result["error"] = compactResult.SkippedReason
		}
	}
	if err != nil {
		result["error"] = err.Error()
	}

	resp := map[string]interface{}{
		"type":            "compress_result",
		"compress_result": result,
	}
	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, nil)

	if err == nil && compactResult != nil && compactResult.Compacted {
		g.AddLog("info", fmt.Sprintf("Context compressed: session=%s freed=%d tokens", sessKey, freedTokens))
	} else {
		errMsg := "not compacted"
		if err != nil {
			errMsg = err.Error()
		} else if compactResult != nil && compactResult.SkippedReason != "" {
			errMsg = compactResult.SkippedReason
		}
		g.AddLog("warn", fmt.Sprintf("Context compression failed: session=%s error=%s", sessKey, errMsg))
	}
}

func estimateGatewaySessionTokens(sess *session.Session) int {
	if sess == nil {
		return 0
	}
	tokens := 0
	for _, m := range sess.GetMessages() {
		tokens += len(m.Content) / 3
		tokens += len(m.Role) + 5
		for _, tc := range m.ToolCalls {
			tokens += len(tc.Function.Name) + len(tc.Function.Arguments)/3 + 20
		}
	}
	return tokens
}

// handleWSTaskStatus returns the current running task status for a session
func (g *Gateway) handleWSTaskStatus(conn *websocket.Conn, wsMsg WSMessage) {
	sessKey := wsMsg.Session
	if sessKey == "" && wsMsg.UserID != "" && wsMsg.AgentID != "" {
		sessKey = fmt.Sprintf("agent:%s:user:%s:ws", wsMsg.AgentID, wsMsg.UserID)
	}

	if sessKey == "" {
		g.sendWSErrorWithFields(conn, "Session or UserID+AgentID is required", logger.Fields{
			"action":   wsMsg.Action,
			"user_id":  wsMsg.UserID,
			"agent_id": wsMsg.AgentID,
		})
		return
	}

	g.activeTasksMu.RLock()
	task := g.activeTasks[sessKey]
	g.activeTasksMu.RUnlock()

	resp := map[string]interface{}{
		"type":       "task_status",
		"session_id": sessKey,
	}

	if task != nil {
		resp["running"] = true
		resp["started_at"] = task.StartedAt.Format(time.RFC3339)
		resp["duration"] = time.Since(task.StartedAt).Seconds()
		g.AddLog("info", fmt.Sprintf("Task status: session=%s running=true", sessKey))
	} else {
		resp["running"] = false
		g.AddLog("info", fmt.Sprintf("Task status: session=%s running=false", sessKey))
	}

	data, _ := json.Marshal(resp)
	g.writeWSJSON(conn, data, logger.Fields{"response_type": "task_status", "session_id": sessKey})
}
