package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type UserMemoryState struct {
	LastLongtermExtractAt int64 `json:"last_longterm_extract_at"`
	LastMediumCleanupAt   int64 `json:"last_medium_cleanup_at"`
	LastUserLongtermCleanupAt int64 `json:"last_user_longterm_cleanup_at"`
}

type AgentMemoryState struct {
	LastAgentLongtermCleanupAt int64 `json:"last_agent_longterm_cleanup_at"`
}

func (m *Manager) getUserStatePath(userID string) string {
	base := filepath.Dir(m.GetUserLongtermPath(userID))
	return filepath.Join(base, "memory_state.json")
}

func (m *Manager) GetUserState(userID string) (*UserMemoryState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	path := m.getUserStatePath(userID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &UserMemoryState{}, nil
		}
		return nil, err
	}
	var state UserMemoryState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (m *Manager) SaveUserState(userID string, state *UserMemoryState) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := m.getUserStatePath(userID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (m *Manager) GetAgentState() (*AgentMemoryState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	path := filepath.Join(m.workspace, ".memory_agent_state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &AgentMemoryState{}, nil
		}
		return nil, err
	}
	var state AgentMemoryState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (m *Manager) SaveAgentState(state *AgentMemoryState) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := filepath.Join(m.workspace, ".memory_agent_state.json")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func unixNow() int64 {
	return time.Now().Unix()
}
