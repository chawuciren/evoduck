package session

import (
	"strings"
	"sync"
	"time"

	"github.com/chawuciren/evoduck/pkg/models"
)

type Session struct {
	mu                  sync.RWMutex
	Key                 string // Session Key: "channel:user_id"
	ID                  string // Session ID (unique identifier)
	UserID              string // User ID extracted from Key for memory isolation
	Metadata            map[string]string
	msgs                []models.Message
	UpdatedAt           time.Time
	store               *JSONLStore
	inToolExecution     bool // Tracks whether runtime is processing tool calls
	pendingToolReplay   *models.Message
	pendingReplayActive bool
}

func NewSession(key, id string, store *JSONLStore) *Session {
	s := &Session{
		Key:       key,
		ID:        id,
		UserID:    extractUserID(key),
		Metadata:  make(map[string]string),
		UpdatedAt: time.Now(),
		store:     store,
	}
	if store != nil {
		s.msgs, _ = store.Load(key)
		if metadata, err := store.LoadMetadata(key); err == nil && len(metadata) > 0 {
			s.Metadata = metadata
		}
	}
	return s
}

// extractUserID 从 Session Key 中提取 UserID
// Session Key 格式:
//   - "channel:user_id" - 标准格式
//   - "agent:xxx:user:yyy:ws" - WebSocket (用户隔离，已简化)
//   - "agent:xxx:user:yyy:ws:conn_id" - WebSocket 旧格式 (兼容)
//   - "agent:xxx:ws:conn_id" - WebSocket 无用户 ID
func extractUserID(key string) string {
	if key == "" {
		return ""
	}

	parts := strings.Split(key, ":")

	// 标准格式: "channel:user_id"
	if len(parts) == 2 {
		return parts[1]
	}

	// WebSocket 带用户格式:
	//   - "agent:xxx:user:yyy:ws" (新, 5段)
	//   - "agent:xxx:user:yyy:ws:conn_id" (旧, 6段+)
	// parts[2] == "user" 即可
	if len(parts) >= 5 && parts[2] == "user" {
		return parts[3]
	}

	// WebSocket 无用户格式: "agent:xxx:ws:conn_id" - 无明确用户
	if len(parts) >= 3 && parts[2] == "ws" {
		return ""
	}

	// 其他格式，尝试取最后一部分
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}

	return ""
}

func (s *Session) Append(msg models.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// ToolCallID 去重：跳过已存在的 tool 消息
	if msg.Role == "tool" && msg.ToolCallID != "" {
		for _, existing := range s.msgs {
			if existing.Role == "tool" && existing.ToolCallID == msg.ToolCallID {
				return
			}
		}
	}

	msg.Timestamp = time.Now()
	s.msgs = append(s.msgs, msg)
	s.UpdatedAt = time.Now()
	if s.store != nil {
		s.store.Append(s.Key, msg)
	}
}

func (s *Session) GetMessages() []models.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]models.Message, len(s.msgs))
	copy(out, s.msgs)
	return out
}

func (s *Session) ReplaceWithSummary(summary string, recent []models.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append([]models.Message{
		{Role: "system", Content: "Previous conversation summary: " + summary, Timestamp: time.Now()},
	}, recent...)
	s.UpdatedAt = time.Now()
	if s.store != nil {
		s.store.Replace(s.Key, s.msgs)
	}
}

func (s *Session) FormatContext() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var parts []string
	for _, m := range s.msgs {
		parts = append(parts, m.Role+": "+m.Content)
	}
	return "## Session History\n" + strings.Join(parts, "\n")
}

func (s *Session) MessageCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.msgs)
}

func (s *Session) GetKey() string {
	return s.Key
}

// GetUserByID 获取用户 ID
func (s *Session) GetUserID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.UserID
}

func (s *Session) SetUserID(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.UserID = strings.TrimSpace(userID)
	s.UpdatedAt = time.Now()
}

func (s *Session) SetMetadataValue(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Metadata == nil {
		s.Metadata = make(map[string]string)
	}
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return
	}
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		delete(s.Metadata, trimmedKey)
	} else {
		s.Metadata[trimmedKey] = trimmedValue
	}
	s.UpdatedAt = time.Now()
	if s.store != nil {
		s.store.SaveMetadata(s.Key, s.Metadata)
	}
}

func (s *Session) GetMetadataValue(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Metadata == nil {
		return ""
	}
	return s.Metadata[strings.TrimSpace(key)]
}

func (s *Session) MetadataCopy() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Metadata) == 0 {
		return nil
	}
	out := make(map[string]string, len(s.Metadata))
	for k, v := range s.Metadata {
		out[k] = v
	}
	return out
}

// Clear 清空会话历史
func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = []models.Message{}
	s.UpdatedAt = time.Now()
	if s.store != nil {
		s.store.Replace(s.Key, s.msgs)
	}
}

// SetToolExecution marks the session as being in tool execution mode.
// During tool execution, the runtime manages the message sequence (assistant+tool_calls → tool messages).
// This prevents other components from inserting messages that would corrupt the sequence.
func (s *Session) SetToolExecution(inProgress bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inToolExecution = inProgress
}

// IsInToolExecution returns whether the session is currently in tool execution mode.
func (s *Session) IsInToolExecution() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inToolExecution
}

func (s *Session) SetPendingToolReplay(msg *models.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if msg == nil {
		s.pendingToolReplay = nil
		s.pendingReplayActive = false
		return
	}
	clone := *msg
	clone.Media = append([]models.OutgoingMedia(nil), msg.Media...)
	clone.ToolCalls = append([]models.ToolCall(nil), msg.ToolCalls...)
	clone.ReasoningMetadata = models.CloneReasoningReplay(msg.ReasoningMetadata)
	s.pendingToolReplay = &clone
	s.pendingReplayActive = true
}

func (s *Session) PendingToolReplay() *models.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.pendingReplayActive || s.pendingToolReplay == nil {
		return nil
	}
	clone := *s.pendingToolReplay
	clone.Media = append([]models.OutgoingMedia(nil), s.pendingToolReplay.Media...)
	clone.ToolCalls = append([]models.ToolCall(nil), s.pendingToolReplay.ToolCalls...)
	clone.ReasoningMetadata = models.CloneReasoningReplay(s.pendingToolReplay.ReasoningMetadata)
	return &clone
}

func (s *Session) ClearPendingToolReplay() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingToolReplay = nil
	s.pendingReplayActive = false
}
