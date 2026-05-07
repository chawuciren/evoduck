package channels

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/chawuciren/evoduck/pkg/models"
)

// Bridge 渠道桥接接口
type Bridge interface {
	Name() string
	Connect(ctx context.Context) error
	Disconnect() error
	OnMessage(handler func(*models.NormalizedMessage))
	Send(ctx context.Context, msg *models.OutgoingMessage) error
	Broadcast(ctx context.Context, content string, excludeTarget string) error
}

// ProactiveBridge reports whether a channel supports long-lived proactive delivery
// without relying on a short-lived reply token/context.
type ProactiveBridge interface {
	SupportsProactiveSend() bool
}

// TypingBridge reports whether a channel can expose a transient typing/inputing state.
type TypingBridge interface {
	SetTyping(ctx context.Context, msg *models.NormalizedMessage, active bool) error
}

// EventBridge allows a channel to receive semantic runtime events and decide
// its own delivery policy instead of receiving only flattened outbound text.
type EventBridge interface {
	HandleEvent(ctx context.Context, target *models.NormalizedMessage, event *models.ChannelEvent) error
}

var ErrEventDeliveryUnsupported = errors.New("channel does not support event delivery")

// Manager 渠道管理器
type Manager struct {
	mu       sync.RWMutex
	bridges  map[string]Bridge
	handlers []func(*models.NormalizedMessage)
}

// NewManager 创建渠道管理器
func NewManager() *Manager {
	return &Manager{
		bridges:  make(map[string]Bridge),
		handlers: make([]func(*models.NormalizedMessage), 0),
	}
}

// Register 注册渠道
func (m *Manager) Register(bridge Bridge) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bridges[bridge.Name()] = bridge
}

// Get 获取渠道
func (m *Manager) Get(name string) (Bridge, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.bridges[name]
	if !ok {
		return nil, fmt.Errorf("channel not found: %s", name)
	}
	return b, nil
}

// ConnectAll 连接所有渠道
func (m *Manager) ConnectAll(ctx context.Context) error {
	m.mu.RLock()
	bridges := make([]Bridge, 0, len(m.bridges))
	for _, b := range m.bridges {
		bridges = append(bridges, b)
	}
	m.mu.RUnlock()

	// 先设置每个 bridge 的 handler，再连接
	for _, b := range bridges {
		// 让每个 bridge 的消息路由到 Manager 的 handlers
		b.OnMessage(func(msg *models.NormalizedMessage) {
			m.RouteMessage(msg)
		})
	}

	// 然后连接
	for _, b := range bridges {
		if err := b.Connect(ctx); err != nil {
			return fmt.Errorf("connect channel %s: %w", b.Name(), err)
		}
	}
	return nil
}

// DisconnectAll 断开所有渠道
func (m *Manager) DisconnectAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, b := range m.bridges {
		b.Disconnect()
	}
}

// OnMessage 注册消息处理器
func (m *Manager) OnMessage(handler func(*models.NormalizedMessage)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers = append(m.handlers, handler)
}

// RouteMessage 路由消息到处理器
func (m *Manager) RouteMessage(msg *models.NormalizedMessage) {
	m.mu.RLock()
	handlers := make([]func(*models.NormalizedMessage), len(m.handlers))
	copy(handlers, m.handlers)
	m.mu.RUnlock()

	for _, h := range handlers {
		go h(msg)
	}
}

// SendToChannel 发送消息到指定渠道
func (m *Manager) SendToChannel(ctx context.Context, channel string, msg *models.OutgoingMessage) error {
	b, err := m.Get(channel)
	if err != nil {
		return err
	}
	return b.Send(ctx, msg)
}

// SupportsProactiveSend reports whether the named channel can send proactively
// without a short-lived reply token/context.
func (m *Manager) SupportsProactiveSend(channel string) bool {
	b, err := m.Get(channel)
	if err != nil {
		return false
	}
	proactive, ok := b.(ProactiveBridge)
	if !ok {
		return false
	}
	return proactive.SupportsProactiveSend()
}

// SetTyping toggles the typing/inputing state for a channel when supported.
func (m *Manager) SetTyping(ctx context.Context, channel string, msg *models.NormalizedMessage, active bool) error {
	b, err := m.Get(channel)
	if err != nil {
		return err
	}
	typing, ok := b.(TypingBridge)
	if !ok {
		return nil
	}
	return typing.SetTyping(ctx, msg, active)
}

// HandleEvent forwards a semantic channel event to the named bridge when supported.
func (m *Manager) HandleEvent(ctx context.Context, channel string, target *models.NormalizedMessage, event *models.ChannelEvent) error {
	b, err := m.Get(channel)
	if err != nil {
		return err
	}
	eventBridge, ok := b.(EventBridge)
	if !ok {
		return fmt.Errorf("%w: %s", ErrEventDeliveryUnsupported, channel)
	}
	return eventBridge.HandleEvent(ctx, target, event)
}

// Broadcast 广播消息到所有渠道
func (m *Manager) Broadcast(ctx context.Context, content string, excludeTarget string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, b := range m.bridges {
		if err := b.Broadcast(ctx, content, excludeTarget); err != nil {
			// 记录错误但不中断
			continue
		}
	}
	return nil
}

// List 列出所有渠道
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var names []string
	for name := range m.bridges {
		names = append(names, name)
	}
	return names
}
