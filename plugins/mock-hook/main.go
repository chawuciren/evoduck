package main

import (
	"encoding/json"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/internal/plugin"
	"github.com/gorilla/websocket"
)

type frame = plugin.Frame

func main() {
	pluginID := strings.TrimSpace(os.Getenv("EVODUCK_PLUGIN_ID"))
	token := strings.TrimSpace(os.Getenv("EVODUCK_PLUGIN_TOKEN"))
	wsURL := strings.TrimSpace(os.Getenv("EVODUCK_WS_URL"))
	hookFile := strings.TrimSpace(os.Getenv("MOCK_HOOK_EVENT_FILE"))
	blockToolName := strings.TrimSpace(os.Getenv("MOCK_HOOK_BLOCK_TOOL_NAME"))
	blockEvent := strings.TrimSpace(os.Getenv("MOCK_HOOK_BLOCK_EVENT"))
	recordEvent := strings.TrimSpace(os.Getenv("MOCK_HOOK_RECORD_EVENT"))
	recordFile := strings.TrimSpace(os.Getenv("MOCK_HOOK_RECORD_FILE"))
	patchEvent := strings.TrimSpace(os.Getenv("MOCK_HOOK_PATCH_EVENT"))
	patchValue := strings.TrimSpace(os.Getenv("MOCK_HOOK_PATCH_VALUE"))
	if pluginID == "" || token == "" || wsURL == "" {
		log.Fatalf("missing required env")
	}

	u, err := url.Parse(wsURL)
	if err != nil {
		log.Fatalf("parse ws url: %v", err)
	}
	q := u.Query()
	q.Set("plugin_id", pluginID)
	q.Set("token", token)
	u.RawQuery = q.Encode()

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatalf("dial plugin server: %v", err)
	}
	defer conn.Close()

	register := frame{
		ID:        "register-1",
		Type:      plugin.FrameTypeRequest,
		Method:    plugin.MethodRegister,
		PluginID:  pluginID,
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"plugin_id":        pluginID,
			"protocol_version": int(plugin.CurrentProtocolVersion),
			"plugin_version":   "0.1.0",
			"name":             "mock-hook",
			"capabilities": []map[string]interface{}{
				{
					"type":          string(plugin.CapabilityTypeHook),
					"capability_id": pluginID + "-hook",
					"events":        []string{"before_agent_start", "before_llm_call", "after_tool_call", "after_llm_complete", "before_tool_call", "before_message_send", "after_message_receive", "on_conversation_binding"},
					"priority":      100,
				},
			},
		},
	}
	if err := conn.WriteJSON(register); err != nil {
		log.Fatalf("register write: %v", err)
	}

	for {
		var incoming frame
		if err := conn.ReadJSON(&incoming); err != nil {
			log.Fatalf("read frame: %v", err)
		}

		if incoming.Type != plugin.FrameTypeRequest || incoming.Method != plugin.MethodHookTrigger {
			continue
		}

		if hookFile != "" {
			payload := ""
			if rawPayload, ok := incoming.Data["payload"]; ok {
				if buf, err := json.Marshal(rawPayload); err == nil {
					payload = string(buf)
				}
			}
			if err := os.MkdirAll(filepath.Dir(hookFile), 0o755); err == nil {
				_ = os.WriteFile(hookFile, []byte(payload), 0o644)
			}
		}
		if recordFile != "" {
			eventName, _ := incoming.Data["event"].(string)
			if recordEvent == "" || recordEvent == eventName {
				payload := ""
				if rawPayload, ok := incoming.Data["payload"]; ok {
					if buf, err := json.Marshal(rawPayload); err == nil {
						payload = string(buf)
					}
				}
				if err := os.MkdirAll(filepath.Dir(recordFile), 0o755); err == nil {
					_ = os.WriteFile(recordFile, []byte(payload), 0o644)
				}
			}
		}

		responseData := map[string]interface{}{"ok": true}
		eventName, _ := incoming.Data["event"].(string)
		if eventName == "before_tool_call" && blockToolName != "" {
			if payload, ok := incoming.Data["payload"].(map[string]interface{}); ok {
				if tool, ok := payload["tool"].(map[string]interface{}); ok {
					if toolName, _ := tool["name"].(string); toolName == blockToolName {
						responseData["block"] = true
						responseData["message"] = "blocked by mock hook"
					}
				}
			}
		}
		if blockEvent != "" && eventName == blockEvent {
			responseData["block"] = true
			responseData["message"] = "blocked by mock hook"
		}
		if patchEvent != "" && patchValue != "" && eventName == patchEvent {
			switch eventName {
			case "before_llm_call":
				responseData["patch"] = map[string]interface{}{"prepend_system_message": patchValue}
			case "before_message_send":
				responseData["patch"] = map[string]interface{}{"content": patchValue}
			}
		}

		_ = conn.WriteJSON(frame{
			ID:           incoming.ID + ":response",
			Type:         plugin.FrameTypeResponse,
			Method:       plugin.MethodHookTrigger,
			ReplyTo:      incoming.ID,
			PluginID:     pluginID,
			CapabilityID: incoming.CapabilityID,
			Timestamp:    time.Now().Unix(),
			Data:         responseData,
		})
	}
}
