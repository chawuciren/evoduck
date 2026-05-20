package subagent

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type Manager struct {
	mu      sync.RWMutex
	store   *Store
	records map[string]Record
}

func NewManager(store *Store) *Manager {
	return &Manager{store: store, records: make(map[string]Record)}
}

func (m *Manager) Load() error {
	if m == nil || m.store == nil {
		return nil
	}
	records, err := m.store.LoadAll()
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, record := range records {
		if strings.TrimSpace(record.ID) == "" {
			continue
		}
		m.records[record.ID] = record
	}
	return nil
}

func (m *Manager) List(agentID, userID string) []Record {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]Record, 0, len(m.records))
	for _, record := range m.records {
		if agentID != "" && record.CallerAgentID != agentID && record.TargetAgentID != agentID {
			continue
		}
		if userID != "" && record.UserID != userID {
			continue
		}
		result = append(result, record)
	}
	return result
}

func (m *Manager) Get(id string) (Record, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, ok := m.records[strings.TrimSpace(id)]
	return record, ok
}

func (m *Manager) Create(record Record) (Record, error) {
	if strings.TrimSpace(record.ID) == "" {
		record.ID = fmt.Sprintf("subagent-%d", time.Now().UnixNano())
	}
	now := time.Now().Unix()
	if record.CreatedAt == 0 {
		record.CreatedAt = now
	}
	if record.UpdatedAt == 0 {
		record.UpdatedAt = now
	}
	if record.Status == "" {
		record.Status = StatusStarting
	}
	m.mu.Lock()
	if _, exists := m.records[record.ID]; exists {
		m.mu.Unlock()
		return Record{}, fmt.Errorf("subagent already exists: %s", record.ID)
	}
	m.records[record.ID] = record
	m.mu.Unlock()
	return record, m.save()
}

func (m *Manager) UpdateHeartbeat(id string, ts int64) {
	m.mu.Lock()
	record, ok := m.records[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	record.LastHeartbeatAt = ts
	m.records[id] = record
	m.mu.Unlock()
}

func (m *Manager) Update(record Record) error {
	if strings.TrimSpace(record.ID) == "" {
		return fmt.Errorf("subagent id is required")
	}
	record.UpdatedAt = time.Now().Unix()
	m.mu.Lock()
	m.records[record.ID] = record
	m.mu.Unlock()
	return m.save()
}

func (m *Manager) save() error {
	if m == nil || m.store == nil {
		return nil
	}
	m.mu.RLock()
	records := make([]Record, 0, len(m.records))
	for _, record := range m.records {
		records = append(records, record)
	}
	m.mu.RUnlock()
	return m.store.SaveAll(records)
}
