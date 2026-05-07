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

type frameType string

const (
	frameTypeRequest  frameType = "request"
	frameTypeResponse frameType = "response"
)

type method string

const (
	methodRegister    method = "register"
	methodToolExecute method = "tool.execute"
	methodCancel      method = "cancel"
)

type frame struct {
	ID           string                 `json:"id"`
	Type         frameType              `json:"type"`
	Method       method                 `json:"method"`
	ReplyTo      string                 `json:"reply_to,omitempty"`
	PluginID     string                 `json:"plugin_id,omitempty"`
	CapabilityID string                 `json:"capability_id,omitempty"`
	Timestamp    int64                  `json:"timestamp"`
	Data         map[string]interface{} `json:"data,omitempty"`
}

type registration struct {
	PluginID        string       `json:"plugin_id"`
	ProtocolVersion int          `json:"protocol_version"`
	PluginVersion   string       `json:"plugin_version"`
	Name            string       `json:"name"`
	Capabilities    []capability `json:"capabilities"`
}

type capability struct {
	Type         string                 `json:"type"`
	CapabilityID string                 `json:"capability_id"`
	ToolName     string                 `json:"tool_name,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Parameters   map[string]interface{} `json:"parameters,omitempty"`
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
		log.Fatalf("missing required env: EVODUCK_PLUGIN_ID / EVODUCK_PLUGIN_TOKEN / EVODUCK_WS_URL")
	}

	wsWithQuery, err := buildWSURL(wsURL, pluginID, token)
	if err != nil {
		log.Fatalf("build ws url: %v", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsWithQuery, nil)
	if err != nil {
		log.Fatalf("dial plugin server: %v", err)
	}
	defer conn.Close()

	tracker := &requestTracker{cancels: make(map[string]context.CancelFunc)}

	capabilityID := pluginID + "/tool/echo"
	registrationData := map[string]interface{}{
		"plugin_id":        pluginID,
		"protocol_version": 1,
		"plugin_version":   "0.1.0",
		"name":             "Echo Tool Demo",
		"capabilities": []capability{
			{
				Type:         "tool",
				CapabilityID: capabilityID,
				ToolName:     "echo_tool",
				Description:  "Echoes input text with optional prefix",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"text": map[string]interface{}{
							"type":        "string",
							"description": "Text to echo back",
						},
						"prefix": map[string]interface{}{
							"type":        "string",
							"description": "Optional prefix added before text",
						},
					},
					"required": []string{"text"},
				},
			},
		},
	}

	if err := conn.WriteJSON(frame{
		ID:        "register-" + fmt.Sprint(time.Now().UnixNano()),
		Type:      frameTypeRequest,
		Method:    methodRegister,
		PluginID:  pluginID,
		Timestamp: time.Now().Unix(),
		Data:      registrationData,
	}); err != nil {
		log.Fatalf("send register: %v", err)
	}

	for {
		var incoming frame
		if err := conn.ReadJSON(&incoming); err != nil {
			log.Fatalf("read frame: %v", err)
		}

		if incoming.Type == frameTypeRequest && incoming.Method == methodToolExecute {
			go handleToolExecuteAsync(conn, tracker, pluginID, incoming)
			continue
		}

		if incoming.Type == frameTypeRequest && incoming.Method == methodCancel {
			handleCancel(tracker, incoming)
		}
	}
}

func buildWSURL(rawURL, pluginID, token string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("plugin_id", pluginID)
	query.Set("token", token)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func handleToolExecuteAsync(conn *websocket.Conn, tracker *requestTracker, pluginID string, incoming frame) {
	ctx, cancel := context.WithCancel(context.Background())
	tracker.set(incoming.ID, cancel)
	defer tracker.delete(incoming.ID)

	response, err := handleToolExecute(ctx, pluginID, incoming)
	if err != nil {
		response = frame{
			ID:        incoming.ID + ":response",
			Type:      frameTypeResponse,
			Method:    methodToolExecute,
			ReplyTo:   incoming.ID,
			PluginID:  pluginID,
			Timestamp: time.Now().Unix(),
			Data: map[string]interface{}{
				"ok":    false,
				"error": err.Error(),
			},
		}
	}

	if err := conn.WriteJSON(response); err != nil {
		log.Printf("write tool response: %v", err)
	}
}

func handleToolExecute(ctx context.Context, pluginID string, incoming frame) (frame, error) {
	toolName, _ := incoming.Data["tool_name"].(string)
	requestKey, _ := incoming.Data["request_key"].(string)
	if toolName != "echo_tool" {
		return frame{}, fmt.Errorf("unsupported tool: %s", toolName)
	}

	args, err := decodeArguments(incoming.Data["arguments"])
	if err != nil {
		return frame{}, err
	}

	text, _ := args["text"].(string)
	if strings.TrimSpace(text) == "" {
		return frame{}, fmt.Errorf("text is required")
	}
	if fail, _ := args["fail"].(bool); fail {
		return frame{}, fmt.Errorf("forced failure")
	}
	if delayMS, ok := intFromArgs(args, "delay_ms"); ok && delayMS > 0 {
		select {
		case <-ctx.Done():
			return frame{}, fmt.Errorf("request cancelled")
		case <-time.After(time.Duration(delayMS) * time.Millisecond):
		}
	}
	prefix, _ := args["prefix"].(string)
	content := strings.TrimSpace(strings.TrimSpace(prefix) + " " + text)

	return frame{
		ID:        incoming.ID + ":response",
		Type:      frameTypeResponse,
		Method:    methodToolExecute,
		ReplyTo:   incoming.ID,
		PluginID:  pluginID,
		Timestamp: time.Now().Unix(),
		Data: map[string]interface{}{
			"ok":          true,
			"content":     content,
			"request_key": requestKey,
		},
	}, nil
}

func handleCancel(tracker *requestTracker, incoming frame) {
	replyTo, _ := incoming.Data["reply_to"].(string)
	if replyTo == "" {
		return
	}
	tracker.cancel(replyTo)
}

func decodeArguments(raw interface{}) (map[string]interface{}, error) {
	if raw == nil {
		return map[string]interface{}{}, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var args map[string]interface{}
	if err := json.Unmarshal(b, &args); err != nil {
		return nil, err
	}
	return args, nil
}

func intFromArgs(args map[string]interface{}, key string) (int, bool) {
	value, ok := args[key]
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
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
