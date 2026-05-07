package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/chawuciren/evoduck/pkg/models"
)

type JSONLStore struct {
	mu   sync.Mutex
	base string
}

func NewJSONLStore(base string) (*JSONLStore, error) {
	if err := os.MkdirAll(base, 0755); err != nil {
		return nil, err
	}
	return &JSONLStore{base: base}, nil
}

func (s *JSONLStore) filePath(key string) string {
	safeKey := strings.ReplaceAll(key, ":", "_")
	safeKey = strings.ReplaceAll(safeKey, "/", "_")
	safeKey = strings.ReplaceAll(safeKey, "\\", "_")
	return filepath.Join(s.base, safeKey+".jsonl")
}

func (s *JSONLStore) metadataPath(key string) string {
	safeKey := strings.ReplaceAll(key, ":", "_")
	safeKey = strings.ReplaceAll(safeKey, "/", "_")
	safeKey = strings.ReplaceAll(safeKey, "\\", "_")
	return filepath.Join(s.base, safeKey+".meta.json")
}

func (s *JSONLStore) Load(key string) ([]models.Message, error) {
	path := s.filePath(key)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File not found - this is normal for new sessions
			return nil, nil
		}
		return nil, fmt.Errorf("open JSONL file: %w", err)
	}
	defer f.Close()

	var msgs []models.Message
	scanner := bufio.NewScanner(f)
	// Increase buffer for large messages
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var m models.Message
		if err := json.Unmarshal(line, &m); err != nil {
			// Log parse errors but continue
			continue
		}
		if isLegacySyntheticToolMessage(m) {
			continue
		}
		hydrateStoredMediaPaths(s.base, &m)
		msgs = append(msgs, m)
	}
	if err := scanner.Err(); err != nil {
		return msgs, fmt.Errorf("scan JSONL: %w", err)
	}
	return msgs, nil
}

func hydrateStoredMediaPaths(sessionBase string, msg *models.Message) {
	if msg == nil || len(msg.Media) == 0 {
		return
	}
	mediaRoot := filepath.Join(filepath.Dir(sessionBase), "media")
	for i := range msg.Media {
		item := &msg.Media[i]
		if strings.TrimSpace(item.Path) != "" {
			continue
		}
		mediaID := storedMediaIDFromURL(item.URL)
		if mediaID == "" {
			continue
		}
		meta, err := loadStoredMediaRecord(mediaRoot, mediaID)
		if err != nil {
			continue
		}
		item.Path = filepath.Join(mediaRoot, meta.StoredName)
		if strings.TrimSpace(item.Name) == "" {
			item.Name = meta.Name
		}
		if strings.TrimSpace(item.MimeType) == "" {
			item.MimeType = meta.MimeType
		}
		if item.FileSize == 0 {
			item.FileSize = meta.Size
		}
	}
}

type storedMediaRecord struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	MimeType   string `json:"mime_type,omitempty"`
	Size       int64  `json:"size"`
	StoredName string `json:"stored_name"`
}

func loadStoredMediaRecord(mediaRoot, mediaID string) (*storedMediaRecord, error) {
	metaPath := filepath.Join(mediaRoot, mediaID+".json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}
	var record storedMediaRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	if strings.TrimSpace(record.StoredName) == "" {
		return nil, fmt.Errorf("stored media %q is missing stored_name", mediaID)
	}
	return &record, nil
}

func storedMediaIDFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "/media/") {
		return ""
	}
	mediaID := strings.TrimSpace(strings.TrimPrefix(raw, "/media/"))
	if mediaID == "" || strings.Contains(mediaID, "/") || strings.Contains(mediaID, "\\") {
		return ""
	}
	return mediaID
}

func isLegacySyntheticToolMessage(msg models.Message) bool {
	return strings.TrimSpace(strings.ToLower(msg.Role)) == "tool" && strings.HasPrefix(msg.ToolCallID, "runtime_task_plan_reminder_")
}

func (s *JSONLStore) Append(key string, msg models.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.filePath(key), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, _ := json.Marshal(msg)
	_, err = f.Write(append(data, '\n'))
	if err != nil {
		return err
	}
	// Force sync to disk - critical for Windows where buffered writes can be lost
	return f.Sync()
}

func (s *JSONLStore) Replace(key string, msgs []models.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.filePath(key)
	tmpPath := path + ".tmp"

	// Write to temp file first
	f, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	w := bufio.NewWriter(f)
	for _, m := range msgs {
		data, _ := json.Marshal(m)
		if _, err := w.Write(append(data, '\n')); err != nil {
			f.Close()
			os.Remove(tmpPath)
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Atomic rename
	return os.Rename(tmpPath, path)
}

func (s *JSONLStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.filePath(key)
	if _, err := os.Stat(path); err == nil {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	metaPath := s.metadataPath(key)
	if _, err := os.Stat(metaPath); err == nil {
		if err := os.Remove(metaPath); err != nil {
			return err
		}
	}
	return nil
}

func (s *JSONLStore) LoadMetadata(key string) (map[string]string, error) {
	path := s.metadataPath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read metadata file: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var metadata map[string]string
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("unmarshal metadata: %w", err)
	}
	return metadata, nil
}

func (s *JSONLStore) SaveMetadata(key string, metadata map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.metadataPath(key)
	if len(metadata) == 0 {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return nil
		}
		return os.Remove(path)
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write metadata temp file: %w", err)
	}
	return os.Rename(tmpPath, path)
}

func (s *JSONLStore) ListKeys() ([]string, error) {
	entries, err := os.ReadDir(s.base)
	if err != nil {
		return nil, fmt.Errorf("read session store dir: %w", err)
	}
	keys := make(map[string]struct{})
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		switch {
		case strings.HasSuffix(name, ".jsonl"):
			name = strings.TrimSuffix(name, ".jsonl")
		case strings.HasSuffix(name, ".meta.json"):
			name = strings.TrimSuffix(name, ".meta.json")
		default:
			continue
		}
		if name == "" {
			continue
		}
		key := strings.ReplaceAll(name, "_", ":")
		keys[key] = struct{}{}
	}
	list := make([]string, 0, len(keys))
	for key := range keys {
		list = append(list, key)
	}
	sort.Strings(list)
	return list, nil
}
