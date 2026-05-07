package mediautil

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/pkg/models"
)

type Store struct {
	rootDir string
}

type Record struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	MimeType   string    `json:"mime_type,omitempty"`
	Size       int64     `json:"size"`
	StoredName string    `json:"stored_name"`
	CreatedAt  time.Time `json:"created_at"`
}

func NewStore(dataDir string) (*Store, error) {
	rootDir := filepath.Join(dataDir, "media")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, err
	}
	return &Store{rootDir: rootDir}, nil
}

func (s *Store) Save(name, mimeType string, data []byte) (*Record, error) {
	if s == nil {
		return nil, fmt.Errorf("media store is not configured")
	}
	mediaID, err := NewID()
	if err != nil {
		return nil, err
	}
	name = SanitizeName(name)
	if name == "" {
		name = mediaID
	}
	ext := strings.ToLower(filepath.Ext(name))
	storedName := mediaID + ext
	record := &Record{
		ID:         mediaID,
		Name:       name,
		MimeType:   strings.TrimSpace(mimeType),
		Size:       int64(len(data)),
		StoredName: storedName,
		CreatedAt:  time.Now().UTC(),
	}
	if record.MimeType == "" {
		record.MimeType = DetectMimeType(ext, data)
	}
	if err := os.WriteFile(s.FilePath(record), data, 0o644); err != nil {
		return nil, err
	}
	meta, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.MetaPath(mediaID), meta, 0o644); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Store) Load(mediaID string) (*Record, error) {
	if s == nil {
		return nil, fmt.Errorf("media store is not configured")
	}
	meta, err := os.ReadFile(s.MetaPath(mediaID))
	if err != nil {
		return nil, err
	}
	var record Record
	if err := json.Unmarshal(meta, &record); err != nil {
		return nil, err
	}
	if record.ID == "" {
		record.ID = mediaID
	}
	return &record, nil
}

func (s *Store) FilePath(record *Record) string {
	return filepath.Join(s.rootDir, record.StoredName)
}

func (s *Store) MetaPath(mediaID string) string {
	return filepath.Join(s.rootDir, mediaID+".json")
}

func NewID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "med_" + hex.EncodeToString(buf), nil
}

func SanitizeName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.Trim(name, ". ")
	if name == "" || name == "." {
		return ""
	}
	return name
}

func DetectMimeType(ext string, data []byte) string {
	if ext != "" {
		if byExt := mime.TypeByExtension(ext); byExt != "" {
			return byExt
		}
	}
	return http.DetectContentType(data)
}

func MediaURL(mediaID string) string {
	return "/media/" + mediaID
}

func MediaIDFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Path != "" {
		raw = parsed.Path
	}
	if !strings.HasPrefix(raw, "/media/") {
		return ""
	}
	mediaID := strings.TrimSpace(strings.TrimPrefix(raw, "/media/"))
	if mediaID == "" || strings.Contains(mediaID, "/") || strings.Contains(mediaID, "\\") {
		return ""
	}
	return mediaID
}

func ResolveStoredMedia(store *Store, item models.OutgoingMedia) (models.OutgoingMedia, bool, error) {
	if store == nil {
		return item, false, nil
	}
	mediaID := MediaIDFromURL(item.URL)
	if mediaID == "" {
		return item, false, nil
	}
	record, err := store.Load(mediaID)
	if err != nil {
		return item, false, err
	}
	item.URL = MediaURL(record.ID)
	item.Name = SanitizeName(item.Name)
	if item.Name == "" {
		item.Name = record.Name
	}
	if strings.TrimSpace(item.MimeType) == "" {
		item.MimeType = record.MimeType
	}
	if item.FileSize == 0 {
		item.FileSize = record.Size
	}
	item.Path = store.FilePath(record)
	return item, true, nil
}

func NormalizeOutgoingMedia(store *Store, media []models.OutgoingMedia) ([]models.OutgoingMedia, error) {
	if len(media) == 0 {
		return nil, nil
	}
	normalized := make([]models.OutgoingMedia, 0, len(media))
	for _, item := range media {
		clean := item
		clean.Type = strings.TrimSpace(clean.Type)
		clean.Name = SanitizeName(clean.Name)
		clean.URL = strings.TrimSpace(clean.URL)
		clean.Path = strings.TrimSpace(clean.Path)
		clean.Data = strings.TrimSpace(clean.Data)
		if clean.URL != "" {
			resolved, ok, err := ResolveStoredMedia(store, clean)
			if err != nil {
				return nil, fmt.Errorf("resolve media %q: %w", clean.URL, err)
			}
			if ok {
				normalized = append(normalized, resolved)
				continue
			}
		}
		if (clean.Data == "" && clean.Path == "") || store == nil {
			normalized = append(normalized, clean)
			continue
		}

		var (
			data []byte
			err  error
		)
		if clean.Data != "" {
			data, err = base64.StdEncoding.DecodeString(clean.Data)
			if err != nil {
				return nil, fmt.Errorf("decode media %q: %w", clean.Name, err)
			}
		} else {
			data, err = os.ReadFile(clean.Path)
			if err != nil {
				return nil, fmt.Errorf("read media path %q: %w", clean.Path, err)
			}
			if clean.Name == "" {
				clean.Name = SanitizeName(filepath.Base(clean.Path))
			}
		}
		if clean.Name == "" {
			clean.Name = "upload"
		}
		stored, err := store.Save(clean.Name, clean.MimeType, data)
		if err != nil {
			return nil, err
		}
		clean.URL = MediaURL(stored.ID)
		clean.MimeType = stored.MimeType
		clean.FileSize = stored.Size
		clean.Path = store.FilePath(stored)
		clean.Data = ""
		normalized = append(normalized, clean)
	}
	return normalized, nil
}
