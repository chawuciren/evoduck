package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chawuciren/evoduck/pkg/models"
)

// ArchiveMeta 归档元数据（/resume 列表展示用，避免每次打开 jsonl）
type ArchiveMeta struct {
	ID           string    `json:"id"`            // 归档 ID（文件名前缀，unix nano）
	CreatedAt    time.Time `json:"created_at"`    // 归档时间
	MessageCount int       `json:"message_count"` // 消息数
	Preview      string    `json:"preview"`       // 首条用户消息预览（前 120 字符）
	Title        string    `json:"title"`         // LLM 生成的标题（≤20字），为空时回退到 Preview
	OriginalKey  string    `json:"original_key"`  // 原始 session key（防 key 转义冲突）
	AgentID      string    `json:"agent_id"`      // 归档时所属 agent
}

// ArchiveEntry 归档条目（meta + 可解析的文件路径）
type ArchiveEntry struct {
	Meta ArchiveMeta
	Path string // .jsonl 文件绝对路径
}

// ArchiveStore 会话归档存储（与活跃 sessions 完全隔离）
type ArchiveStore struct {
	mu   sync.Mutex
	base string // data/sessions.archive/
}

// NewArchiveStore 创建归档存储，base 为归档根目录
func NewArchiveStore(base string) (*ArchiveStore, error) {
	if err := os.MkdirAll(base, 0755); err != nil {
		return nil, err
	}
	return &ArchiveStore{base: base}, nil
}

// safeKeyForArchive 与 JSONLStore.filePath 转义逻辑保持一致（防止 key 漂移）
func safeKeyForArchive(key string) string {
	s := strings.ReplaceAll(key, ":", "_")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	return s
}

// keyDir 返回某个 session key 的归档子目录
func (a *ArchiveStore) keyDir(key string) string {
	return filepath.Join(a.base, safeKeyForArchive(key))
}

// Save 归档一组消息，返回归档 ID。若 msgs 为空则不归档，返回空 ID。
// title 为 LLM 生成的标题（可为空，空时回退到 Preview）。
func (a *ArchiveStore) Save(key, agentID, title string, msgs []models.Message) (string, error) {
	if len(msgs) == 0 {
		return "", nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	dir := a.keyDir(key)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create archive dir: %w", err)
	}

	// unix nano 天然有序、无同秒冲突
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	jsonlPath := filepath.Join(dir, id+".jsonl")
	metaPath := filepath.Join(dir, id+".meta.json")

	// 写 jsonl（追加式构建，单文件全量写）
	f, err := os.Create(jsonlPath)
	if err != nil {
		return "", fmt.Errorf("create archive jsonl: %w", err)
	}
	for _, m := range msgs {
		data, err := json.Marshal(m)
		if err != nil {
			f.Close()
			return "", fmt.Errorf("marshal archive msg: %w", err)
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			f.Close()
			return "", fmt.Errorf("write archive msg: %w", err)
		}
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", fmt.Errorf("sync archive jsonl: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close archive jsonl: %w", err)
	}

	// 写 meta
	t := strings.TrimSpace(title)
	meta := ArchiveMeta{
		ID:           id,
		CreatedAt:    time.Now(),
		MessageCount: len(msgs),
		Preview:      t,
		Title:        t,
		OriginalKey:  key,
		AgentID:      agentID,
	}
	metaData, err := json.Marshal(meta)
	if err != nil {
		return id, fmt.Errorf("marshal archive meta: %w", err)
	}
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		return id, fmt.Errorf("write archive meta: %w", err)
	}
	return id, nil
}

// List 列出某个 session key 的所有归档，按时间倒序（最新在前）
func (a *ArchiveStore) List(key string) ([]ArchiveEntry, error) {
	dir := a.keyDir(key)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read archive dir: %w", err)
	}
	var list []ArchiveEntry
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".meta.json") {
			continue
		}
		metaPath := filepath.Join(dir, name)
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var meta ArchiveMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue
		}
		jsonlPath := filepath.Join(dir, strings.TrimSuffix(name, ".meta.json")+".jsonl")
		list = append(list, ArchiveEntry{Meta: meta, Path: jsonlPath})
	}
	// 时间倒序：ID 是 unix nano 字符串，字典序即时间序
	sort.Slice(list, func(i, j int) bool {
		return list[i].Meta.ID > list[j].Meta.ID
	})
	return list, nil
}

// Load 加载指定归档的全部消息，并过滤掉 Role=="system" 的历史消息
// （system prompt 始终由 PromptBuilder 实时生成，从归档恢复会导致旧 prompt 污染）
func (a *ArchiveStore) Load(key, archiveID string) ([]models.Message, error) {
	jsonlPath := filepath.Join(a.keyDir(key), archiveID+".jsonl")
	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		return nil, fmt.Errorf("read archive jsonl: %w", err)
	}
	var msgs []models.Message
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m models.Message
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		// 过滤 system（防旧 prompt 污染）+ legacy synthetic tool msg
		if strings.EqualFold(strings.TrimSpace(m.Role), "system") {
			continue
		}
		if isLegacySyntheticToolMessage(m) {
			continue
		}
		// 清除推理过程元数据：跨 provider（如归档时 Claude，恢复后切 DeepSeek）
		// 这些字段的格式可能不兼容，导致 API 解析报错或幻觉。
		// 推理过程不是上下文事实，清除后 LLM 仍能从 Content 获取所有事实信息。
		m.ThinkingContent = ""
		m.ReasoningMetadata = nil
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// Cleanup 清理归档：每个 key 保留最近 maxPerKey 条，删除超过 maxAgeHours 的
func (a *ArchiveStore) Cleanup(maxPerKey int, maxAgeHours int) (int, error) {
	if maxPerKey <= 0 && maxAgeHours <= 0 {
		return 0, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	rootEntries, err := os.ReadDir(a.base)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	deleted := 0
	now := time.Now()
	maxAge := time.Duration(maxAgeHours) * time.Hour
	for _, keyDir := range rootEntries {
		if !keyDir.IsDir() {
			continue
		}
		dirPath := filepath.Join(a.base, keyDir.Name())
		files, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		// 收集 meta + 时间
		type fileInfo struct {
			id   string
			ts   time.Time
			meta string
			jl   string
		}
		var infos []fileInfo
		for _, f := range files {
			name := f.Name()
			if !strings.HasSuffix(name, ".meta.json") {
				continue
			}
			id := strings.TrimSuffix(name, ".meta.json")
			metaPath := filepath.Join(dirPath, name)
			data, err := os.ReadFile(metaPath)
			if err != nil {
				continue
			}
			var meta ArchiveMeta
			if err := json.Unmarshal(data, &meta); err == nil {
				infos = append(infos, fileInfo{id: id, ts: meta.CreatedAt, meta: metaPath, jl: filepath.Join(dirPath, id+".jsonl")})
			}
		}
		// 时间倒序（最新在前）
		sort.Slice(infos, func(i, j int) bool { return infos[i].id > infos[j].id })

		for i, info := range infos {
			needDelete := false
			if maxPerKey > 0 && i >= maxPerKey {
				needDelete = true
			}
			if maxAgeHours > 0 && now.Sub(info.ts) > maxAge {
				needDelete = true
			}
			if needDelete {
				os.Remove(info.meta)
				os.Remove(info.jl)
				deleted++
			}
		}
	}
	return deleted, nil
}
