package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func main() {
	pluginID := strings.TrimSpace(os.Getenv("EVODUCK_PLUGIN_ID"))
	token := strings.TrimSpace(os.Getenv("EVODUCK_PLUGIN_TOKEN"))
	wsURL := strings.TrimSpace(os.Getenv("EVODUCK_WS_URL"))
	ackFile := strings.TrimSpace(os.Getenv("MOCK_CHANNEL_ACK_FILE"))
	delayMS := 300
	if rawDelay := strings.TrimSpace(os.Getenv("MOCK_CHANNEL_MESSAGE_DELAY_MS")); rawDelay != "" {
		if parsed, err := strconv.Atoi(rawDelay); err == nil && parsed >= 0 {
			delayMS = parsed
		}
	}
	if pluginID == "" || token == "" || wsURL == "" {
		log.Fatalf("missing required env")
	}

	parsed, err := url.Parse(wsURL)
	if err != nil {
		log.Fatal(err)
	}
	query := parsed.Query()
	query.Set("plugin_id", pluginID)
	query.Set("token", token)
	parsed.RawQuery = query.Encode()

	conn, _, err := websocket.DefaultDialer.Dial(parsed.String(), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	capabilityID := pluginID + "/channel/default"
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
			"name":             "Mock Channel Demo",
			"capabilities": []map[string]interface{}{
				{
					"type":          "channel",
					"capability_id": capabilityID,
					"bridge_name":   "mock-channel",
					"account_id":    "mock-channel",
				},
			},
		},
	}
	if err := conn.WriteJSON(register); err != nil {
		log.Fatal(err)
	}

	go func() {
		time.Sleep(time.Duration(delayMS) * time.Millisecond)
		_ = conn.WriteJSON(frame{
			ID:           fmt.Sprintf("channel-message-%d", time.Now().UnixNano()),
			Type:         "event",
			Method:       "channel.message",
			PluginID:     pluginID,
			CapabilityID: capabilityID,
			Timestamp:    time.Now().Unix(),
			Data: map[string]interface{}{
				"sender_id":     "user-1",
				"user_id":       "user-1",
				"content":       "hello from mock channel",
				"thread_id":     "thread-1",
				"is_dm":         true,
				"context_token": "ctx-1",
			},
		})
	}()

	for {
		var incoming frame
		if err := conn.ReadJSON(&incoming); err != nil {
			log.Fatal(err)
		}
		if incoming.Type == "request" && incoming.Method == "channel.send" {
			if ackFile != "" {
				content := ""
				if media, ok := incoming.Data["media"].([]interface{}); ok && len(media) > 0 {
					if body, err := json.Marshal(incoming.Data); err == nil {
						content = string(body)
					}
				} else if text, ok := incoming.Data["content"].(string); ok {
					content = text
				}
				if err := os.MkdirAll(filepath.Dir(ackFile), 0o755); err == nil {
					_ = os.WriteFile(ackFile, []byte(content), 0o644)
				}
			}
			_ = conn.WriteJSON(frame{
				ID:        incoming.ID + ":response",
				Type:      "response",
				Method:    "channel.send",
				ReplyTo:   incoming.ID,
				PluginID:  pluginID,
				Timestamp: time.Now().Unix(),
				Data: map[string]interface{}{
					"ok": true,
				},
			})
		}
	}
}
