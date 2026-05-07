package gateway

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chawuciren/evoduck/internal/mediautil"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
)

func TestMediaUploadAndFetch(t *testing.T) {
	gw := New(&config.Config{DataDir: t.TempDir()}, "", nil, nil, nil, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/media/upload", gw.handleMediaUpload)
	mux.HandleFunc("/media/", gw.handleMediaGet)
	server := httptest.NewServer(mux)
	defer server.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "demo.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("hello media")); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/media/upload", &body)
	if err != nil {
		t.Fatalf("new upload request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected upload status: %d", resp.StatusCode)
	}
	var uploaded mediaUploadResponse
	if err := json.NewDecoder(resp.Body).Decode(&uploaded); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if uploaded.ID == "" || uploaded.URL == "" {
		t.Fatalf("unexpected upload response: %#v", uploaded)
	}
	if !strings.HasPrefix(uploaded.URL, "/media/") {
		t.Fatalf("unexpected media url: %q", uploaded.URL)
	}

	fetchResp, err := http.Get(server.URL + uploaded.URL)
	if err != nil {
		t.Fatalf("fetch media failed: %v", err)
	}
	defer fetchResp.Body.Close()
	if fetchResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected fetch status: %d", fetchResp.StatusCode)
	}
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(fetchResp.Body); err != nil {
		t.Fatalf("read fetched media: %v", err)
	}
	if got := buf.String(); got != "hello media" {
		t.Fatalf("unexpected fetched content: %q", got)
	}
}

func TestNormalizeIncomingMediaStoresBase64Payload(t *testing.T) {
	gw := New(&config.Config{DataDir: t.TempDir()}, "", nil, nil, nil, nil)
	normalized, err := gw.normalizeIncomingMedia([]models.OutgoingMedia{{
		Type: "image",
		Name: "demo.txt",
		Data: base64.StdEncoding.EncodeToString([]byte("hello media")),
	}})
	if err != nil {
		t.Fatalf("normalize incoming media: %v", err)
	}
	if len(normalized) != 1 {
		t.Fatalf("expected one media item, got %d", len(normalized))
	}
	if normalized[0].Data != "" {
		t.Fatalf("expected data to be cleared after storage")
	}
	if normalized[0].URL == "" || !strings.HasPrefix(normalized[0].URL, "/media/") {
		t.Fatalf("expected stored media url, got %#v", normalized[0].URL)
	}
	req := httptest.NewRequest(http.MethodGet, normalized[0].URL, nil)
	rec := httptest.NewRecorder()
	gw.handleMediaGet(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected media get status: %d", rec.Code)
	}
	if got := rec.Body.String(); got != "hello media" {
		t.Fatalf("unexpected stored media content: %q", got)
	}
}

func TestNormalizeIncomingMediaResolvesStoredMediaURL(t *testing.T) {
	gw := New(&config.Config{DataDir: t.TempDir()}, "", nil, nil, nil, nil)
	stored, err := gw.mediaStore.Save("upload-test.png", "image/png", []byte("png-bytes"))
	if err != nil {
		t.Fatalf("save media: %v", err)
	}

	normalized, err := gw.normalizeIncomingMedia([]models.OutgoingMedia{{
		Type: "image",
		Name: "upload-test.png",
		URL:  mediautil.MediaURL(stored.ID),
	}})
	if err != nil {
		t.Fatalf("normalize incoming media by url: %v", err)
	}
	if len(normalized) != 1 {
		t.Fatalf("expected one media item, got %d", len(normalized))
	}
	if normalized[0].Path == "" {
		t.Fatal("expected stored media path to be populated")
	}
	if normalized[0].MimeType != "image/png" {
		t.Fatalf("expected mime type to be restored, got %q", normalized[0].MimeType)
	}
	if normalized[0].FileSize != int64(len("png-bytes")) {
		t.Fatalf("expected file size to be restored, got %d", normalized[0].FileSize)
	}
	if normalized[0].Data != "" {
		t.Fatalf("expected data to remain empty, got %q", normalized[0].Data)
	}
}
