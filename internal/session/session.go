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
	// generation 在 SwapMessages / Clear 时自增。stream 入口捕获快照，
	// 每次 AppendChecked 比较——若不一致说明 session 被切换过，旧 goroutine 的写入应丢弃。
	// 防止被 cancel 的 subagent / 异步 MCP tool 在 swap 后污染新会话。
	generation uint64
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

// Generation 返回当前 generation 快照。stream goroutine 在入口捕获一次，
// 后续 AppendChecked 时传入该值；若中间发生 SwapMessages/Clear，generation 会自增，
// 旧 goroutine 的写入会被静默丢弃。
func (s *Session) Generation() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.generation
}

// AppendChecked 在指定 generation 下追加消息。
// 若 generation 已过期（被 SwapMessages/Clear 改过），返回 false 不追加，调用方应停止处理。
// 与 Append 共享相同的去重 + store 写入逻辑。
func (s *Session) AppendChecked(msg models.Message, gen uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation != gen {
		return false
	}
	if msg.Role == "tool" && msg.ToolCallID != "" {
		for _, existing := range s.msgs {
			if existing.Role == "tool" && existing.ToolCallID == msg.ToolCallID {
				return true
			}
		}
	}
	msg.Timestamp = time.Now()
	s.msgs = append(s.msgs, msg)
	s.UpdatedAt = time.Now()
	if s.store != nil {
		s.store.Append(s.Key, msg)
	}
	return true
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
	// 摘要使用 user 角色。智谱 GLM 等上游要求 messages 中 system 只能有一条且在最前，
	// 而 PromptBuilder 已在序列最前注入了一个 system；若这里再用 system 会出现「两条前导
	// system」，触发上游 "messages 参数非法" (code 1214)。改用 user 角色携带摘要可避免该冲突。
	s.msgs = append([]models.Message{
		{Role: "user", Content: "Previous conversation summary: " + summary, Timestamp: time.Now()},
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
	s.generation++ // 使旧 goroutine 的后续 AppendChecked 失败
	s.msgs = []models.Message{}
	s.UpdatedAt = time.Now()
	if s.store != nil {
		s.store.Replace(s.Key, s.msgs)
	}
}

// FixIncompleteToolCalls 修复孤立的 tool_calls：若 assistant 发出了 tool_calls 但后续
// 没有对应 Role=="tool" 消息（被 cancel 截断），合成 cancel tool message 补全。
// 必须持有 s.mu 写锁。
func (s *Session) FixIncompleteToolCalls() {
	if len(s.msgs) == 0 {
		return
	}
	answered := make(map[string]bool)
	for _, m := range s.msgs {
		if strings.EqualFold(m.Role, "tool") && m.ToolCallID != "" {
			answered[m.ToolCallID] = true
		}
	}
	var newMsgs []models.Message
	appended := false
	for _, m := range s.msgs {
		newMsgs = append(newMsgs, m)
		if strings.EqualFold(m.Role, "assistant") && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				if tc.ID != "" && !answered[tc.ID] {
					newMsgs = append(newMsgs, models.Message{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content:    "[cancelled by user before completion]",
						Timestamp:  time.Now(),
					})
					appended = true
				}
			}
		}
	}
	if appended {
		s.msgs = newMsgs
	}
}

// SwapMessages 原子替换全部消息（/resume 用），返回旧消息供归档。
func (s *Session) SwapMessages(newMsgs []models.Message) []models.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++ // 使旧 goroutine 的后续 AppendChecked 失败
	old := s.msgs
	s.msgs = make([]models.Message, len(newMsgs))
	copy(s.msgs, newMsgs)
	s.UpdatedAt = time.Now()
	if s.store != nil {
		s.store.Replace(s.Key, s.msgs)
	}
	return old
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
