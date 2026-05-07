package plugin

import (
	"context"
	"fmt"
	"sync"

	"github.com/chawuciren/evoduck/internal/channels"
	"github.com/chawuciren/evoduck/pkg/models"
)

type ChannelBridge struct {
	pluginID     string
	capabilityID string
	bridgeName   string
	accountID    string
	manager      *Manager

	mu      sync.Mutex
	handler func(*models.NormalizedMessage)
	pending []*models.NormalizedMessage
}

func NewChannelBridge(manager *Manager, pluginID string, capability Capability) *ChannelBridge {
	return &ChannelBridge{
		pluginID:     pluginID,
		capabilityID: capability.CapabilityID,
		bridgeName:   capability.BridgeName,
		accountID:    capability.AccountID,
		manager:      manager,
	}
}

func (b *ChannelBridge) Name() string { return b.bridgeName }

func (b *ChannelBridge) Connect(ctx context.Context) error {
	if b.manager == nil || b.manager.transport == nil {
		return fmt.Errorf("plugin transport is not initialized")
	}
	return nil
}

func (b *ChannelBridge) Disconnect() error { return nil }

func (b *ChannelBridge) OnMessage(handler func(*models.NormalizedMessage)) {
	b.mu.Lock()
	b.handler = handler
	pending := append([]*models.NormalizedMessage(nil), b.pending...)
	b.pending = nil
	b.mu.Unlock()

	if handler == nil {
		return
	}
	for _, msg := range pending {
		handler(msg)
	}
}

func (b *ChannelBridge) Send(ctx context.Context, msg *models.OutgoingMessage) error {
	if b.manager == nil || b.manager.transport == nil {
		return fmt.Errorf("plugin transport is not initialized")
	}
	_, err := b.manager.transport.SendRequest(ctx, b.pluginID, MethodChannelSend, b.capabilityID, map[string]interface{}{
		"account_id":    b.accountID,
		"channel":       msg.Channel,
		"target_id":     msg.TargetID,
		"content":       msg.Content,
		"media":         msg.Media,
		"thread_id":     msg.ThreadID,
		"context_token": msg.ContextToken,
	})
	return err
}

func (b *ChannelBridge) Broadcast(ctx context.Context, content string, excludeTarget string) error {
	return b.Send(ctx, &models.OutgoingMessage{
		Channel:  b.bridgeName,
		TargetID: "*",
		Content:  content,
		ThreadID: excludeTarget,
	})
}

func (b *ChannelBridge) deliver(msg *models.NormalizedMessage) {
	b.mu.Lock()
	handler := b.handler
	if handler == nil {
		b.pending = append(b.pending, msg)
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()

	handler(msg)
}

var _ channels.Bridge = (*ChannelBridge)(nil)
