package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type frame struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Method       string                 `json:"method"`
	ReplyTo      string                 `json:"reply_to,omitempty"`
	PluginID     string                 `json:"plugin_id,omitempty"`
	CapabilityID string                 `json:"capability_id,omitempty"`
	Timestamp    int64                  `json:"timestamp"`
	Data         map[string]interface{} `json:"data,omitempty"`
}

type requestTracker struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func main() {
	pluginID := strings.TrimSpace(os.Getenv("EVODUCK_PLUGIN_ID"))
	token := strings.TrimSpace(os.Getenv("EVODUCK_PLUGIN_TOKEN"))
	wsURL := strings.TrimSpace(os.Getenv("EVODUCK_WS_URL"))
	if pluginID == "" || token == "" || wsURL == "" {
		log.Fatalf("missing required env")
	}

	parsed, err := url.Parse(wsURL)
	if err != nil {
		log.Fatal(err)
	}
	q := parsed.Query()
	q.Set("plugin_id", pluginID)
	q.Set("token", token)
	parsed.RawQuery = q.Encode()

	conn, _, err := websocket.DefaultDialer.Dial(parsed.String(), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	tracker := &requestTracker{cancels: make(map[string]context.CancelFunc)}

	capabilityID := pluginID + "/provider/default"
	register := frame{
		ID:        fmt.Sprintf("register-%d", time.Now().UnixNano()),
		Type:      "request",
		Method:    "register",
		PluginID:  pluginID,
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"plugin_id":        pluginID,
			"protocol_version": 1,
			"plugin_version":   "0.1.0",
			"name":             "Mock Provider Demo",
			"capabilities": []map[string]interface{}{
				{
					"type":          "provider",
					"capability_id": capabilityID,
					"provider_name": "mock-provider",
					"models": []map[string]interface{}{
						{
							"id":                 "mock-model",
							"name":               "mock-model",
							"context_window":     128000,
							"max_tokens":         8192,
							"supports_tools":     true,
							"supports_streaming": true,
						},
					},
				},
			},
		},
	}
	if err := conn.WriteJSON(register); err != nil {
		log.Fatal(err)
	}

	for {
		var incoming frame
		if err := conn.ReadJSON(&incoming); err != nil {
			log.Fatal(err)
		}
		if incoming.Type == "request" && incoming.Method == "provider.chat" {
			go handleProviderChatAsync(conn, tracker, pluginID, incoming)
			continue
		}
		if incoming.Type == "request" && incoming.Method == "cancel" {
			handleCancel(tracker, incoming)
		}
	}
}

func handleProviderChatAsync(conn *websocket.Conn, tracker *requestTracker, pluginID string, incoming frame) {
	ctx, cancel := context.WithCancel(context.Background())
	tracker.set(incoming.ID, cancel)
	defer tracker.delete(incoming.ID)

	eventFrames, err := buildProviderEvents(ctx, pluginID, incoming)
	if err != nil {
		_ = conn.WriteJSON(frame{
			ID:        incoming.ID + ":response",
			Type:      "response",
			Method:    "provider.chat",
			ReplyTo:   incoming.ID,
			PluginID:  pluginID,
			Timestamp: time.Now().Unix(),
			Data: map[string]interface{}{
				"ok":    false,
				"error": err.Error(),
			},
		})
		return
	}
	for _, event := range eventFrames {
		if err := conn.WriteJSON(event); err != nil {
			log.Printf("write provider frame error: %v", err)
			return
		}
	}
}

func buildProviderEvents(ctx context.Context, pluginID string, incoming frame) ([]frame, error) {
	delayMS, _ := intFromRequest(incoming.Data, "delay_ms")
	if delayMS > 0 {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("provider request cancelled")
		case <-time.After(time.Duration(delayMS) * time.Millisecond):
		}
	}

	includeToolCalls, _ := incoming.Data["include_tool_calls"].(bool)
	events := []frame{
		{
			ID:        incoming.ID + ":content",
			Type:      "event",
			Method:    "provider.event",
			ReplyTo:   incoming.ID,
			PluginID:  pluginID,
			Timestamp: time.Now().Unix(),
			Data: map[string]interface{}{
				"event_type": "content",
				"content":    "mock provider says hello",
			},
		},
	}

	if includeToolCalls {
		events = append(events, frame{
			ID:        incoming.ID + ":toolcalls",
			Type:      "event",
			Method:    "provider.event",
			ReplyTo:   incoming.ID,
			PluginID:  pluginID,
			Timestamp: time.Now().Unix(),
			Data: map[string]interface{}{
				"event_type": "tool_calls",
				"tool_calls": []map[string]interface{}{
					{
						"id":   "call_mock_1",
						"type": "function",
						"function": map[string]interface{}{
							"name":      "echo_tool",
							"arguments": `{"text":"from provider"}`,
						},
					},
				},
			},
		})
	}

	events = append(events,
		frame{
			ID:        incoming.ID + ":stop",
			Type:      "event",
			Method:    "provider.event",
			ReplyTo:   incoming.ID,
			PluginID:  pluginID,
			Timestamp: time.Now().Unix(),
			Data: map[string]interface{}{
				"event_type": "stop",
			},
		},
		frame{
			ID:        incoming.ID + ":response",
			Type:      "response",
			Method:    "provider.chat",
			ReplyTo:   incoming.ID,
			PluginID:  pluginID,
			Timestamp: time.Now().Unix(),
			Data: map[string]interface{}{
				"ok": true,
			},
		},
	)
	return events, nil
}

func handleCancel(tracker *requestTracker, incoming frame) {
	replyTo, _ := incoming.Data["reply_to"].(string)
	if replyTo == "" {
		return
	}
	tracker.cancel(replyTo)
}

func intFromRequest(data map[string]interface{}, key string) (int, bool) {
	value, ok := data[key]
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case float64:
		return int(v), true
	case string:
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func (r *requestTracker) set(id string, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancels[id] = cancel
}

func (r *requestTracker) delete(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cancels, id)
}

func (r *requestTracker) cancel(id string) {
	r.mu.Lock()
	cancel, ok := r.cancels[id]
	r.mu.Unlock()
	if ok {
		cancel()
	}
}

var _ = json.Marshal
