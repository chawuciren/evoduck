package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
)

type SessionGateway interface {
	List() []session.SessionInfo
	Get(key string) (*session.Session, error)
	GetOrCreate(key string) *session.Session
	SendSessionMessage(ctx context.Context, sessionKey string, content string) (int, error)
	SendSessionOutgoingMessage(ctx context.Context, sessionKey string, outgoing *models.OutgoingMessage) (int, error)
	RunSessionInput(ctx context.Context, agentID, sessionKey, input string) error
}

type SessionGatewayProvider func() SessionGateway

type SessionToolPolicy struct {
	enabled    bool
	visibility map[models.Role]string
	allow      map[models.Role]map[string]bool
}

func NewSessionToolPolicy(cfg config.SessionToolConfig) SessionToolPolicy {
	policy := SessionToolPolicy{
		enabled: cfg.Enabled,
		visibility: map[models.Role]string{
			models.RoleEmployee: strings.TrimSpace(strings.ToLower(cfg.Visibility.Employee)),
			models.RoleCustomer: strings.TrimSpace(strings.ToLower(cfg.Visibility.Customer)),
		},
		allow: map[models.Role]map[string]bool{
			models.RoleEmployee: make(map[string]bool),
			models.RoleCustomer: make(map[string]bool),
		},
	}
	for _, name := range cfg.Allow.Employee {
		policy.allow[models.RoleEmployee][strings.TrimSpace(name)] = true
	}
	for _, name := range cfg.Allow.Customer {
		policy.allow[models.RoleCustomer][strings.TrimSpace(name)] = true
	}
	return policy
}

func (p SessionToolPolicy) IsAllowed(role models.Role, toolName string) bool {
	if role == models.RoleAdmin {
		return true
	}
	if !p.enabled {
		return false
	}
	allowed := p.allow[role]
	return allowed[toolName]
}

func (p SessionToolPolicy) CanAccess(role models.Role, currentSessionKey, currentUserID, currentAgentID, targetSessionKey string) bool {
	if role == models.RoleAdmin {
		return true
	}
	visibility := p.visibility[role]
	if visibility == "" {
		visibility = "self"
	}
	targetUserID := extractSessionUserID(targetSessionKey)
	targetAgentID := extractSessionAgentID(targetSessionKey)
	switch visibility {
	case "all":
		return true
	case "agent":
		return currentAgentID != "" && currentAgentID == targetAgentID
	case "user":
		return currentUserID != "" && currentUserID == targetUserID
	case "self":
		fallthrough
	default:
		return strings.TrimSpace(currentSessionKey) != "" && strings.TrimSpace(currentSessionKey) == strings.TrimSpace(targetSessionKey)
	}
}

type SessionListTool struct {
	gateway SessionGatewayProvider
	policy  SessionToolPolicy
	agentID string
	name    string
}

func NewSessionListTool(agentID string, gateway SessionGatewayProvider, policy SessionToolPolicy) *SessionListTool {
	return &SessionListTool{gateway: gateway, policy: policy, agentID: agentID, name: "sessions_list"}
}

func (t *SessionListTool) Name() string { return t.name }
func (t *SessionListTool) Description() string {
	return "List sessions visible to the current caller."
}
func (t *SessionListTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}
func (t *SessionListTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("sessions_list requires user context")
}
func (t *SessionListTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	if !t.policy.IsAllowed(role, t.name) {
		return "", fmt.Errorf("access denied: %s is not allowed for role %s", t.name, role)
	}
	currentSessionKey := SessionKeyFromContext(ctx)
	gateway, err := t.resolveGateway()
	if err != nil {
		return "", err
	}
	items := gateway.List()
	filtered := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if !t.policy.CanAccess(role, currentSessionKey, userID, t.agentID, item.Key) {
			continue
		}
		filtered = append(filtered, map[string]interface{}{
			"key":               item.Key,
			"agent_id":          item.AgentID,
			"user_id":           item.UserID,
			"session_kind":      item.SessionKind,
			"memory_policy":     item.MemoryPolicy,
			"message_count":     item.MessageCount,
			"updated_at":        item.UpdatedAt,
			"age_seconds":       item.AgeSeconds,
			"is_schedule":       item.IsSchedule,
			"is_memory_ignored": item.IsMemoryIgnored,
		})
	}
	sort.Slice(filtered, func(i, j int) bool {
		return fmt.Sprintf("%v", filtered[i]["key"]) < fmt.Sprintf("%v", filtered[j]["key"])
	})
	data, err := json.MarshalIndent(filtered, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

const (
	maxSessionHistoryLimit    = 50
	sessionHistoryPreviewRunes = 160
)

type SessionHistoryTool struct {
	gateway SessionGatewayProvider
	policy  SessionToolPolicy
	agentID string
	name    string
}

func NewSessionHistoryTool(agentID string, gateway SessionGatewayProvider, policy SessionToolPolicy) *SessionHistoryTool {
	return &SessionHistoryTool{gateway: gateway, policy: policy, agentID: agentID, name: "sessions_history"}
}

func (t *SessionHistoryTool) Name() string { return t.name }
func (t *SessionHistoryTool) Description() string {
	return "Read recent messages from a visible session."
}
func (t *SessionHistoryTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_key": map[string]interface{}{"type": "string", "description": "Target session key. Must not be the current session."},
			"limit":       map[string]interface{}{"type": "integer", "description": "Maximum messages to return, default 20, max 50"},
		},
		"required": []string{"session_key"},
	}
}
func (t *SessionHistoryTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("sessions_history requires user context")
}
func (t *SessionHistoryTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	if !t.policy.IsAllowed(role, t.name) {
		return "", fmt.Errorf("access denied: %s is not allowed for role %s", t.name, role)
	}
	sessionKey, _ := args["session_key"].(string)
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return "", fmt.Errorf("session_key is required")
	}
	currentSessionKey := strings.TrimSpace(SessionKeyFromContext(ctx))
	if currentSessionKey != "" && currentSessionKey == sessionKey {
		return "", fmt.Errorf("sessions_history cannot read the current session; current context already includes its history")
	}
	if !t.policy.CanAccess(role, currentSessionKey, userID, t.agentID, sessionKey) {
		return "", fmt.Errorf("access denied to session: %s", sessionKey)
	}
	limit := 20
	if raw, ok := args["limit"].(float64); ok && int(raw) > 0 {
		limit = int(raw)
	}
	if limit > maxSessionHistoryLimit {
		limit = maxSessionHistoryLimit
	}
	gateway, err := t.resolveGateway()
	if err != nil {
		return "", err
	}
	sess, err := gateway.Get(sessionKey)
	if err != nil {
		return "", err
	}
	msgs := sess.GetMessages()
	if limit > len(msgs) {
		limit = len(msgs)
	}
	start := len(msgs) - limit
	if start < 0 {
		start = 0
	}
	data, err := json.MarshalIndent(projectSessionHistoryMessages(msgs[start:]), "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

type SessionSendTool struct {
	gateway SessionGatewayProvider
	policy  SessionToolPolicy
	agentID string
	name    string
}

func NewSessionSendTool(agentID string, gateway SessionGatewayProvider, policy SessionToolPolicy) *SessionSendTool {
	return &SessionSendTool{gateway: gateway, policy: policy, agentID: agentID, name: "sessions_send"}
}

func (t *SessionSendTool) Name() string { return t.name }
func (t *SessionSendTool) Description() string {
	return "Send content or media to a visible session and append it to that session history. Defaults to the current session when session_key is omitted."
}
func (t *SessionSendTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_key": map[string]interface{}{"type": "string", "description": "Target session key. Defaults to the current session when omitted."},
			"message":     map[string]interface{}{"type": "string", "description": "Backward-compatible alias of content."},
			"content":     map[string]interface{}{"type": "string", "description": "Text content to send."},
			"media": map[string]interface{}{
				"type":        "array",
				"description": "Optional media attachments to send with the message.",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"type":                map[string]interface{}{"type": "string", "description": "Media type: image, audio, voice, video, or file."},
						"name":                map[string]interface{}{"type": "string", "description": "Optional display filename."},
						"mime_type":           map[string]interface{}{"type": "string", "description": "Optional MIME type."},
						"url":                 map[string]interface{}{"type": "string", "description": "Optional source URL for channels that support it."},
						"path":                map[string]interface{}{"type": "string", "description": "Optional local file path to upload."},
						"data":                map[string]interface{}{"type": "string", "description": "Optional base64-encoded raw bytes."},
						"encrypt_query_param": map[string]interface{}{"type": "string", "description": "Optional pre-uploaded channel media reference."},
						"aes_key":             map[string]interface{}{"type": "string", "description": "Optional AES key paired with encrypt_query_param."},
						"file_size":           map[string]interface{}{"type": "integer", "description": "Optional original file size in bytes."},
					},
					"required": []string{"type"},
				},
			},
		},
	}
}
func (t *SessionSendTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("sessions_send requires user context")
}
func (t *SessionSendTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	if !t.policy.IsAllowed(role, t.name) {
		return "", fmt.Errorf("access denied: %s is not allowed for role %s", t.name, role)
	}
	sessionKey, _ := args["session_key"].(string)
	message, _ := args["message"].(string)
	content, _ := args["content"].(string)
	sessionKey = strings.TrimSpace(sessionKey)
	message = strings.TrimSpace(message)
	content = strings.TrimSpace(content)
	if content == "" {
		content = message
	}
	currentSessionKey := SessionKeyFromContext(ctx)
	if sessionKey == "" {
		sessionKey = strings.TrimSpace(currentSessionKey)
	}
	if sessionKey == "" {
		return "", fmt.Errorf("session_key is required when there is no current session")
	}
	media, err := decodeOutgoingMedia(args["media"])
	if err != nil {
		return "", err
	}
	if content == "" && len(media) == 0 {
		return "", fmt.Errorf("content, message, or media is required")
	}
	if !t.policy.CanAccess(role, currentSessionKey, userID, t.agentID, sessionKey) {
		return "", fmt.Errorf("access denied to session: %s", sessionKey)
	}
	gateway, err := t.resolveGateway()
	if err != nil {
		return "", err
	}
	if ambiguous := findAmbiguousSessionTargets(gateway.List(), sessionKey); len(ambiguous) > 0 {
		return "", fmt.Errorf("session_key %q is ambiguous; use one of: %s", sessionKey, strings.Join(ambiguous, ", "))
	}
	_, err = gateway.SendSessionOutgoingMessage(ctx, sessionKey, &models.OutgoingMessage{
		Content: content,
		Media:   media,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Sent message to session %s", sessionKey), nil
}

func findAmbiguousSessionTargets(sessions []session.SessionInfo, sessionKey string) []string {
	target := strings.TrimSpace(sessionKey)
	if target == "" {
		return nil
	}
	exact := false
	prefix := target + ":"
	options := make([]string, 0)
	for _, item := range sessions {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		if key == target {
			exact = true
		}
		if strings.HasPrefix(key, prefix) {
			options = append(options, key)
		}
	}
	if !exact || len(options) == 0 {
		return nil
	}
	sort.Strings(options)
	return options
}

type SessionRunTool struct {
	gateway SessionGatewayProvider
	policy  SessionToolPolicy
	agentID string
	name    string
}

func NewSessionRunTool(agentID string, gateway SessionGatewayProvider, policy SessionToolPolicy) *SessionRunTool {
	return &SessionRunTool{gateway: gateway, policy: policy, agentID: agentID, name: "sessions_run"}
}

func (t *SessionRunTool) Name() string { return t.name }
func (t *SessionRunTool) Description() string {
	return "Run one input turn inside another visible session using its existing context. Requires an explicit target session key and does not create new session types."
}
func (t *SessionRunTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_key": map[string]interface{}{"type": "string", "description": "Target existing session key"},
			"input":       map[string]interface{}{"type": "string", "description": "Input content to run in the target session"},
		},
		"required": []string{"session_key", "input"},
	}
}
func (t *SessionRunTool) Execute(args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("sessions_run requires user context")
}
func (t *SessionRunTool) ExecuteWithUserContext(ctx context.Context, args map[string]interface{}, role models.Role, userID string, userIsolationEnabled bool, workspace string) (string, error) {
	if !t.policy.IsAllowed(role, t.name) {
		return "", fmt.Errorf("access denied: %s is not allowed for role %s", t.name, role)
	}
	sessionKey, _ := args["session_key"].(string)
	input, _ := args["input"].(string)
	sessionKey = strings.TrimSpace(sessionKey)
	input = strings.TrimSpace(input)
	if sessionKey == "" {
		return "", fmt.Errorf("session_key is required")
	}
	if input == "" {
		return "", fmt.Errorf("input is required")
	}
	currentSessionKey := SessionKeyFromContext(ctx)
	if !t.policy.CanAccess(role, currentSessionKey, userID, t.agentID, sessionKey) {
		return "", fmt.Errorf("access denied to session: %s", sessionKey)
	}
	gateway, err := t.resolveGateway()
	if err != nil {
		return "", err
	}
	if err := gateway.RunSessionInput(ctx, t.agentID, sessionKey, input); err != nil {
		return "", err
	}
	return fmt.Sprintf("Triggered session run for %s", sessionKey), nil
}

func (t *SessionListTool) resolveGateway() (SessionGateway, error) {
	if t.gateway == nil {
		return nil, fmt.Errorf("session gateway unavailable")
	}
	gateway := t.gateway()
	if gateway == nil {
		return nil, fmt.Errorf("session gateway unavailable")
	}
	return gateway, nil
}

func (t *SessionHistoryTool) resolveGateway() (SessionGateway, error) {
	if t.gateway == nil {
		return nil, fmt.Errorf("session gateway unavailable")
	}
	gateway := t.gateway()
	if gateway == nil {
		return nil, fmt.Errorf("session gateway unavailable")
	}
	return gateway, nil
}

func (t *SessionSendTool) resolveGateway() (SessionGateway, error) {
	if t.gateway == nil {
		return nil, fmt.Errorf("session gateway unavailable")
	}
	gateway := t.gateway()
	if gateway == nil {
		return nil, fmt.Errorf("session gateway unavailable")
	}
	return gateway, nil
}

func projectSessionHistoryMessages(msgs []models.Message) []map[string]interface{} {
	projected := make([]map[string]interface{}, 0, len(msgs))
	for _, msg := range msgs {
		item := map[string]interface{}{
			"role":      msg.Role,
			"timestamp": msg.Timestamp,
		}
		if msg.Content != "" {
			if summary, ok := summarizeSessionHistoryPayload(msg); ok {
				item["content"] = summary
				item["content_collapsed"] = true
				item["content_original_length"] = len(msg.Content)
			} else {
				item["content"] = msg.Content
			}
		}
		if len(msg.Media) > 0 {
			item["media"] = msg.Media
		}
		if msg.ThinkingContent != "" {
			item["thinking_content"] = msg.ThinkingContent
		}
		if msg.ReasoningMetadata != nil {
			item["reasoning_metadata"] = msg.ReasoningMetadata
		}
		if len(msg.ToolCalls) > 0 {
			item["tool_calls"] = msg.ToolCalls
		}
		if msg.ToolCallID != "" {
			item["tool_call_id"] = msg.ToolCallID
		}
		projected = append(projected, item)
	}
	return projected
}

func summarizeSessionHistoryPayload(msg models.Message) (string, bool) {
	if msg.Role != "tool" || msg.Content == "" {
		return "", false
	}
		var nested []models.Message
	if err := json.Unmarshal([]byte(msg.Content), &nested); err != nil || !looksLikeSessionHistoryMessages(nested) {
		return "", false
	}
	roleCounts := make(map[string]int)
	nestedHistoryCount := 0
	for _, nestedMsg := range nested {
		roleCounts[nestedMsg.Role]++
		if nestedMsg.Role == "tool" {
			var deeper []models.Message
			if err := json.Unmarshal([]byte(nestedMsg.Content), &deeper); err == nil && looksLikeSessionHistoryMessages(deeper) {
				nestedHistoryCount++
			}
		}
	}
	parts := make([]string, 0, len(roleCounts))
	for _, role := range []string{"system", "user", "assistant", "tool"} {
		if roleCounts[role] == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%d", role, roleCounts[role]))
	}
	for role, count := range roleCounts {
		if role == "system" || role == "user" || role == "assistant" || role == "tool" || count == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%d", role, count))
	}
	sort.Strings(parts)
	preview := summarizeMessagePreview(nested)
	summary := fmt.Sprintf("[collapsed sessions_history output: messages=%d; roles=%s", len(nested), strings.Join(parts, ", "))
	if nestedHistoryCount > 0 {
		summary += fmt.Sprintf("; nested_history_tool_messages=%d", nestedHistoryCount)
	}
	if preview != "" {
		summary += fmt.Sprintf("; preview=%s", preview)
	}
	summary += "]"
	return summary, true
}

func looksLikeSessionHistoryMessages(msgs []models.Message) bool {
	if len(msgs) == 0 {
		return false
	}
	validRoles := map[string]bool{
		"system":    true,
		"user":      true,
		"assistant": true,
		"tool":      true,
	}
	validCount := 0
	for _, msg := range msgs {
		if !validRoles[msg.Role] {
			return false
		}
		validCount++
	}
	return validCount > 0
}

func summarizeMessagePreview(msgs []models.Message) string {
	previews := make([]string, 0, 3)
	for _, msg := range msgs {
		text := strings.TrimSpace(msg.Content)
		if text == "" && len(msg.ToolCalls) > 0 {
			text = fmt.Sprintf("tool_calls=%d", len(msg.ToolCalls))
		}
		if text == "" {
			continue
		}
		previews = append(previews, fmt.Sprintf("%s:%s", msg.Role, truncateRunes(text, sessionHistoryPreviewRunes/3)))
		if len(previews) == 3 {
			break
		}
	}
	return truncateRunes(strings.Join(previews, " | "), sessionHistoryPreviewRunes)
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

func decodeOutgoingMedia(raw interface{}) ([]models.OutgoingMedia, error) {
	if raw == nil {
		return nil, nil
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal media payload: %w", err)
	}
	var media []models.OutgoingMedia
	if err := json.Unmarshal(buf, &media); err != nil {
		return nil, fmt.Errorf("decode media payload: %w", err)
	}
	for i := range media {
		media[i].Type = strings.TrimSpace(media[i].Type)
		media[i].Name = strings.TrimSpace(media[i].Name)
		media[i].MimeType = strings.TrimSpace(media[i].MimeType)
		media[i].URL = strings.TrimSpace(media[i].URL)
		media[i].Path = strings.TrimSpace(media[i].Path)
		media[i].Data = strings.TrimSpace(media[i].Data)
		media[i].EncryptQueryParam = strings.TrimSpace(media[i].EncryptQueryParam)
		media[i].AESKey = strings.TrimSpace(media[i].AESKey)
		if media[i].Type == "" {
			return nil, fmt.Errorf("media[%d].type is required", i)
		}
	}
	return media, nil
}

func (t *SessionRunTool) resolveGateway() (SessionGateway, error) {
	if t.gateway == nil {
		return nil, fmt.Errorf("session gateway unavailable")
	}
	gateway := t.gateway()
	if gateway == nil {
		return nil, fmt.Errorf("session gateway unavailable")
	}
	return gateway, nil
}

func extractSessionUserID(key string) string {
	if key == "" {
		return ""
	}
	parts := strings.Split(key, ":")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "user" {
			return parts[i+1]
		}
	}
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

func extractSessionAgentID(key string) string {
	if key == "" {
		return ""
	}
	parts := strings.Split(key, ":")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "agent" {
			return parts[i+1]
		}
	}
	return ""
}
