package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var sanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9_\-]`)

func sanitizeID(id string) string {
	return sanitizeRe.ReplaceAllString(id, "_")
}

type Manager struct {
	mu           sync.RWMutex
	workspace    string
	dailyDir     string
	longtermFile string
	dataDir      string
	agentID      string
}

func NewManager(workspace, dailyDir, longtermFile, dataDir, agentID string) (*Manager, error) {
	return &Manager{
		workspace:    workspace,
		dailyDir:     filepath.Join(workspace, dailyDir),
		longtermFile: filepath.Join(workspace, longtermFile),
		dataDir:      dataDir,
		agentID:      agentID,
	}, nil
}

func (m *Manager) AppendMedium(content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	path := filepath.Join(m.dailyDir, today+".md")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "\n## %s\n%s\n", time.Now().Format("15:04"), content)
	return err
}

func (m *Manager) LoadRecentDays(n int) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var parts []string
	for i := 0; i < n; i++ {
		day := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		path := filepath.Join(m.dailyDir, day+".md")
		data, err := os.ReadFile(path)
		if err == nil {
			parts = append(parts, fmt.Sprintf("## %s\n%s", day, string(data)))
		}
	}
	return strings.Join(parts, "\n\n")
}

func (m *Manager) LoadLongTerm() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := os.ReadFile(m.longtermFile)
	if err != nil {
		return ""
	}
	return string(data)
}

func (m *Manager) AppendLongTerm(content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	f, err := os.OpenFile(m.longtermFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "\n## %s\n%s\n", time.Now().Format("2006-01-02 15:04"), content)
	return err
}

// GetUserMediumDir returns the user-level medium memory directory: data/users/{agentID}_user_{userID}/memory
func (m *Manager) GetUserMediumDir(userID string) string {
	safeAgent := sanitizeID(m.agentID)
	safeUser := sanitizeID(userID)
	return filepath.Join(m.dataDir, "users", fmt.Sprintf("%s_user_%s", safeAgent, safeUser), "memory")
}

// AppendUserMedium appends content to the user-level medium memory file {today}.md
func (m *Manager) AppendUserMedium(userID, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	dir := m.GetUserMediumDir(userID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	today := time.Now().Format("2006-01-02")
	path := filepath.Join(dir, today+".md")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "\n## %s\n%s\n", time.Now().Format("15:04"), content)
	return err
}

// LoadUserRecentMedium loads the user's recent medium memory for the past N days
func (m *Manager) LoadUserRecentMedium(userID string, days int) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	dir := m.GetUserMediumDir(userID)
	var parts []string
	for i := 0; i < days; i++ {
		day := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		path := filepath.Join(dir, day+".md")
		data, err := os.ReadFile(path)
		if err == nil {
			parts = append(parts, fmt.Sprintf("## %s\n%s", day, string(data)))
		}
	}
	return strings.Join(parts, "\n\n")
}

// GetUserMediumTotalSize calculates the total character size of all .md files in the user's medium memory directory
func (m *Manager) GetUserMediumTotalSize(userID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	dir := m.GetUserMediumDir(userID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}

	total := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err == nil {
				total += len(data)
			}
		}
	}
	return total
}

// ListUserMediumFiles returns sorted user medium memory file paths.
func (m *Manager) ListUserMediumFiles(userID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	dir := m.GetUserMediumDir(userID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	return files
}

func (m *Manager) ListUserRecentMediumFiles(userID string, days int) []string {
	all := m.ListUserMediumFiles(userID)
	if days <= 0 || len(all) == 0 {
		return all
	}

	cutoff := time.Now().AddDate(0, 0, -(days - 1))
	filtered := make([]string, 0, len(all))
	for _, path := range all {
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		day, err := time.Parse("2006-01-02", name)
		if err != nil {
			continue
		}
		if day.Before(time.Date(cutoff.Year(), cutoff.Month(), cutoff.Day(), 0, 0, 0, 0, cutoff.Location())) {
			continue
		}
		filtered = append(filtered, path)
	}
	return filtered
}

func (m *Manager) HasUserMediumUpdatesSince(userID string, since time.Time, days int) bool {
	files := m.ListUserRecentMediumFiles(userID, days)
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime().After(since) {
			return true
		}
	}
	return false
}

// RewriteUserMediumFile overwrites a user medium memory file with normalized content.
func (m *Manager) RewriteUserMediumFile(path, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return os.WriteFile(path, []byte(content), 0644)
}

// RewriteUserLongterm overwrites the user longterm MEMORY.md file.
func (m *Manager) RewriteUserLongterm(userID, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := m.GetUserLongtermPath(userID)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// GetUserLongtermPath returns the user-level longterm memory file path: data/users/{agentID}_user_{userID}/MEMORY.md
func (m *Manager) GetUserLongtermPath(userID string) string {
	safeAgent := sanitizeID(m.agentID)
	safeUser := sanitizeID(userID)
	return filepath.Join(m.dataDir, "users", fmt.Sprintf("%s_user_%s", safeAgent, safeUser), "MEMORY.md")
}

// GetUserLongterm reads the user-level longterm memory
func (m *Manager) GetUserLongterm(userID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	path := m.GetUserLongtermPath(userID)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// AppendUserLongterm appends content to the user-level longterm memory
func (m *Manager) AppendUserLongterm(userID, content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	path := m.GetUserLongtermPath(userID)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "\n## %s\n%s\n", time.Now().Format("2006-01-02 15:04"), content)
	return err
}

// GetUserLongtermSize returns the size of the user-level MEMORY.md file in bytes
func (m *Manager) GetUserLongtermSize(userID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	path := m.GetUserLongtermPath(userID)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return len(data)
}

// ListPersistedUserIDs returns sanitized user IDs discovered on disk for the current agent.
func (m *Manager) ListPersistedUserIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	usersDir := filepath.Join(m.dataDir, "users")
	entries, err := os.ReadDir(usersDir)
	if err != nil {
		return nil
	}

	prefix := sanitizeID(m.agentID) + "_user_"
	userSet := make(map[string]struct{})
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		userID := strings.TrimPrefix(name, prefix)
		if userID == "" {
			continue
		}
		userSet[userID] = struct{}{}
	}

	users := make([]string, 0, len(userSet))
	for userID := range userSet {
		users = append(users, userID)
	}
	sort.Strings(users)
	return users
}

// GetAgentLongterm returns the agent-level longterm memory (shared across all users)
func (m *Manager) GetAgentLongterm() string {
	return m.LoadLongTerm()
}

// AppendAgentLongterm appends content to the agent-level longterm memory
func (m *Manager) AppendAgentLongterm(content string) error {
	return m.AppendLongTerm(content)
}

// RewriteAgentLongterm overwrites the agent-level longterm memory.
func (m *Manager) RewriteAgentLongterm(content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return os.WriteFile(m.longtermFile, []byte(content), 0644)
}

// GetAgentLongtermSize returns the size of the legacy longterm file in bytes.
// Shared reusable information should now live in the knowledge system.
func (m *Manager) GetAgentLongtermSize() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := os.ReadFile(m.longtermFile)
	if err != nil {
		return 0
	}
	return len(data)
}
