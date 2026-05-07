package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Connection struct {
	pluginID string
	conn     *websocket.Conn
	mu       sync.Mutex
}

func (c *Connection) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *Connection) WriteJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteJSON(v)
}

type Transport struct {
	mu              sync.RWMutex
	addr            string
	manager         *Manager
	authenticator   Authenticator
	registry        *Registry
	server          *http.Server
	conns           map[string]*Connection
	pending         map[string]chan Frame
	pendingStreams  map[string]chan Frame
	readyCh         chan struct{}
	readyOnce       sync.Once
	expectedPlugins map[string]struct{}
	upgrader        websocket.Upgrader
}

func NewTransport(addr string, authenticator Authenticator, registry *Registry, expectedPlugins []string) *Transport {
	expected := make(map[string]struct{}, len(expectedPlugins))
	for _, pluginID := range expectedPlugins {
		expected[pluginID] = struct{}{}
	}

	return &Transport{
		addr:            addr,
		authenticator:   authenticator,
		registry:        registry,
		conns:           make(map[string]*Connection),
		pending:         make(map[string]chan Frame),
		pendingStreams:  make(map[string]chan Frame),
		readyCh:         make(chan struct{}),
		expectedPlugins: expected,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (t *Transport) SetManager(manager *Manager) {
	t.manager = manager
}

func (t *Transport) SendStreamingRequest(ctx context.Context, pluginID, requestID string, method Method, capabilityID string, data map[string]interface{}) (<-chan Frame, error) {
	t.mu.RLock()
	connection, ok := t.conns[pluginID]
	t.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("plugin connection not found: %s", pluginID)
	}

	streamCh := make(chan Frame, 32)
	t.mu.Lock()
	t.pendingStreams[requestID] = streamCh
	t.mu.Unlock()

	cleanup := func() {
		t.mu.Lock()
		if ch, ok := t.pendingStreams[requestID]; ok {
			delete(t.pendingStreams, requestID)
			close(ch)
		}
		t.mu.Unlock()
	}

	frame := Frame{
		ID:           requestID,
		Type:         FrameTypeRequest,
		Method:       method,
		PluginID:     pluginID,
		CapabilityID: capabilityID,
		Timestamp:    time.Now().Unix(),
		Data:         data,
	}
	if err := connection.WriteJSON(frame); err != nil {
		cleanup()
		return nil, err
	}

	go func() {
		<-ctx.Done()
		_ = t.sendCancel(pluginID, requestID, capabilityID)
		cleanup()
	}()

	return streamCh, nil
}

func (t *Transport) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/plugin", t.handlePluginWS)
	mux.HandleFunc("/health", t.handleHealth)

	t.server = &http.Server{
		Addr:    t.addr,
		Handler: mux,
	}

	go func() {
		logger.Info("Plugin server starting", logger.Fields{"address": t.addr})
		if err := t.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Plugin server error", logger.Fields{"error": err.Error()})
		}
	}()

	return nil
}

func (t *Transport) Shutdown(ctx context.Context) error {
	t.mu.Lock()
	connections := make([]*Connection, 0, len(t.conns))
	for _, conn := range t.conns {
		connections = append(connections, conn)
	}
	t.mu.Unlock()

	for _, conn := range connections {
		_ = conn.Close()
	}

	if t.server == nil {
		return nil
	}
	return t.server.Shutdown(ctx)
}

func (t *Transport) WaitReady(ctx context.Context) error {
	if len(t.expectedPlugins) == 0 {
		return nil
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.readyCh:
		return nil
	}
}

func (t *Transport) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (t *Transport) handlePluginWS(w http.ResponseWriter, r *http.Request) {
	pluginID := r.URL.Query().Get("plugin_id")
	token := r.URL.Query().Get("token")
	if pluginID == "" || token == "" {
		http.Error(w, "missing plugin_id or token", http.StatusUnauthorized)
		return
	}
	if t.authenticator != nil {
		if err := t.authenticator.Authenticate(r.Context(), AuthContext{PluginID: pluginID, Token: token}); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	conn, err := t.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error("Plugin websocket upgrade failed", logger.Fields{"error": err.Error()})
		return
	}

	wrapped := &Connection{pluginID: pluginID, conn: conn}
	t.mu.Lock()
	t.conns[pluginID] = wrapped
	t.mu.Unlock()

	go t.readLoop(wrapped)
}

func (t *Transport) SendRequest(ctx context.Context, pluginID string, method Method, capabilityID string, data map[string]interface{}) (Frame, error) {
	t.mu.RLock()
	connection, ok := t.conns[pluginID]
	t.mu.RUnlock()
	if !ok {
		return Frame{}, fmt.Errorf("plugin connection not found: %s", pluginID)
	}

	requestID := uuid.NewString()
	responseCh := make(chan Frame, 1)

	t.mu.Lock()
	t.pending[requestID] = responseCh
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		delete(t.pending, requestID)
		t.mu.Unlock()
	}()

	frame := Frame{
		ID:           requestID,
		Type:         FrameTypeRequest,
		Method:       method,
		PluginID:     pluginID,
		CapabilityID: capabilityID,
		Timestamp:    time.Now().Unix(),
		Data:         data,
	}
	if err := connection.WriteJSON(frame); err != nil {
		return Frame{}, err
	}

	select {
	case <-ctx.Done():
		_ = t.sendCancel(pluginID, requestID, capabilityID)
		return Frame{}, ctx.Err()
	case response := <-responseCh:
		return response, nil
	}
}

func (t *Transport) readLoop(connection *Connection) {
	defer func() {
		_ = connection.Close()
		t.mu.Lock()
		delete(t.conns, connection.pluginID)
		t.mu.Unlock()
		_ = t.registry.SetStatus(connection.pluginID, StatusDisconnected)
	}()

	for {
		_ = connection.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, payload, err := connection.conn.ReadMessage()
		if err != nil {
			return
		}

		var frame Frame
		if err := json.Unmarshal(payload, &frame); err != nil {
			logger.Warn("Plugin frame decode failed", logger.Fields{"error": err.Error()})
			continue
		}

		t.registry.Touch(connection.pluginID)

		if frame.Type == FrameTypeRequest && frame.Method == MethodRegister {
			if err := t.handleRegister(connection, frame); err != nil {
				logger.Warn("Plugin register failed", logger.Fields{
					"plugin_id": connection.pluginID,
					"error":     err.Error(),
				})
				return
			}
			continue
		}

		if frame.Type == FrameTypeResponse {
			t.handleResponse(frame)
			continue
		}

		if frame.Type == FrameTypeEvent {
			t.handleEvent(frame)
		}
	}
}

func (t *Transport) handleRegister(connection *Connection, frame Frame) error {
	registration, err := decodeRegistration(frame)
	if err != nil {
		return err
	}
	if registration.PluginID != connection.pluginID {
		return fmt.Errorf("registration plugin_id mismatch: %s != %s", registration.PluginID, connection.pluginID)
	}

	if _, err := t.registry.Register(registration); err != nil {
		return err
	}
	if t.manager != nil {
		t.manager.reindexCapabilities()
	}

	response := Frame{
		ID:        frame.ID + ":response",
		Type:      FrameTypeResponse,
		Method:    MethodRegister,
		ReplyTo:   frame.ID,
		PluginID:  connection.pluginID,
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"accepted": registration.Capabilities,
			"rejected": []Capability{},
		},
	}
	if err := connection.WriteJSON(response); err != nil {
		return err
	}

	t.markReadyIfSatisfied()
	logger.Info("Plugin registered", logger.Fields{
		"plugin_id":    registration.PluginID,
		"capabilities": len(registration.Capabilities),
	})
	return nil
}

func (t *Transport) handleResponse(frame Frame) {
	if frame.ReplyTo != "" {
		t.mu.RLock()
		streamCh, ok := t.pendingStreams[frame.ReplyTo]
		t.mu.RUnlock()
		if ok {
			select {
			case streamCh <- frame:
			default:
			}
		}
	}

	if frame.ReplyTo == "" {
		return
	}

	t.mu.RLock()
	responseCh, ok := t.pending[frame.ReplyTo]
	t.mu.RUnlock()
	if !ok {
		return
	}

	select {
	case responseCh <- frame:
	default:
	}
}

func (t *Transport) handleEvent(frame Frame) {
	if frame.Method == MethodChannelMessage {
		t.handleChannelMessage(frame)
		return
	}

	if frame.ReplyTo == "" {
		return
	}
	t.mu.RLock()
	streamCh, ok := t.pendingStreams[frame.ReplyTo]
	t.mu.RUnlock()
	if !ok {
		return
	}
	select {
	case streamCh <- frame:
	default:
	}
}

func (t *Transport) handleChannelMessage(frame Frame) {
	if frame.CapabilityID == "" {
		return
	}
	pluginState, ok := t.registry.Get(frame.PluginID)
	if !ok {
		return
	}
	for _, capability := range pluginState.Capabilities {
		if capability.Type != CapabilityTypeChannel || capability.CapabilityID != frame.CapabilityID {
			continue
		}
		msg, err := decodeNormalizedMessage(frame.Data)
		if err != nil {
			logger.Warn("Plugin channel message decode failed", logger.Fields{"error": err.Error()})
			return
		}
		msg.AccountID = capability.AccountID
		msg.Channel = capability.BridgeName
		if t.manager != nil {
			t.manager.deliverChannelMessage(capability.CapabilityID, msg)
		}
		return
	}
}

func (t *Transport) sendCancel(pluginID, replyTo, capabilityID string) error {
	t.mu.RLock()
	connection, ok := t.conns[pluginID]
	t.mu.RUnlock()
	if !ok {
		return fmt.Errorf("plugin connection not found: %s", pluginID)
	}

	frame := Frame{
		ID:           uuid.NewString(),
		Type:         FrameTypeRequest,
		Method:       MethodCancel,
		PluginID:     pluginID,
		CapabilityID: capabilityID,
		Timestamp:    time.Now().Unix(),
		Data: map[string]interface{}{
			"reply_to": replyTo,
		},
	}
	return connection.WriteJSON(frame)
}

func (t *Transport) markReadyIfSatisfied() {
	if len(t.expectedPlugins) == 0 {
		t.readyOnce.Do(func() { close(t.readyCh) })
		return
	}

	for pluginID := range t.expectedPlugins {
		pluginState, ok := t.registry.Get(pluginID)
		if !ok || pluginState.Status != StatusReady {
			return
		}
	}

	t.readyOnce.Do(func() { close(t.readyCh) })
}

func decodeRegistration(frame Frame) (Registration, error) {
	var registration Registration
	buf, err := json.Marshal(frame.Data)
	if err != nil {
		return registration, err
	}
	if err := json.Unmarshal(buf, &registration); err != nil {
		return registration, err
	}
	return registration, nil
}

func decodeNormalizedMessage(data map[string]interface{}) (*models.NormalizedMessage, error) {
	buf, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var msg models.NormalizedMessage
	if err := json.Unmarshal(buf, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}
