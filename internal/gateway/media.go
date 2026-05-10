package gateway

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/chawuciren/evoduck/internal/mediautil"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
)

const maxMediaUploadBytes = 20 << 20

type mediaUploadRequest struct {
	Path     string `json:"path"`
	Data     string `json:"data"`
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	Compress *bool  `json:"compress,omitempty"`
	MaxBytes int    `json:"max_bytes,omitempty"`
}

type mediaUploadResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MimeType     string `json:"mime_type,omitempty"`
	Size         int64  `json:"size"`
	URL          string `json:"url"`
	Path         string `json:"path"`
	OriginalSize int64  `json:"original_size"`
	FinalSize    int64  `json:"final_size"`
	Compressed   bool   `json:"compressed"`
}

func (g *Gateway) normalizeIncomingMedia(media []models.OutgoingMedia) ([]models.OutgoingMedia, error) {
	return mediautil.NormalizeOutgoingMediaWithOptions(g.mediaStore, media, mediautil.NormalizeOptions{
		Compress: true,
		MaxBytes: uploadMaxBytes(0, g.config),
	})
}

func (g *Gateway) handleMediaUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if g.mediaStore == nil {
		http.Error(w, "Media storage unavailable", http.StatusServiceUnavailable)
		return
	}
	result, err := g.storeUploadedMedia(w, r)
	if err != nil {
		logger.Error("Media upload failed", logger.Fields{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, mediaUploadResponse{
		ID:           result.ID,
		Name:         result.Name,
		MimeType:     result.MimeType,
		Size:         result.FinalSize,
		URL:          result.URL,
		Path:         result.Path,
		OriginalSize: result.OriginalSize,
		FinalSize:    result.FinalSize,
		Compressed:   result.Compressed,
	})
}

func (g *Gateway) storeUploadedMedia(w http.ResponseWriter, r *http.Request) (*mediautil.StoreResult, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMediaUploadBytes)
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))), "application/json") {
		return g.storeUploadedMediaFromJSON(r)
	}
	return g.storeUploadedMediaFromMultipart(r)
}

func (g *Gateway) storeUploadedMediaFromJSON(r *http.Request) (*mediautil.StoreResult, error) {
	var req mediaUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, fmt.Errorf("invalid json upload")
	}
	return mediautil.StoreMedia(g.mediaStore, mediautil.StoreInput{
		Path:     req.Path,
		Data:     req.Data,
		Name:     req.Name,
		MimeType: req.MimeType,
		Compress: req.Compress == nil || *req.Compress,
		MaxBytes: uploadMaxBytes(req.MaxBytes, g.config),
	})
}

func (g *Gateway) storeUploadedMediaFromMultipart(r *http.Request) (*mediautil.StoreResult, error) {
	if err := r.ParseMultipartForm(maxMediaUploadBytes); err != nil {
		return nil, fmt.Errorf("invalid multipart upload")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, fmt.Errorf("missing file")
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read file")
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = header.Filename
	}
	mimeType := strings.TrimSpace(r.FormValue("mime_type"))
	if mimeType == "" {
		mimeType = header.Header.Get("Content-Type")
	}
	return mediautil.StoreMedia(g.mediaStore, mediautil.StoreInput{
		Data:     dataToBase64(data),
		Name:     name,
		MimeType: mimeType,
		Compress: boolFormValue(r, "compress", true),
		MaxBytes: uploadMaxBytes(intFormValue(r, "max_bytes"), g.config),
	})
}

func uploadMaxBytes(requested int, cfg *config.Config) int {
	if requested > 0 {
		return requested
	}
	if cfg != nil {
		return cfg.ImageAutoCompressLimit
	}
	return 0
}

func dataToBase64(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(data)
}

func boolFormValue(r *http.Request, key string, fallback bool) bool {
	raw := strings.TrimSpace(r.FormValue(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func intFormValue(r *http.Request, key string) int {
	raw := strings.TrimSpace(r.FormValue(key))
	if raw == "" {
		return 0
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return parsed
}

func (g *Gateway) handleMediaGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if g.mediaStore == nil {
		http.Error(w, "Media storage unavailable", http.StatusServiceUnavailable)
		return
	}
	mediaID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/media/"))
	if mediaID == "" || strings.Contains(mediaID, "/") {
		http.NotFound(w, r)
		return
	}
	record, err := g.mediaStore.Load(mediaID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(g.mediaStore.FilePath(record))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	if record.MimeType != "" {
		w.Header().Set("Content-Type", record.MimeType)
	}
	if record.Name != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", record.Name))
	}
	http.ServeContent(w, r, record.Name, record.CreatedAt, file)
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
