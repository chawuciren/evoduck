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

type Store struct {
	mu   sync.RWMutex
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) LoadAll() ([]ScheduleRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	file, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []ScheduleRecord{}, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	schedules := make([]ScheduleRecord, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var schedule ScheduleRecord
		if err := json.Unmarshal([]byte(line), &schedule); err != nil {
			return nil, err
		}
		if strings.TrimSpace(schedule.ID) == "" || strings.TrimSpace(schedule.Schedule) == "" {
			continue
		}
		schedules = append(schedules, schedule)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	sort.Slice(schedules, func(i, j int) bool { return schedules[i].ID < schedules[j].ID })
	return schedules, nil
}

func (s *Store) SaveAll(schedules []ScheduleRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	for _, schedule := range schedules {
		if err := encoder.Encode(schedule); err != nil {
			return err
		}
	}
	return os.WriteFile(s.path, buf.Bytes(), 0644)
}
