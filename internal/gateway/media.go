package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/chawuciren/evoduck/internal/mediautil"
	"github.com/chawuciren/evoduck/pkg/logger"
	"github.com/chawuciren/evoduck/pkg/models"
)

const maxMediaUploadBytes = 20 << 20

type mediaUploadResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size"`
	URL      string `json:"url"`
}

func (g *Gateway) normalizeIncomingMedia(media []models.OutgoingMedia) ([]models.OutgoingMedia, error) {
	return mediautil.NormalizeOutgoingMedia(g.mediaStore, media)
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
	r.Body = http.MaxBytesReader(w, r.Body, maxMediaUploadBytes)
	if err := r.ParseMultipartForm(maxMediaUploadBytes); err != nil {
		http.Error(w, "Invalid multipart upload", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusBadRequest)
		return
	}
	record, err := g.mediaStore.Save(header.Filename, header.Header.Get("Content-Type"), data)
	if err != nil {
		logger.Error("Media upload failed", logger.Fields{"error": err.Error()})
		http.Error(w, "Failed to store media", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, mediaUploadResponse{
		ID:       record.ID,
		Name:     record.Name,
		MimeType: record.MimeType,
		Size:     record.Size,
		URL:      mediautil.MediaURL(record.ID),
	})
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
