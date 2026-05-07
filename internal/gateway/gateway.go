package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chawuciren/evoduck/internal/agent"
	"github.com/chawuciren/evoduck/internal/channels"
	"github.com/chawuciren/evoduck/internal/channels/wecom"
	"github.com/chawuciren/evoduck/internal/channels/weixin"
	"github.com/chawuciren/evoduck/internal/command"
	cronpkg "github.com/chawuciren/evoduck/internal/cron"
	"github.com/chawuciren/evoduck/internal/llm"
	"github.com/chawuciren/evoduck/internal/mediautil"
	"github.com/chawuciren/evoduck/internal/plugin"
	"github.com/chawuciren/evoduck/internal/profile"
	"github.com/chawuciren/evoduck/internal/router"
	"github.com/chawuciren/evoduck/internal/scheduler"
	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/internal/subagent"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
	webassets "github.com/chawuciren/evoduck/web"
)

func init() {
	// Register channel factories at package init time
	channels.RegisterFactory("weixin", newWeixinBridgeFactory)
	channels.RegisterFactory("wecom", newWecomBridgeFactory)
}

func newWeixinBridgeFactory(channelID string, cfg config.ChannelConfig, decider *proxy.Decider) (channels.Bridge, error) {
	if cfg.Token == "" {
		logger.Debug("Skipping weixin channel without token", logger.Fields{
			"channel_id": channelID,
		})
		return nil, nil
	}

	apiBaseURL := cfg.APIBaseURL
	if apiBaseURL == "" {
		apiBaseURL = weixin.DefaultAPIBaseURL
	}

	bridge := weixin.New(weixin.WeixinConfig{
		Token:      cfg.Token,
		AccountID:  channelID,
		APIBaseURL: apiBaseURL,
	}, decider)
	bridge.SetChannelConfig(channelID, cfg.UserID, models.Role(cfg.Role))
	return bridge, nil
}

func newWecomBridgeFactory(channelID string, cfg config.ChannelConfig, decider *proxy.Decider) (channels.Bridge, error) {
	if cfg.BotID == "" || cfg.Secret == "" {
		logger.Debug("Skipping wecom channel without bot_id/secret", logger.Fields{
			"channel_id": channelID,
		})
		return nil, nil
	}

	bridge := wecom.New(wecom.WeComConfig{
		BotID:  cfg.BotID,
		Secret: cfg.Secret,
	}, decider)
	bridge.SetChannelConfig(channelID, models.Role(cfg.Role))
	return bridge, nil
}

type Gateway struct {
	config            *config.Config
	configPath        string
	configMu          sync.RWMutex
	llmReg            *llm.Registry
	agentMgr          *agent.Manager
	router            *router.Router
	sessionMgr        *session.Manager
	channelMgr        *channels.Manager
	wsServer          *http.Server
	httpServer        *http.Server
	startTime         time.Time
	wsConnMu          sync.RWMutex
	wsConns           map[string]*WSConnection
	activeTasks       map[string]*ActiveTask
	activeTasksMu     sync.RWMutex
	logBuffer         []LogEntry
	logMu             sync.RWMutex
	cleanupStopCh     chan struct{}
	scheduler         *cronpkg.Cron
	schedulerService  *scheduler.Service
	backgroundRuntime *BackgroundAgentRuntime
	subagentManager   *subagent.Manager
	slashHandler      *SlashCommandHandler // 斜杆命令处理器
	channelsStarted   bool
	pluginManager     *plugin.Manager
	mediaStore        *mediautil.Store
	proxyDecider      *proxy.Decider
}

type WSConnection struct {
	ConnID  string
	AgentID string
	UserID  string
	SessKey string
	Conn    interface{}
	WriteMu sync.Mutex
}

// ActiveTask 活动任务追踪
type ActiveTask struct {
	RunID      string
	SessionKey string
	ConnID     string
	CancelFunc context.CancelFunc
	StartedAt  time.Time
}

type schedulerExecutor struct {
	gateway *Gateway
}

func (e schedulerExecutor) Execute(req *scheduler.ExecutionRequest) error {
	if req == nil {
		return fmt.Errorf("execution request is required")
	}
	schedule := req.Schedule
	switch schedule.Scope {
	case scheduler.ScheduleScopeSystem:
		return e.gateway.executeSystemSchedule(schedule)
	case scheduler.ScheduleScopeAgent, scheduler.ScheduleScopeUser:
		err := e.gateway.executeScheduledRun(schedule)
		status, statusErr := e.gateway.scheduleDeliveryStatus(schedule)
		if statusErr != nil {
			req.DeliveryStatus = scheduler.DeliveryStatusUnknown
		} else {
			req.DeliveryStatus = status
		}
		if err != nil && req.DeliveryStatus == "" {
			req.DeliveryStatus = scheduler.DeliveryStatusFailed
		}
		return err
	default:
		return fmt.Errorf("unsupported schedule scope: %s", schedule.Scope)
	}
}

func (g *Gateway) scheduleDeliveryStatus(schedule scheduler.ScheduleRecord) (scheduler.DeliveryStatus, error) {
	sessionKey := strings.TrimSpace(schedule.ExecutionSessionKey)
	if sessionKey == "" {
		sessionKey = g.scheduledExecutionSessionKey(schedule)
	}
	if strings.TrimSpace(sessionKey) == "" {
		return scheduler.DeliveryStatusNotAttempted, nil
	}
	g.wsConnMu.RLock()
	defer g.wsConnMu.RUnlock()
	for _, wsConn := range g.wsConns {
		if wsConn == nil {
			continue
		}
		if strings.TrimSpace(wsConn.SessKey) == sessionKey {
			return scheduler.DeliveryStatusWSDelivered, nil
		}
	}
	return scheduler.DeliveryStatusNotAttempted, nil
}

func New(cfg *config.Config, configPath string, llmReg *llm.Registry, agentMgr *agent.Manager, pluginMgr *plugin.Manager, proxyDecider *proxy.Decider) *Gateway {
	storeBase := filepath.Join(cfg.DataDir, "sessions")
	store, err := session.NewJSONLStore(storeBase)
	if err != nil {
		logger.Warn("Failed to create session store", logger.Fields{
			"error": err.Error(),
		})
		store = nil
	}

	sessionTTL := cfg.Memory.ShortTerm.SessionTTL

	gw := &Gateway{
		config:        cfg,
		configPath:    configPath,
		llmReg:        llmReg,
		agentMgr:      agentMgr,
		router:        router.New(agentMgr, cfg.Channels, cfg.DefaultAgent),
		sessionMgr:    session.NewManager(store, sessionTTL),
		channelMgr:    initChannels(cfg, pluginMgr, proxyDecider),
		startTime:     time.Now(),
		wsConns:       make(map[string]*WSConnection),
		activeTasks:   make(map[string]*ActiveTask),
		logBuffer:     make([]LogEntry, 0, 100),
		cleanupStopCh: make(chan struct{}),
		scheduler:     cronpkg.New(),
		pluginManager: pluginMgr,
		proxyDecider:  proxyDecider,
	}
	gw.backgroundRuntime = NewBackgroundAgentRuntime(agentMgr, gw.sessionMgr)
	if store, err := mediautil.NewStore(cfg.DataDir); err != nil {
		logger.Warn("Failed to create media store", logger.Fields{"error": err.Error()})
	} else {
		gw.mediaStore = store
	}
	gw.schedulerService = scheduler.NewService(gw.scheduler, scheduler.NewStore(scheduler.DefaultStorePath(cfg)), scheduler.NewRunStore(scheduler.DefaultRunStoreDir(cfg)), schedulerExecutor{gateway: gw})
	gw.subagentManager = subagent.NewManager(subagent.NewStore(subagent.DefaultStorePath(cfg.DataDir)))
	if err := gw.subagentManager.Load(); err != nil {
		logger.Warn("Failed to load subagent tasks", logger.Fields{"error": err.Error()})
	}
	if agentMgr != nil {
		agentMgr.SetConfigReloader(gw.reloadSystemConfigFromAgent)
	}

	return gw
}

func (g *Gateway) reloadSystemConfigFromAgent(ctx context.Context) (string, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	result, issues, err := g.reloadConfigFromDisk()
	if err != nil {
		return "", err
	}
	if len(issues) > 0 {
		return "", fmt.Errorf("config reload validation failed with %d issue(s)", len(issues))
	}
	if result == nil {
		return "Reloaded config.", nil
	}
	return fmt.Sprintf("Reloaded config: applied_now=%s restart_required=%s.", strings.Join(result.AppliedNow, ","), strings.Join(result.RestartRequired, ",")), nil
}

// initSlashHandler 初始化斜杆命令处理器 (在 Start 中调用)
func (g *Gateway) initSlashHandler() {
	g.slashHandler = NewSlashCommandHandler(g)
}

// initChannels 初始化渠道
func initChannels(cfg *config.Config, pluginMgr *plugin.Manager, decider *proxy.Decider) *channels.Manager {
	mgr := channels.NewManager()

	for channelID, chCfg := range cfg.Channels {
		if strings.EqualFold(strings.TrimSpace(chCfg.Type), "webchat") {
			logger.Debug("Skipping reserved webchat gateway entry during channel bridge init", logger.Fields{
				"channel_id": channelID,
				"type":       chCfg.Type,
			})
			continue
		}

		bridge, err := channels.NewBridge(channelID, chCfg, decider)
		if err != nil {
			logger.Warn("Failed to create channel bridge", logger.Fields{
				"channel_id": channelID,
				"type":       chCfg.Type,
				"error":      err.Error(),
			})
			continue
		}

		if bridge == nil {
			logger.Debug("Skipping channel without bridge", logger.Fields{
				"channel_id": channelID,
				"type":       chCfg.Type,
			})
			continue
		}

		mgr.Register(bridge)
		logger.Info("Channel registered", logger.Fields{
			"channel_id": channelID,
			"type":       chCfg.Type,
			"role":       chCfg.Role,
			"agent":      chCfg.Agent,
		})
	}

	if pluginMgr != nil {
		for _, bridge := range pluginMgr.ListChannelBridges() {
			mgr.Register(bridge)
			logger.Info("Plugin channel registered", logger.Fields{
				"channel_id": bridge.Name(),
				"type":       "plugin",
			})
		}
	}

	return mgr
}

func (g *Gateway) handleChannelMessage(msg *models.NormalizedMessage) {
	g.triggerAfterMessageReceive(msg)
	logger.Info("Gateway received channel message", logger.Fields{
		"channel": msg.Channel,
		"account": msg.AccountID,
		"sender":  msg.SenderID,
		"thread":  msg.ThreadID,
		"content": truncateString(msg.Content, 100),
	})

	ag, err := g.router.RouteByChannel(msg.AccountID)
	if err != nil {
		logger.Error("Failed to route message", logger.Fields{
			"account_id": msg.AccountID,
			"error":      err.Error(),
		})
		return
	}

	sessKey := models.BuildSessionKey(msg)
	_, sessionExists := g.sessionMgr.Get(sessKey)
	sess := g.sessionMgr.GetOrCreate(sessKey)
	g.bindChannelSession(sess, msg)
	if sessionExists != nil {
		g.triggerOnConversationBinding(msg, sessKey)
	}
	ctx := context.Background()
	streamConfig := models.StreamConfig{MaxIterations: ag.Config.MaxIterations, SendToolEvents: true}
	stream, err := g.runSessionInputWithMedia(ctx, ag.ID, sess.Key, msg.Content, msg.Media, streamConfig)
	if err != nil {
		logger.Error("Agent stream failed", logger.Fields{
			"agent_id": ag.ID,
			"error":    err.Error(),
		})
		return
	}

	var responseBuilder strings.Builder
	streamID := fmt.Sprintf("channel-stream-%d", time.Now().UnixNano())
	if err := g.deliverChannelEvent(ctx, msg, &models.ChannelEvent{Type: models.ChannelEventRunStart, StreamID: streamID}); err != nil {
		logger.Error("Failed to deliver channel event", logger.Fields{
			"channel":    msg.Channel,
			"account_id": msg.AccountID,
			"event_type": models.ChannelEventRunStart,
			"error":      err.Error(),
		})
	}
	for event := range stream {
		if event.Type == models.StreamEventContent {
			responseBuilder.WriteString(event.Content)
		}
		channelEvent := buildChannelEvent(event, responseBuilder.String(), streamID)
		if channelEvent == nil {
			continue
		}
		if err := g.deliverChannelEvent(ctx, msg, channelEvent); err != nil {
			logger.Error("Failed to deliver channel event", logger.Fields{
				"channel":    msg.Channel,
				"account_id": msg.AccountID,
				"event_type": channelEvent.Type,
				"error":      err.Error(),
			})
		}
	}
}

func (g *Gateway) rebuildChannels(cfg *config.Config, connect bool) {
	if g.channelMgr != nil {
		g.channelMgr.DisconnectAll()
	}
	g.router = router.New(g.agentMgr, cfg.Channels, cfg.DefaultAgent)
	g.channelMgr = initChannels(cfg, g.pluginManager, g.proxyDecider)
	if g.channelMgr == nil {
		return
	}
	g.channelMgr.OnMessage(g.handleChannelMessage)
	if connect {
		ctx := context.Background()
		if err := g.channelMgr.ConnectAll(ctx); err != nil {
			logger.Error("Failed to connect channels", logger.Fields{"error": err.Error()})
		}
		g.channelsStarted = true
	}
}

// getLogBuffer 返回过滤后的日志
func (g *Gateway) getLogBuffer(level string) []LogEntry {
	g.logMu.RLock()
	defer g.logMu.RUnlock()

	if level == "" || level == "all" {
		return g.logBuffer
	}

	var filtered []LogEntry
	for _, log := range g.logBuffer {
		if log.Level == level {
			filtered = append(filtered, log)
		}
	}
	return filtered
}

// AddLog 添加日志到 buffer
func (g *Gateway) AddLog(level, message string) {
	g.logMu.Lock()
	defer g.logMu.Unlock()

	g.logBuffer = append(g.logBuffer, LogEntry{
		Time:    time.Now().Format("15:04:05"),
		Level:   level,
		Message: message,
	})

	// 保持 buffer 大小不超过 100
	if len(g.logBuffer) > 100 {
		g.logBuffer = g.logBuffer[len(g.logBuffer)-100:]
	}
}

func (g *Gateway) Start() error {
	// 初始化斜杆命令处理器
	g.initSlashHandler()

	cfg := g.currentConfig()
	addr := fmt.Sprintf("%s:%d", cfg.Gateway.Host, cfg.Gateway.Port)

	// 静态文件服务 - 提供前端页面
	webFS, err := fs.Sub(webassets.Files, ".")
	if err != nil {
		return fmt.Errorf("prepare embedded web assets: %w", err)
	}
	fs := http.FileServer(http.FS(webFS))

	rootMux := http.NewServeMux()
	rootMux.Handle("/ws", g.withAuth(http.HandlerFunc(g.handleWebSocket)))
	rootMux.HandleFunc("/health", g.handleHealth)
	rootMux.HandleFunc("/api/shutdown", g.handleShutdown)
	rootMux.Handle("/api/media/upload", g.withAuth(http.HandlerFunc(g.handleMediaUpload)))
	rootMux.HandleFunc("/media/", g.handleMediaGet)
	rootMux.Handle("/web/", http.StripPrefix("/web/", fs))
	rootMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// 根路径重定向到前端页面
		http.Redirect(w, r, "/web/index.html", http.StatusFound)
	})

	g.wsServer = &http.Server{
		Addr:    addr,
		Handler: logger.LoggingMiddleware(rootMux),
	}

	httpMux := http.NewServeMux()
	httpMux.HandleFunc("/api/health", g.handleHealth)
	httpMux.HandleFunc("/api/media/upload", g.handleMediaUpload)
	httpMux.HandleFunc("/media/", g.handleMediaGet)

	g.httpServer = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", cfg.Gateway.Host, cfg.Gateway.Port+1),
		Handler: logger.LoggingMiddleware(g.withAuth(httpMux)),
	}

	go func() {
		logger.Info("WebSocket server starting", logger.Fields{
			"address": addr,
		})
		g.AddLog("info", "WebSocket server started on "+addr)
		if err := g.wsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("WebSocket server error", logger.Fields{
				"error": err.Error(),
			})
			g.AddLog("error", "WebSocket server error: "+err.Error())
		}
	}()

	go func() {
		httpAddr := g.httpServer.Addr
		logger.Info("HTTP API server starting", logger.Fields{
			"address": httpAddr,
		})
		g.AddLog("info", "HTTP API server started on "+httpAddr)
		if err := g.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", logger.Fields{
				"error": err.Error(),
			})
			g.AddLog("error", "HTTP server error: "+err.Error())
		}
	}()

	g.startSessionCleanup()

	if g.schedulerService != nil {
		if err := g.schedulerService.Load(); err != nil {
			return err
		}
		if err := g.schedulerService.RegisterLoadedTasks(); err != nil {
			return err
		}
	}
	if err := g.registerSystemScheduledTasks(); err != nil {
		return err
	}

	g.scheduler.Start()
	logger.Info("Scheduler started")

	// 启动渠道
	g.startChannels()

	return nil
}

// startChannels 启动所有渠道
func (g *Gateway) startChannels() {
	if g.channelMgr == nil {
		return
	}

	g.channelMgr.OnMessage(g.handleChannelMessage)

	// 连接所有渠道
	ctx := context.Background()
	if err := g.channelMgr.ConnectAll(ctx); err != nil {
		logger.Error("Failed to connect channels", logger.Fields{
			"error": err.Error(),
		})
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// sendChannelMessage 向渠道发送消息
func (g *Gateway) sendChannelMessage(ctx context.Context, msg *models.NormalizedMessage, content string) error {
	return g.sendChannelOutgoingMessage(ctx, msg, &models.OutgoingMessage{Content: content})
}

func (g *Gateway) sendChannelOutgoingMessage(ctx context.Context, msg *models.NormalizedMessage, outgoing *models.OutgoingMessage) error {
	if outgoing == nil {
		return fmt.Errorf("outgoing message is nil")
	}
	content, err := g.runBeforeMessageSendHook(msg, outgoing.Content)
	if err != nil {
		return err
	}
	outMsg := &models.OutgoingMessage{
		Channel:      msg.Channel,
		TargetID:     msg.SenderID,
		Content:      content,
		Media:        append([]models.OutgoingMedia(nil), outgoing.Media...),
		ThreadID:     msg.ThreadID,
		ContextToken: msg.ContextToken,
		ResponseURL:  msg.ResponseURL,
		StreamID:     outgoing.StreamID,
		StreamDone:   outgoing.StreamDone,
	}
	err = g.channelMgr.SendToChannel(ctx, msg.AccountID, outMsg)
	if err == nil {
		return nil
	}
	if !g.shouldFallbackToProactive(msg, outMsg) {
		return err
	}
	fallbackMsg := *outMsg
	fallbackMsg.ContextToken = ""
	fallbackMsg.ResponseURL = ""
	return g.channelMgr.SendToChannel(ctx, msg.AccountID, &fallbackMsg)
}

func (g *Gateway) deliverChannelEvent(ctx context.Context, msg *models.NormalizedMessage, event *models.ChannelEvent) error {
	if g.channelMgr == nil || msg == nil || event == nil {
		return nil
	}
	err := g.channelMgr.HandleEvent(ctx, msg.AccountID, msg, event)
	if err == nil {
		return nil
	}
	if !errors.Is(err, channels.ErrEventDeliveryUnsupported) {
		return err
	}
	return g.deliverLegacyChannelEvent(ctx, msg, event)
}

func (g *Gateway) deliverLegacyChannelEvent(ctx context.Context, msg *models.NormalizedMessage, event *models.ChannelEvent) error {
	switch event.Type {
	case models.ChannelEventFinal, models.ChannelEventError, models.ChannelEventCancelled:
		return g.sendChannelMessage(ctx, msg, event.Content)
	case models.ChannelEventContentChunk:
		if msg.Channel == "wecom" {
			return nil
		}
		return nil
	case models.ChannelEventRunStart, models.ChannelEventThinking, models.ChannelEventPlan, models.ChannelEventPlanUpdate, models.ChannelEventToolStart, models.ChannelEventToolEnd:
		return nil
	default:
		return nil
	}
}

func (g *Gateway) triggerAfterMessageReceive(msg *models.NormalizedMessage) {
	if g.pluginManager == nil || !g.isPluginChannel(msg.AccountID) {
		return
	}
	payload := map[string]interface{}{
		"channel":       msg.Channel,
		"account_id":    msg.AccountID,
		"sender_id":     msg.SenderID,
		"user_id":       msg.UserID,
		"content":       msg.Content,
		"thread_id":     msg.ThreadID,
		"context_token": msg.ContextToken,
	}
	hookCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	g.pluginManager.TriggerObserverHook(hookCtx, "after_message_receive", payload)
}

func (g *Gateway) triggerOnConversationBinding(msg *models.NormalizedMessage, sessionKey string) {
	if g.pluginManager == nil || !g.isPluginChannel(msg.AccountID) {
		return
	}
	payload := map[string]interface{}{
		"channel":     msg.Channel,
		"account_id":  msg.AccountID,
		"sender_id":   msg.SenderID,
		"user_id":     msg.UserID,
		"session_key": sessionKey,
	}
	hookCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	g.pluginManager.TriggerObserverHook(hookCtx, "on_conversation_binding", payload)
}

func (g *Gateway) runBeforeMessageSendHook(msg *models.NormalizedMessage, content string) (string, error) {
	if g.pluginManager == nil || !g.isPluginChannel(msg.AccountID) {
		return content, nil
	}
	payload := map[string]interface{}{
		"channel":    msg.Channel,
		"account_id": msg.AccountID,
		"sender_id":  msg.SenderID,
		"user_id":    msg.UserID,
		"content":    content,
		"thread_id":  msg.ThreadID,
	}
	hookCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	decision := g.pluginManager.TriggerMutatingHook(hookCtx, "before_message_send", payload)
	if patched := applyBeforeMessageSendPatch(content, decision.Patch); patched != "" {
		content = patched
	}
	if !decision.Block {
		return content, nil
	}
	if strings.TrimSpace(decision.Message) != "" {
		return "", fmt.Errorf("message send blocked: %s", decision.Message)
	}
	return "", fmt.Errorf("message send blocked by plugin hook")
}

func applyBeforeMessageSendPatch(content string, patch map[string]interface{}) string {
	if len(patch) == 0 {
		return ""
	}
	patched, _ := patch["content"].(string)
	if strings.TrimSpace(patched) == "" {
		return ""
	}
	return patched
}

func (g *Gateway) isPluginChannel(accountID string) bool {
	if g.pluginManager == nil {
		return false
	}
	for _, bridge := range g.pluginManager.ListChannelBridges() {
		if bridge != nil && bridge.Name() == accountID {
			return true
		}
	}
	return false
}

func (g *Gateway) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	g.AddLog("info", "Gateway shutting down...")

	close(g.cleanupStopCh)

	if g.scheduler != nil {
		g.scheduler.Stop()
		logger.Info("Scheduler stopped")
	}

	// 断开所有渠道
	if g.channelMgr != nil {
		g.channelMgr.DisconnectAll()
	}

	// Pipeline 不需要显式停止

	if g.wsServer != nil {
		if err := g.wsServer.Shutdown(ctx); err != nil {
			logger.Error("WebSocket server shutdown error", logger.Fields{
				"error": err.Error(),
			})
			g.AddLog("error", "WebSocket shutdown error: "+err.Error())
		}
	}

	if g.httpServer != nil {
		if err := g.httpServer.Shutdown(ctx); err != nil {
			logger.Error("HTTP server shutdown error", logger.Fields{
				"error": err.Error(),
			})
			g.AddLog("error", "HTTP shutdown error: "+err.Error())
		}
	}

	logger.Info("Gateway stopped")
	g.AddLog("info", "Gateway stopped")
	return nil
}

func (g *Gateway) startSessionCleanup() {
	cfg := g.currentConfig()
	cleanupInterval := cfg.Memory.ShortTerm.CleanupInterval
	if cleanupInterval == 0 {
		cleanupInterval = 1 * time.Hour
	}

	logger.Info("Session cleanup task started", logger.Fields{
		"interval": cleanupInterval.String(),
		"ttl":      g.sessionMgr.TTL().String(),
	})

	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				count, err := g.sessionMgr.Cleanup()
				if err != nil {
					logger.Error("Session cleanup failed", logger.Fields{
						"error": err.Error(),
					})
					g.AddLog("error", "Session cleanup failed: "+err.Error())
				} else if count > 0 {
					logger.Info("Session cleanup completed", logger.Fields{
						"deleted": count,
					})
					g.AddLog("info", fmt.Sprintf("Cleaned up %d expired sessions", count))
				}
			case <-g.cleanupStopCh:
				logger.Info("Session cleanup task stopped")
				return
			}
		}
	}()
}

func (g *Gateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"uptime": time.Since(g.startTime).String(),
	})
}

func (g *Gateway) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "shutting_down",
		"message": "Graceful shutdown initiated",
	})
	// Trigger graceful shutdown asynchronously
	go g.Stop()
}

func (g *Gateway) registerSystemScheduledTasks() error {
	if g.scheduler == nil {
		return nil
	}

	for _, scheduleJob := range g.systemScheduledJobs() {
		if g.scheduler.Exists(scheduleJob.ID) {
			if err := g.scheduler.Update(scheduleJob); err != nil {
				return err
			}
			continue
		}
		if err := g.scheduler.Register(scheduleJob); err != nil {
			return err
		}
	}
	if g.schedulerService != nil {
		for _, schedule := range g.systemScheduleRecords() {
			g.schedulerService.RegisterSystem(schedule)
			// System schedules use ephemeral curator sessions, no need to initialize persistent session.
		}
	}
	return nil
}

func (g *Gateway) ensureExperienceCuratorAgent(cfg *config.Config) error {
	if g.agentMgr == nil || cfg == nil {
		return nil
	}
	base := config.AgentConfig{
		Provider: cfg.LLM.DefaultProvider,
		Model:    cfg.LLM.DefaultModel,
	}
	if defaultCfg, ok := cfg.Agents[cfg.DefaultAgent]; ok {
		base.Provider = defaultCfg.Provider
		base.Model = defaultCfg.Model
		base.Temperature = defaultCfg.Temperature
		base.MaxTokens = defaultCfg.MaxTokens
		base.TopP = defaultCfg.TopP
		base.MaxIterations = defaultCfg.MaxIterations
	}
	return g.agentMgr.EnsureExperienceCurator(base)
}

func (g *Gateway) bindChannelSession(sess *session.Session, msg *models.NormalizedMessage) {
	if sess == nil || msg == nil {
		return
	}
	actorUserID := strings.TrimSpace(msg.UserID)
	if actorUserID == "" {
		actorUserID = strings.TrimSpace(msg.SenderID)
	}
	sess.SetMetadataValue("channel", strings.TrimSpace(msg.Channel))
	sess.SetMetadataValue("account_id", strings.TrimSpace(msg.AccountID))
	sess.SetMetadataValue("sender_id", strings.TrimSpace(msg.SenderID))
	sess.SetMetadataValue("actor_user_id", actorUserID)
	sess.SetMetadataValue("thread_id", strings.TrimSpace(msg.ThreadID))
	if msg.IsDM {
		sess.SetMetadataValue("chat_type", "direct")
	} else if strings.TrimSpace(msg.ThreadID) != "" {
		sess.SetMetadataValue("chat_type", "group")
	}
	sess.SetMetadataValue("context_token", strings.TrimSpace(msg.ContextToken))
	sess.SetMetadataValue("response_url", strings.TrimSpace(msg.ResponseURL))
	if msg.Channel != "" {
		sess.SetMetadataValue("delivery_target", "channel")
	}
}

func (g *Gateway) runSessionInput(ctx context.Context, agentID, sessionKey, input string, config models.StreamConfig) (<-chan models.StreamEvent, error) {
	return g.runSessionInputWithMedia(ctx, agentID, sessionKey, input, nil, config)
}

func (g *Gateway) runSessionInputWithMedia(ctx context.Context, agentID, sessionKey, input string, media []models.OutgoingMedia, config models.StreamConfig) (<-chan models.StreamEvent, error) {
	if g.backgroundRuntime == nil {
		g.backgroundRuntime = NewBackgroundAgentRuntime(g.agentMgr, g.sessionMgr)
	}
	return g.backgroundRuntime.StartInternalRun(ctx, BackgroundAgentRunRequest{
		Kind:                "session_input",
		AgentID:             agentID,
		ExecutionSessionKey: sessionKey,
		Prompt:              input,
		Media:               append([]models.OutgoingMedia(nil), media...),
		StreamConfig:        config,
	})
}

func (g *Gateway) systemScheduledJobs() []cronpkg.Job {
	records := g.systemScheduleRecords()
	jobs := make([]cronpkg.Job, 0, len(records))
	for _, record := range records {
		schedule := record
		jobs = append(jobs, cronpkg.Job{
			ID:          schedule.ID,
			Name:        schedule.Name,
			Scope:       schedule.Scope,
			AgentID:     schedule.AgentID,
			Schedule:    schedule.Schedule,
			Description: schedule.Description,
			Enabled:     true,
			Handler: func(ctx context.Context) error {
				if g.schedulerService != nil {
					return g.schedulerService.TriggerNow(schedule.ID, scheduler.TriggerSourceCron)
				}
				return g.executeSystemSchedule(schedule)
			},
		})
	}
	return jobs
}

func (g *Gateway) systemScheduleRecords() []scheduler.ScheduleRecord {
	cfg := g.currentConfig()
	return []scheduler.ScheduleRecord{
		{
			ID:                  "system:memory-curation",
			Name:                "memory curation",
			Scope:               scheduler.ScheduleScopeSystem,
			AgentID:             agent.ExperienceCuratorID,
			Schedule:            cfg.Scheduler.SystemTasks.MemoryCuration.Schedule,
			Description:         "Run hourly lightweight user memory curation with the experience curator",
			Enabled:             true,
			ExecutionSessionKey: "agent:" + agent.ExperienceCuratorID + ":schedule:system:memory-curation",
			ConcurrencyPolicy:   scheduler.ConcurrencyPolicySkipIfRunning,
			Metadata: map[string]string{
				"session_kind":  "schedule",
				"memory_policy": "ignore",
				"task_kind":     "memory_curation",
			},
		},
		{
			ID:                  "system:experience-curation",
			Name:                "experience curation",
			Scope:               scheduler.ScheduleScopeSystem,
			AgentID:             agent.ExperienceCuratorID,
			Schedule:            cfg.Scheduler.SystemTasks.ExperienceCuration.Schedule,
			Description:         "Run daily experience curation and reusable knowledge or skill evolution",
			Enabled:             true,
			ExecutionSessionKey: "agent:" + agent.ExperienceCuratorID + ":schedule:system:experience-curation",
			ConcurrencyPolicy:   scheduler.ConcurrencyPolicySkipIfRunning,
			Metadata: map[string]string{
				"session_kind":  "schedule",
				"memory_policy": "ignore",
				"task_kind":     "experience_curation",
			},
		},
	}
}

func (g *Gateway) executeSystemSchedule(schedule scheduler.ScheduleRecord) error {
	taskKind, prompt, err := systemCurationTaskSpec(schedule.ID)
	if err != nil {
		return err
	}
	return g.executeCuratorSystemTask(schedule, taskKind, prompt)
}

func systemCurationTaskSpec(scheduleID string) (taskKind string, prompt string, err error) {
	switch scheduleID {
	case "system:memory-curation":
		return "memory_curation", profile.DefaultHourlyMemoryCurationPrompt(), nil
	case "system:experience-curation":
		return "experience_curation", profile.DefaultDailyExperienceCurationPrompt(), nil
	default:
		return "", "", fmt.Errorf("unknown system schedule: %s", scheduleID)
	}
}

func (g *Gateway) executeCuratorSystemTask(schedule scheduler.ScheduleRecord, taskKind string, prompt string) error {
	startedAt := time.Now()
	if strings.TrimSpace(schedule.AgentID) == "" {
		schedule.AgentID = agent.ExperienceCuratorID
	}
	if strings.TrimSpace(schedule.ExecutionSessionKey) == "" {
		schedule.ExecutionSessionKey = g.scheduledExecutionSessionKey(schedule)
	}
	if fromMeta := strings.TrimSpace(schedule.Metadata["task_kind"]); fromMeta != "" {
		taskKind = fromMeta
	}
	logger.Info("Starting curator system task", logger.Fields{
		"schedule_id":  schedule.ID,
		"schedule":     schedule.Schedule,
		"task_kind":    taskKind,
		"session_key":  schedule.ExecutionSessionKey,
		"prompt_chars": len(prompt),
	})
	report, err := g.agentMgr.RunExperienceCuratorEphemeral(context.Background(), prompt, agent.EphemeralRunOptions{
		Metadata: map[string]string{
			"session_kind":  "schedule",
			"memory_policy": "ignore",
			"task_kind":     taskKind,
		},
	})
	if err != nil {
		logger.Error("Curator system task failed", logger.Fields{
			"schedule_id": schedule.ID,
			"task_kind":   taskKind,
			"duration_ms": time.Since(startedAt).Milliseconds(),
			"error":       err.Error(),
		})
		return err
	}
	logger.Info("Curator system task completed", logger.Fields{
		"schedule_id":  schedule.ID,
		"task_kind":    taskKind,
		"duration_ms":  time.Since(startedAt).Milliseconds(),
		"report_chars": len(strings.TrimSpace(report)),
	})
	return nil
}

func (g *Gateway) executeScheduledRun(schedule scheduler.ScheduleRecord) error {
	if schedule.AgentID == "" {
		schedule.AgentID = g.GetDefaultAgentID()
	}

	sessionKey := strings.TrimSpace(schedule.ExecutionSessionKey)
	if sessionKey == "" {
		sessionKey = g.scheduledExecutionSessionKey(schedule)
	}
	streamConfig := models.StreamConfig{SendToolEvents: false}
	if g.backgroundRuntime == nil {
		g.backgroundRuntime = NewBackgroundAgentRuntime(g.agentMgr, g.sessionMgr)
	}
	stream, err := g.backgroundRuntime.StartInternalRun(context.Background(), BackgroundAgentRunRequest{
		RunID:               schedule.ID,
		Kind:                "schedule",
		AgentID:             schedule.AgentID,
		UserID:              schedule.UserID,
		ParentSessionKey:    schedule.OriginSessionKey,
		ExecutionSessionKey: sessionKey,
		Prompt:              schedule.Prompt,
		Media:               nil,
		Metadata: map[string]string{
			"session_kind":       "schedule",
			"memory_policy":      "ignore",
			"schedule_id":        schedule.ID,
			"origin_session_key": schedule.OriginSessionKey,
		},
		StreamConfig:     streamConfig,
		EphemeralSession: true,
		LogPath:          g.backgroundRunLogPath("schedule", schedule.ID),
	})
	if err != nil {
		return err
	}
	for event := range stream {
		resp := WSResponse{
			Type:            event.Type,
			Content:         event.Content,
			ThinkingContent: event.ThinkingContent,
			ToolID:          event.ToolID,
			ToolName:        event.ToolName,
			ToolResult:      event.ToolResult,
			Iteration:       event.Iteration,
			Plan:            event.Plan,
			Done:            event.Done,
		}
		if event.Error != nil {
			resp.Type = "error"
			resp.Content = event.Error.Error()
			resp.Done = true
			g.sendWSEventToSession(sessionKey, resp)
			return event.Error
		}
		g.sendWSEventToSession(sessionKey, resp)
	}
	return nil
}

// ============================================================================
// GatewayAccessor 接口实现 (用于命令系统)
// ============================================================================

// GetAgentManager 获取 Agent Manager
func (g *Gateway) GetAgentManager() command.AgentManagerAccessor {
	return &agentManagerAccessor{mgr: g.agentMgr}
}

// GetDefaultAgentID 获取默认 Agent ID
// 优先使用配置中的 default_agent，否则返回第一个注册的 agent
func (g *Gateway) GetDefaultAgentID() string {
	// 优先使用配置中的默认 agent
	cfg := g.currentConfig()
	if cfg.DefaultAgent != "" {
		if _, err := g.agentMgr.Get(cfg.DefaultAgent); err == nil {
			return cfg.DefaultAgent
		}
	}
	// 否则返回第一个注册的 agent
	agents := g.agentMgr.List()
	if len(agents) > 0 {
		return agents[0].ID
	}
	return ""
}

// GetSessionManager 获取 Session Manager
func (g *Gateway) GetSessionManager() command.SessionManagerAccessor {
	return &sessionManagerAccessor{mgr: g.sessionMgr}
}

// GetSessionManagerRaw 获取原始 Session Manager (用于 Pipeline)
func (g *Gateway) GetSessionManagerRaw() *session.Manager {
	return g.sessionMgr
}

// GetOrCreateSession 获取或创建 Session
func (g *Gateway) GetOrCreateSession(sessionKey string) *session.Session {
	return g.sessionMgr.GetOrCreate(sessionKey)
}

func (g *Gateway) Get(sessionKey string) (*session.Session, error) {
	return g.sessionMgr.Get(sessionKey)
}

func (g *Gateway) GetOrCreate(sessionKey string) *session.Session {
	return g.sessionMgr.GetOrCreate(sessionKey)
}

func (g *Gateway) List() []session.SessionInfo {
	return g.sessionMgr.List()
}

func (g *Gateway) SendSessionMessage(ctx context.Context, sessionKey string, content string) (int, error) {
	return g.SendSessionOutgoingMessage(ctx, sessionKey, &models.OutgoingMessage{Content: content})
}

func (g *Gateway) SendSessionMediaMessage(ctx context.Context, sessionKey string, content string, media []models.OutgoingMedia) (int, error) {
	return g.SendSessionOutgoingMessage(ctx, sessionKey, &models.OutgoingMessage{Content: content, Media: media})
}

func (g *Gateway) SendSessionOutgoingMessage(ctx context.Context, sessionKey string, outgoing *models.OutgoingMessage) (int, error) {
	if strings.TrimSpace(sessionKey) == "" {
		return 0, fmt.Errorf("session key is required")
	}
	if outgoing == nil {
		return 0, fmt.Errorf("outgoing message is required")
	}
	displayContent := sessionOutgoingDisplayContent(outgoing)
	if strings.TrimSpace(displayContent) == "" {
		return 0, fmt.Errorf("content or media is required")
	}
	sess := g.sessionMgr.GetOrCreate(sessionKey)
	wsMedia, err := g.normalizeIncomingMedia(outgoing.Media)
	if err != nil {
		return 0, err
	}
	wsDelivered := g.sendWSMessageToSession(sessionKey, displayContent, wsMedia, true)
	// Only append to session history if not during active tool execution.
	// During tool calls, the runtime manages the message sequence (assistant+tool_calls → tool messages).
	// Appending an assistant message here would corrupt the sequence required by OpenAI API.
	if !sess.IsInToolExecution() {
		sess.Append(models.Message{Role: "assistant", Content: displayContent, Media: append([]models.OutgoingMedia(nil), wsMedia...)})
	}
	if err := g.sendBoundChannelOutgoingMessage(ctx, sess, outgoing); err != nil {
		return wsDelivered, err
	}
	return wsDelivered, nil
}

func (g *Gateway) sendBoundChannelMessage(ctx context.Context, sess *session.Session, content string) error {
	return g.sendBoundChannelOutgoingMessage(ctx, sess, &models.OutgoingMessage{Content: content})
}

func (g *Gateway) sendBoundChannelOutgoingMessage(ctx context.Context, sess *session.Session, outgoing *models.OutgoingMessage) error {
	if sess == nil || g.channelMgr == nil {
		return nil
	}
	if strings.TrimSpace(sess.GetMetadataValue("delivery_target")) != "channel" {
		return nil
	}
	channel := strings.TrimSpace(sess.GetMetadataValue("channel"))
	accountID := strings.TrimSpace(sess.GetMetadataValue("account_id"))
	senderID := strings.TrimSpace(sess.GetMetadataValue("sender_id"))
	if channel == "" || accountID == "" || senderID == "" {
		return nil
	}
	normalized := &models.NormalizedMessage{
		Channel:      channel,
		AccountID:    accountID,
		SenderID:     senderID,
		UserID:       strings.TrimSpace(sess.GetMetadataValue("user_id")),
		ThreadID:     strings.TrimSpace(sess.GetMetadataValue("thread_id")),
		ContextToken: strings.TrimSpace(sess.GetMetadataValue("context_token")),
		ResponseURL:  strings.TrimSpace(sess.GetMetadataValue("response_url")),
	}
	return g.sendChannelOutgoingMessage(ctx, normalized, outgoing)
}

func sessionOutgoingDisplayContent(outgoing *models.OutgoingMessage) string {
	if outgoing == nil {
		return ""
	}
	content := strings.TrimSpace(outgoing.Content)
	if len(outgoing.Media) == 0 {
		return content
	}
	mediaSummary := summarizeOutgoingMedia(outgoing.Media)
	if content == "" {
		return mediaSummary
	}
	if mediaSummary == "" {
		return content
	}
	return content + "\n" + mediaSummary
}

func summarizeOutgoingMedia(media []models.OutgoingMedia) string {
	if len(media) == 0 {
		return ""
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
	return strings.Join(parts, " ")
}

func (g *Gateway) shouldFallbackToProactive(msg *models.NormalizedMessage, outMsg *models.OutgoingMessage) bool {
	if g.channelMgr == nil || msg == nil || outMsg == nil {
		return false
	}
	if strings.TrimSpace(msg.AccountID) == "" {
		return false
	}
	if strings.TrimSpace(outMsg.ContextToken) == "" && strings.TrimSpace(outMsg.ResponseURL) == "" {
		return false
	}
	return g.channelMgr.SupportsProactiveSend(msg.AccountID)
}

func (g *Gateway) RunSessionInput(ctx context.Context, agentID, sessionKey, input string) error {
	stream, err := g.runSessionInput(ctx, agentID, sessionKey, input, models.StreamConfig{SendToolEvents: false})
	if err != nil {
		return err
	}
	for event := range stream {
		resp := WSResponse{
			Type:            event.Type,
			Content:         event.Content,
			ThinkingContent: event.ThinkingContent,
			ToolID:          event.ToolID,
			ToolName:        event.ToolName,
			ToolResult:      event.ToolResult,
			Iteration:       event.Iteration,
			Plan:            event.Plan,
			Done:            event.Done,
		}
		if event.Error != nil {
			resp.Type = "error"
			resp.Content = event.Error.Error()
			resp.Done = true
			g.sendWSEventToSession(sessionKey, resp)
			return event.Error
		}
		g.sendWSEventToSession(sessionKey, resp)
	}
	return nil
}

// GetLLMInfo 获取 LLM 信息
func (g *Gateway) GetLLMInfo() (provider string, model string) {
	cfg := g.currentConfig()
	provider = cfg.LLM.DefaultProvider
	model = cfg.LLM.DefaultModel
	if model == "" {
		if pCfg, ok := cfg.LLM.Providers[provider]; ok {
			model = pCfg.DefaultModel
			if model == "" && len(pCfg.Models) > 0 {
				model = strings.TrimSpace(pCfg.Models[0].ID)
			}
		}
	}
	return
}

func (g *Gateway) ListLLMProviders(ctx context.Context) ([]command.LLMProviderInfo, error) {
	if g.llmReg == nil {
		return nil, fmt.Errorf("llm registry unavailable")
	}

	providerNames := g.llmReg.ListProviderNames()
	result := make([]command.LLMProviderInfo, 0, len(providerNames))
	defaultProvider := g.llmReg.DefaultProviderName()
	for _, providerName := range providerNames {
		info := command.LLMProviderInfo{
			Name:      providerName,
			IsDefault: providerName == defaultProvider,
		}
		models, err := g.llmReg.ListModels(ctx, providerName)
		if err != nil {
			info.Error = err.Error()
			result = append(result, info)
			continue
		}
		info.Models = make([]command.LLMModelInfo, 0, len(models))
		for _, model := range models {
			info.Models = append(info.Models, command.LLMModelInfo{
				ID:                model.ID,
				Name:              model.Name,
				ContextWindow:     model.ContextWindow,
				MaxTokens:         model.MaxTokens,
				SupportsTools:     model.SupportsTools,
				SupportsStreaming: model.SupportsStreaming,
				SupportsVision:    model.SupportsVision,
				Reasoning:         model.Reasoning,
			})
		}
		result = append(result, info)
	}
	return result, nil
}

// GetLogs 获取日志
func (g *Gateway) GetLogs(level string, limit int) []command.LogEntry {
	logs := g.getLogBuffer(level)

	// 限制数量
	if limit > 0 && len(logs) > limit {
		logs = logs[len(logs)-limit:]
	}

	// 转换格式
	result := make([]command.LogEntry, len(logs))
	for i, log := range logs {
		result[i] = command.LogEntry{
			Time:    parseLogTime(log.Time),
			Level:   log.Level,
			Message: log.Message,
		}
	}
	return result
}

// GetStartTime 获取启动时间戳
func (g *Gateway) GetStartTime() int64 {
	return g.startTime.Unix()
}

func (g *Gateway) RunMemoryCuration() (*command.SystemTaskRunResult, error) {
	return g.triggerSystemCurationTask("system:memory-curation", scheduler.TriggerSourceManual)
}

func (g *Gateway) RunExperienceCuration() (*command.SystemTaskRunResult, error) {
	return g.triggerSystemCurationTask("system:experience-curation", scheduler.TriggerSourceManual)
}

func (g *Gateway) triggerSystemCurationTask(id string, source scheduler.TriggerSource) (*command.SystemTaskRunResult, error) {
	if g.schedulerService == nil {
		return nil, fmt.Errorf("scheduler service unavailable")
	}
	schedule, ok := g.schedulerService.Get(id)
	if !ok {
		if err := g.registerSystemScheduledTasks(); err != nil {
			return nil, err
		}
		schedule, ok = g.schedulerService.Get(id)
		if !ok {
			return nil, fmt.Errorf("system schedule not found: %s", id)
		}
	}
	if schedule.Scope != scheduler.ScheduleScopeSystem {
		return nil, fmt.Errorf("scheduled task is not system scoped: %s", id)
	}
	if err := g.schedulerService.TriggerNow(id, source); err != nil {
		return nil, err
	}
	return &command.SystemTaskRunResult{
		ScheduleID:  schedule.ID,
		SessionKey:  schedule.ExecutionSessionKey,
		TaskKind:    schedule.Metadata["task_kind"],
		TriggeredBy: string(source),
	}, nil
}

func (g *Gateway) CompactSession(agentID string, sess *session.Session) (*command.CompactResult, error) {
	if sess == nil {
		return &command.CompactResult{Skipped: true, SkippedReason: "session is not initialized"}, nil
	}
	if strings.TrimSpace(agentID) == "" {
		return nil, fmt.Errorf("agent id is required")
	}

	ag, err := g.agentMgr.Get(agentID)
	if err != nil {
		return nil, err
	}
	if ag == nil || ag.Runtime == nil {
		return nil, fmt.Errorf("agent runtime unavailable: %s", agentID)
	}

	beforeMessages := sess.MessageCount()
	if beforeMessages <= 1 {
		return &command.CompactResult{
			Skipped:        true,
			BeforeMessages: beforeMessages,
			AfterMessages:  beforeMessages,
			SkippedReason:  "not enough messages to compact",
		}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := ag.Runtime.ForceCompact(ctx, sess); err != nil {
		return &command.CompactResult{
			BeforeMessages: beforeMessages,
			AfterMessages:  sess.MessageCount(),
			FailureMessage: err.Error(),
		}, err
	}

	afterMessages := sess.MessageCount()
	summaryInserted := false
	msgs := sess.GetMessages()
	if len(msgs) > 0 {
		summaryInserted = strings.HasPrefix(msgs[0].Content, "Previous conversation summary: ")
	}

	return &command.CompactResult{
		Compacted:       true,
		BeforeMessages:  beforeMessages,
		AfterMessages:   afterMessages,
		SummaryInserted: summaryInserted,
	}, nil
}

// ListSchedulerJobs 返回当前注册的调度任务
func (g *Gateway) ListSchedulerJobs() []command.SchedulerJobInfo {
	if g.scheduler == nil {
		return nil
	}

	jobs := g.scheduler.ListJobs()
	result := make([]command.SchedulerJobInfo, 0, len(jobs))
	for _, job := range jobs {
		if job == nil {
			continue
		}
		result = append(result, command.SchedulerJobInfo{
			ID:          job.ID,
			Name:        job.Name,
			Scope:       string(job.Scope),
			AgentID:     job.AgentID,
			Schedule:    job.Schedule,
			Description: job.Description,
			Enabled:     job.Enabled,
		})
	}
	return result
}

func (g *Gateway) scheduleInfoFromRecord(schedule scheduler.ScheduleRecord) command.ScheduleInfo {
	return command.ScheduleInfo{
		ID:                  schedule.ID,
		Scope:               string(schedule.Scope),
		AgentID:             schedule.AgentID,
		UserID:              schedule.UserID,
		Name:                schedule.Name,
		Description:         schedule.Description,
		Schedule:            schedule.Schedule,
		Prompt:              schedule.Prompt,
		Enabled:             schedule.Enabled,
		LastRunAt:           schedule.LastRunAt,
		LastSuccessAt:       schedule.LastSuccessAt,
		LastError:           schedule.LastError,
		RunCount:            schedule.RunCount,
		OriginSessionKey:    schedule.OriginSessionKey,
		ExecutionSessionKey: schedule.ExecutionSessionKey,
		ConcurrencyPolicy:   string(schedule.ConcurrencyPolicy),
		LastTriggerSource:   string(schedule.LastTriggerSource),
	}
}

func (g *Gateway) scheduledExecutionSessionKey(schedule scheduler.ScheduleRecord) string {
	if schedule.Scope == scheduler.ScheduleScopeUser && schedule.UserID != "" {
		return fmt.Sprintf("agent:%s:user:%s:schedule:%s", schedule.AgentID, schedule.UserID, schedule.ID)
	}
	return fmt.Sprintf("agent:%s:schedule:%s", schedule.AgentID, schedule.ID)
}

func (g *Gateway) newScheduledTaskID(agentID, userID string) string {
	base := fmt.Sprintf("sched-%d", time.Now().UnixNano())
	if agentID == "" && userID == "" {
		return base
	}
	parts := []string{base}
	if agentID != "" {
		parts = append(parts, agentID)
	}
	if userID != "" {
		parts = append(parts, userID)
	}
	return strings.Join(parts, "-")
}

func (g *Gateway) backgroundRunLogPath(kind, id string) string {
	cleanKind := strings.NewReplacer(":", "-", "\\", "-", "/", "-").Replace(strings.TrimSpace(kind))
	cleanID := strings.NewReplacer(":", "-", "\\", "-", "/", "-").Replace(strings.TrimSpace(id))
	if cleanKind == "" {
		cleanKind = "run"
	}
	if cleanID == "" {
		cleanID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return filepath.Join(g.currentConfig().DataDir, "scheduler", cleanID+"-"+cleanKind+"-run.log")
}

func (g *Gateway) ListSchedules(agentID, userID string) []command.ScheduleInfo {
	if g.schedulerService == nil {
		return nil
	}

	schedules := g.schedulerService.ListForOwner(scheduler.ScheduleScopeUser, agentID, userID)
	result := make([]command.ScheduleInfo, 0, len(schedules))
	for _, schedule := range schedules {
		result = append(result, g.scheduleInfoFromRecord(schedule))
	}
	return result
}

func (g *Gateway) ListScheduleRuns(agentID, userID, id string, limit int) ([]command.ScheduleRunInfo, error) {
	if g.schedulerService == nil {
		return nil, fmt.Errorf("scheduler service unavailable")
	}
	schedule, ok := g.schedulerService.Get(id)
	if !ok {
		return nil, fmt.Errorf("scheduled task not found: %s", id)
	}
	if schedule.Scope != scheduler.ScheduleScopeUser {
		return nil, fmt.Errorf("scheduled task is not user scoped: %s", id)
	}
	if schedule.AgentID != agentID || schedule.UserID != userID {
		return nil, fmt.Errorf("scheduled task does not belong to current user: %s", id)
	}
	runs, err := g.schedulerService.ListRuns(id, limit)
	if err != nil {
		return nil, err
	}
	result := make([]command.ScheduleRunInfo, 0, len(runs))
	for _, run := range runs {
		result = append(result, command.ScheduleRunInfo{
			RunID:           run.RunID,
			ScheduleID:      run.ScheduleID,
			SessionKey:      run.SessionKey,
			TriggerSource:   string(run.TriggerSource),
			StartedAt:       run.StartedAt,
			FinishedAt:      run.FinishedAt,
			ExecutionStatus: string(run.ExecutionStatus),
			DeliveryStatus:  string(run.DeliveryStatus),
			Error:           run.Error,
		})
	}
	return result, nil
}

func (g *Gateway) CreateSchedule(agentID, userID string, role models.Role, req command.CreateScheduleRequest) (*command.ScheduleInfo, error) {
	if g.schedulerService == nil {
		return nil, fmt.Errorf("scheduler service unavailable")
	}
	if role != models.RoleEmployee && role != models.RoleAdmin {
		return nil, fmt.Errorf("insufficient role for scheduled task management")
	}
	if agentID == "" {
		agentID = g.GetDefaultAgentID()
	}
	if agentID == "" {
		return nil, fmt.Errorf("agent id is required")
	}
	if userID == "" {
		return nil, fmt.Errorf("user id is required")
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	schedule := scheduler.ScheduleRecord{
		ID:                g.newScheduledTaskID(agentID, userID),
		Scope:             scheduler.ScheduleScopeUser,
		AgentID:           agentID,
		UserID:            userID,
		Role:              role,
		Name:              strings.TrimSpace(req.Name),
		Description:       strings.TrimSpace(req.Description),
		Schedule:          strings.TrimSpace(req.Schedule),
		Prompt:            strings.TrimSpace(req.Prompt),
		Channel:           strings.TrimSpace(req.Channel),
		OriginSessionKey:  strings.TrimSpace(req.OriginSessionKey),
		ConcurrencyPolicy: scheduler.ConcurrencyPolicySkipIfRunning,
		Enabled:           enabled,
	}
	schedule.ExecutionSessionKey = strings.TrimSpace(req.ExecutionSessionKey)
	if schedule.ExecutionSessionKey == "" {
		schedule.ExecutionSessionKey = g.scheduledExecutionSessionKey(schedule)
	}
	created, err := g.schedulerService.Create(schedule)
	if err != nil {
		return nil, err
	}
	g.initializeScheduleSession(created)
	info := g.scheduleInfoFromRecord(created)
	return &info, nil
}

func (g *Gateway) initializeScheduleSession(schedule scheduler.ScheduleRecord) {
	if strings.TrimSpace(schedule.ExecutionSessionKey) == "" {
		return
	}
	sess := g.sessionMgr.GetOrCreate(schedule.ExecutionSessionKey)
	if sess == nil {
		return
	}
	sess.SetMetadataValue("session_kind", "schedule")
	sess.SetMetadataValue("memory_policy", "ignore")
	sess.SetMetadataValue("agent_id", schedule.AgentID)
	sess.SetMetadataValue("user_id", schedule.UserID)
	sess.SetMetadataValue("origin_session_key", schedule.OriginSessionKey)
	for key, value := range schedule.Metadata {
		if strings.TrimSpace(key) == "" {
			continue
		}
		sess.SetMetadataValue(key, value)
	}
}

func (g *Gateway) SetScheduleEnabled(agentID, userID, id string, enabled bool) error {
	if g.schedulerService == nil {
		return fmt.Errorf("scheduler service unavailable")
	}

	schedule, ok := g.schedulerService.Get(id)
	if !ok {
		return fmt.Errorf("scheduled task not found: %s", id)
	}
	if schedule.Scope != scheduler.ScheduleScopeUser {
		return fmt.Errorf("scheduled task is not user scoped: %s", id)
	}
	if schedule.AgentID != agentID || schedule.UserID != userID {
		return fmt.Errorf("scheduled task does not belong to current user: %s", id)
	}
	return g.schedulerService.SetEnabled(id, enabled)
}

func (g *Gateway) TriggerSchedule(agentID, userID, id string, source scheduler.TriggerSource) error {
	if g.schedulerService == nil {
		return fmt.Errorf("scheduler service unavailable")
	}

	schedule, ok := g.schedulerService.Get(id)
	if !ok {
		return fmt.Errorf("scheduled task not found: %s", id)
	}
	if schedule.Scope != scheduler.ScheduleScopeUser {
		return fmt.Errorf("scheduled task is not user scoped: %s", id)
	}
	if schedule.AgentID != agentID || schedule.UserID != userID {
		return fmt.Errorf("scheduled task does not belong to current user: %s", id)
	}
	return g.schedulerService.TriggerNow(id, source)
}

func (g *Gateway) DeleteSchedule(agentID, userID, id string) error {
	if g.schedulerService == nil {
		return fmt.Errorf("scheduler service unavailable")
	}

	schedule, ok := g.schedulerService.Get(id)
	if !ok {
		return fmt.Errorf("scheduled task not found: %s", id)
	}
	if schedule.Scope != scheduler.ScheduleScopeUser {
		return fmt.Errorf("scheduled task is not user scoped: %s", id)
	}
	if schedule.AgentID != agentID || schedule.UserID != userID {
		return fmt.Errorf("scheduled task does not belong to current user: %s", id)
	}
	return g.schedulerService.Delete(id)
}

// FlushSessionMemory 在会话清空前提取关键记忆
func (g *Gateway) FlushSessionMemory(agentID string, sess *session.Session, _ models.Role, userID string) (*command.MemoryFlushResult, error) {
	result := &command.MemoryFlushResult{}
	if sess == nil {
		result.Skipped = true
		result.SkippedReason = "session unavailable"
		return result, nil
	}
	if g.agentMgr == nil {
		result.Skipped = true
		result.SkippedReason = "agent manager unavailable"
		return result, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	report, err := g.agentMgr.RunExperienceCuratorEphemeral(ctx, buildSessionResetCurationPrompt(agentID, sess, userID), agent.EphemeralRunOptions{
		UserID: userID,
		Metadata: map[string]string{
			"session_kind":       "session_reset_flush",
			"memory_policy":      "ignore",
			"source_agent_id":    strings.TrimSpace(agentID),
			"source_session_key": sess.Key,
			"source_session_id":  sess.ID,
		},
	})
	if err != nil {
		result.FailureMessage = err.Error()
		return result, err
	}
	if strings.TrimSpace(report) == "" {
		result.Skipped = true
		result.SkippedReason = "curator returned no report"
		return result, nil
	}
	result.Flushed = true
	return result, nil
}

func buildSessionResetCurationPrompt(agentID string, sess *session.Session, userID string) string {
	var b strings.Builder
	targetUserID := strings.TrimSpace(userID)
	if targetUserID == "" {
		targetUserID = strings.TrimSpace(sess.GetMetadataValue("actor_user_id"))
	}
	if targetUserID == "" {
		targetUserID = strings.TrimSpace(sess.GetUserID())
	}
	b.WriteString("Run session_reset memory curation before the source conversation is cleared.\n\n")
	b.WriteString("Boundaries:\n")
	b.WriteString("- Use the existing experience-curator tools and normal tool loop.\n")
	b.WriteString("- Save only high-value durable user/project facts, preferences, decisions, constraints, reusable troubleshooting, or stable knowledge from the source conversation.\n")
	b.WriteString("- This is cross-agent curation: write source agent user memory with file tools under users/<source_agent_id>_user_<target_user_id>/, not under the curator workspace and not with memory_write for the curator's own agent namespace.\n")
	b.WriteString("- Always inspect or create the target source-agent daily memory file memory/YYYY-MM-DD.md for the current date when the source conversation contains any user-facing content, including casual chat.\n")
	b.WriteString("- Add a concise daily note unless the same point is already present in that daily memory.\n")
	b.WriteString("- Update MEMORY.md only for stable durable facts, preferences, decisions, constraints, reusable troubleshooting, or project context that should outlive the day.\n")
	b.WriteString("- Use USER.md only for confirmed profile/preferences.\n")
	b.WriteString("- Do not save this curation task, reset event, tool traces, or internal report text as memory.\n")
	b.WriteString("- Prefer updating existing memory/knowledge over creating duplicates.\n")
	b.WriteString("- Finish with a concise internal report of saved, skipped, and failed items.\n\n")
	b.WriteString("Source metadata:\n")
	b.WriteString(fmt.Sprintf("- agent_id: %s\n", strings.TrimSpace(agentID)))
	b.WriteString(fmt.Sprintf("- session_key: %s\n", sess.Key))
	b.WriteString(fmt.Sprintf("- target_user_id: %s\n", targetUserID))
	b.WriteString(fmt.Sprintf("- session_user_id: %s\n", sess.GetUserID()))
	b.WriteString(fmt.Sprintf("- actor_user_id: %s\n\n", sess.GetMetadataValue("actor_user_id")))
	b.WriteString("Source conversation to preserve before reset:\n")
	for i, msg := range sess.GetMessages() {
		if msg.Role == "tool" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if len(content) > 800 {
			content = content[:800] + "..."
		}
		b.WriteString(fmt.Sprintf("[%d] %s: %s\n", i, strings.ToUpper(msg.Role), content))
	}
	return b.String()
}

// GetMemoryStatus 获取记忆系统状态
func (g *Gateway) GetMemoryStatus(agentID, userID string) *command.MemoryStatus {
	status := &command.MemoryStatus{}

	ag, err := g.agentMgr.Get(agentID)
	if err != nil {
		return status
	}

	if userID == "" || ag.MemoryManager == nil {
		return status
	}
	memoryMDPath := ag.MemoryManager.GetUserLongtermPath(userID)
	if info, err := os.Stat(memoryMDPath); err == nil {
		status.MemoryMDEnabled = true
		status.MemoryMDSize = int(info.Size())
	}

	return status
}

// parseLogTime 解析日志时间字符串为时间戳
func parseLogTime(timeStr string) int64 {
	t, err := time.Parse("15:04:05", timeStr)
	if err != nil {
		return 0
	}
	return t.Unix()
}

// ============================================================================
// AgentManagerAccessor 实现
// ============================================================================

type agentManagerAccessor struct {
	mgr *agent.Manager
}

func (a *agentManagerAccessor) List() []command.AgentInfo {
	agents := a.mgr.List()
	result := make([]command.AgentInfo, len(agents))
	for i, ag := range agents {
		result[i] = command.AgentInfo{
			ID:        ag.ID,
			Role:      models.Role(ag.Config.Role),
			Provider:  ag.Config.Provider,
			Model:     ag.Config.Model,
			Workspace: ag.Config.Workspace,
			Status:    "active",
		}
	}
	return result
}

func (a *agentManagerAccessor) Get(id string) (*command.AgentInfo, error) {
	ag, err := a.mgr.Get(id)
	if err != nil {
		return nil, err
	}
	return &command.AgentInfo{
		ID:        ag.ID,
		Role:      models.Role(ag.Config.Role),
		Provider:  ag.Config.Provider,
		Model:     ag.Config.Model,
		Workspace: ag.Config.Workspace,
		Status:    "active",
	}, nil
}

// ============================================================================
// SessionManagerAccessor 实现
// ============================================================================

type sessionManagerAccessor struct {
	mgr *session.Manager
}

func (s *sessionManagerAccessor) List() []command.SessionInfo {
	sessions := s.mgr.List()
	result := make([]command.SessionInfo, len(sessions))
	for i, sess := range sessions {
		result[i] = command.SessionInfo{
			Key:          sess.Key,
			MessageCount: sess.MessageCount,
			UpdatedAt:    sess.UpdatedAt.Unix(),
		}
	}
	return result
}

func (s *sessionManagerAccessor) GetOrCreate(key string) *session.Session {
	return s.mgr.GetOrCreate(key)
}

func (s *sessionManagerAccessor) Get(key string) (*session.Session, error) {
	return s.mgr.Get(key)
}

func (s *sessionManagerAccessor) NewSession(key string) *session.Session {
	return s.mgr.NewSession(key)
}
