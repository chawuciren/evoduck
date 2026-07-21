package session

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	mu           sync.RWMutex
	sessions     map[string]*Session
	store        *JSONLStore
	archiveStore *ArchiveStore // 会话归档存储（/resume 用）；可为 nil
	ttl          time.Duration
}

func NewManager(store *JSONLStore, ttl time.Duration) *Manager {
	if ttl == 0 {
		ttl = 7 * 24 * time.Hour
	}
	return &Manager{
		sessions: make(map[string]*Session),
		store:    store,
		ttl:      ttl,
	}
}

// SetArchiveStore 注入归档存储（/resume 功能依赖）
func (m *Manager) SetArchiveStore(a *ArchiveStore) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.archiveStore = a
}

// ArchiveStore 暴露归档存储供命令层使用
func (m *Manager) ArchiveStore() *ArchiveStore {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.archiveStore
}

// ArchiveAndClear 归档当前 session 的消息（非空时），然后清空。
// 若 archiveStore 为 nil 或 agentID 为空，退化为直接 Clear（毁灭式，兼容旧行为）。
// 用于 /new 命令：保存当前对话到归档目录，而非直接丢弃。
func (m *Manager) ArchiveAndClear(key, agentID, title string) error {
	m.mu.RLock()
	a := m.archiveStore
	m.mu.RUnlock()

	s, err := m.Get(key)
	if err != nil {
		return err
	}
	msgs := s.GetMessages()
	if len(msgs) > 0 && a != nil && agentID != "" {
		s.FixIncompleteToolCalls()
		if _, err := a.Save(key, agentID, title, s.GetMessages()); err != nil {
			return fmt.Errorf("archive session: %w", err)
		}
	}
	s.Clear()
	return nil
}

func (m *Manager) GetOrCreate(key string) *Session {
	m.mu.RLock()
	s, ok := m.sessions[key]
	m.mu.RUnlock()
	if ok {
		return s
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok = m.sessions[key]; ok {
		return s
	}

	s = NewSession(key, key, m.store)
	m.sessions[key] = s
	return s
}

// NewSession 创建新的空 Session (清空历史)
func (m *Manager) NewSession(key string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 创建新的空 Session
	s := NewSession(key, key, nil) // 不加载历史
	m.sessions[key] = s
	return s
}

func (m *Manager) Get(key string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[key]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", key)
	}
	return s, nil
}

func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// SessionInfo 用于 API 响应的 Session 信息
type SessionInfo struct {
	Key             string    `json:"key"`
	ID              string    `json:"id"`
	AgentID         string    `json:"agent_id,omitempty"`
	UserID          string    `json:"user_id,omitempty"`
	SessionKind     string    `json:"session_kind"`
	MemoryPolicy    string    `json:"memory_policy,omitempty"`
	MessageCount    int       `json:"message_count"`
	UpdatedAt       time.Time `json:"updated_at"`
	AgeSeconds      int64     `json:"age_seconds"`
	IsSchedule      bool      `json:"is_schedule"`
	IsMemoryIgnored bool      `json:"is_memory_ignored"`
}

func (m *Manager) List() []SessionInfo {
	m.mu.RLock()
	keys := make(map[string]struct{}, len(m.sessions))
	for key := range m.sessions {
		keys[key] = struct{}{}
	}
	store := m.store
	m.mu.RUnlock()

	if store != nil {
		storeKeys, err := store.ListKeys()
		if err == nil {
			for _, key := range storeKeys {
				if key == "" {
					continue
				}
				keys[key] = struct{}{}
			}
		}
	}

	list := make([]SessionInfo, 0, len(keys))
	now := time.Now()
	for key := range keys {
		sess := m.GetOrCreate(key)
		metadata := sess.MetadataCopy()
		sessionKind := strings.TrimSpace(metadata["session_kind"])
		if sessionKind == "" {
			sessionKind = "normal"
		}
		memoryPolicy := strings.TrimSpace(metadata["memory_policy"])
		agentID := strings.TrimSpace(metadata["agent_id"])
		if agentID == "" {
			agentID = extractAgentIDFromKey(sess.Key)
		}
		updatedAt := sess.UpdatedAt
		list = append(list, SessionInfo{
			Key:             sess.Key,
			ID:              sess.ID,
			AgentID:         agentID,
			UserID:          sess.GetUserID(),
			SessionKind:     sessionKind,
			MemoryPolicy:    memoryPolicy,
			MessageCount:    sess.MessageCount(),
			UpdatedAt:       updatedAt,
			AgeSeconds:      int64(now.Sub(updatedAt).Seconds()),
			IsSchedule:      sessionKind == "schedule" || isScheduleSessionKey(sess.Key),
			IsMemoryIgnored: memoryPolicy == "ignore",
		})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Key < list[j].Key
	})
	return list
}

func extractAgentIDFromKey(key string) string {
	parts := strings.Split(key, ":")
	if len(parts) >= 2 && parts[0] == "agent" {
		return parts[1]
	}
	return ""
}

func isScheduleSessionKey(key string) bool {
	parts := strings.Split(key, ":")
	for _, part := range parts {
		if part == "schedule" {
			return true
		}
	}
	return false
}

func (m *Manager) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[key]
	if !ok {
		return fmt.Errorf("session not found: %s", key)
	}

	if m.store != nil {
		if err := m.store.Delete(s.Key); err != nil {
			return fmt.Errorf("delete session store: %w", err)
		}
	}

	delete(m.sessions, key)
	return nil
}

func (m *Manager) Cleanup() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	var toDelete []string

	for key, s := range m.sessions {
		if now.Sub(s.UpdatedAt) > m.ttl {
			toDelete = append(toDelete, key)
		}
	}

	for _, key := range toDelete {
		s := m.sessions[key]
		if m.store != nil {
			if err := m.store.Delete(s.Key); err != nil {
				return 0, fmt.Errorf("delete session %s: %w", key, err)
			}
		}
		delete(m.sessions, key)
	}

	return len(toDelete), nil
}

func (m *Manager) TTL() time.Duration {
	return m.ttl
}
