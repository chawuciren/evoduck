package scheduler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type RunStore struct {
	mu  sync.RWMutex
	dir string
}

func NewRunStore(dir string) *RunStore {
	return &RunStore{dir: dir}
}

func (s *RunStore) filePath(scheduleID string) string {
	safeID := strings.ReplaceAll(scheduleID, ":", "_")
	safeID = strings.ReplaceAll(safeID, "/", "_")
	safeID = strings.ReplaceAll(safeID, "\\", "_")
	return filepath.Join(s.dir, safeID+".jsonl")
}

func (s *RunStore) Append(record ScheduleRunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.filePath(record.ScheduleID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

func (s *RunStore) List(scheduleID string, limit int) ([]ScheduleRunRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path := s.filePath(scheduleID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []ScheduleRunRecord{}, nil
		}
		return nil, err
	}
	defer f.Close()
	items := make([]ScheduleRunRecord, 0)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item ScheduleRunRecord
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].StartedAt > items[j].StartedAt })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *RunStore) Replace(scheduleID string, items []ScheduleRunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			return err
		}
	}
	return os.WriteFile(s.filePath(scheduleID), buf.Bytes(), 0644)
}
