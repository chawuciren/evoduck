package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/internal/channels"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

func testDecider() *proxy.Decider {
	return proxy.NewDecider(config.ProxyConfig{Enabled: false})
}

func TestChannelBridgeReceivesMockChannelMessage(t *testing.T) {
	port := freeTCPPort(t)
	pluginPath := filepath.Clean(filepath.Join("..", "..", "plugins", "mock-channel"))
	manager := NewManager(config.PluginConfig{
		WSServer: config.WSServerConfig{Host: "127.0.0.1", Port: port},
		Plugins: map[string]config.PluginDef{
			"mock-channel": {
				Enabled:        true,
				Type:           "local",
				Command:        []string{"go", "run", pluginPath},
				Restart:        "never",
				RequestTimeout: 3000,
			},
		},
	}, testDecider())

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	})

	if err := manager.WaitReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("wait ready: %v", err)
	}

	bridges := manager.ListChannelBridges()
	if len(bridges) != 1 {
		t.Fatalf("expected 1 channel bridge, got %d", len(bridges))
	}

	msgCh := make(chan *models.NormalizedMessage, 1)
	bridges[0].OnMessage(func(msg *models.NormalizedMessage) {
		msgCh <- msg
	})
	if err := bridges[0].Connect(ctx); err != nil {
		t.Fatalf("connect bridge: %v", err)
	}

	select {
	case msg := <-msgCh:
		if msg.Content != "hello from mock channel" {
			t.Fatalf("unexpected message content: %q", msg.Content)
		}
		if msg.AccountID != "mock-channel" {
			t.Fatalf("unexpected account id: %q", msg.AccountID)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for channel message")
	}
}

func TestChannelBridgeSendToMockChannel(t *testing.T) {
	port := freeTCPPort(t)
	pluginPath := filepath.Clean(filepath.Join("..", "..", "plugins", "mock-channel"))
	ackFile := filepath.Join(t.TempDir(), "channel-send.json")
	manager := NewManager(config.PluginConfig{
		WSServer: config.WSServerConfig{Host: "127.0.0.1", Port: port},
		Plugins: map[string]config.PluginDef{
			"mock-channel": {
				Enabled:        true,
				Type:           "local",
				Command:        []string{"go", "run", pluginPath},
				Environment:    map[string]string{"MOCK_CHANNEL_ACK_FILE": ackFile},
				Restart:        "never",
				RequestTimeout: 3000,
			},
		},
	}, testDecider())

	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("start manager: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = manager.Shutdown(shutdownCtx)
	})

	if err := manager.WaitReady(ctx, 10*time.Second); err != nil {
		t.Fatalf("wait ready: %v", err)
	}

	channelMgr := channels.NewManager()
	bridges := manager.ListChannelBridges()
	if len(bridges) != 1 {
		t.Fatalf("expected 1 channel bridge, got %d", len(bridges))
	}
	channelMgr.Register(bridges[0])
	if err := channelMgr.ConnectAll(ctx); err != nil {
		t.Fatalf("connect all channels: %v", err)
	}

	err := channelMgr.SendToChannel(ctx, "mock-channel", &models.OutgoingMessage{
		Channel:  "mock-channel",
		TargetID: "user-1",
		Content:  "reply from gateway",
		Media: []models.OutgoingMedia{
			{
				Type:              "image",
				Name:              "demo.png",
				EncryptQueryParam: "enc=image-token",
				AESKey:            "aes-key",
			},
		},
		ThreadID: "thread-1",
	})
	if err != nil {
		t.Fatalf("send to mock channel: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(ackFile)
		if err == nil {
			var payload map[string]interface{}
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatalf("decode ack payload: %v", err)
			}
			if got, _ := payload["content"].(string); got != "reply from gateway" {
				t.Fatalf("unexpected sent content: %q", got)
			}
			media, ok := payload["media"].([]interface{})
			if !ok || len(media) != 1 {
				t.Fatalf("expected 1 media item, got %#v", payload["media"])
			}
			first, ok := media[0].(map[string]interface{})
			if !ok {
				t.Fatalf("unexpected media payload: %#v", media[0])
			}
			if got, _ := first["type"].(string); got != "image" {
				t.Fatalf("unexpected media type: %q", got)
			}
			if got, _ := first["encrypt_query_param"].(string); got != "enc=image-token" {
				t.Fatalf("unexpected media encrypt query param: %q", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for ack file: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
