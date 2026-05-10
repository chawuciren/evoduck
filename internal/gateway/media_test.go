package gateway

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
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
	if uploaded.ID == "" || uploaded.URL == "" || uploaded.Path == "" {
		t.Fatalf("unexpected upload response: %#v", uploaded)
	}
	if !strings.HasPrefix(uploaded.URL, "/media/") {
		t.Fatalf("unexpected media url: %q", uploaded.URL)
	}
	if uploaded.OriginalSize != int64(len("hello media")) {
		t.Fatalf("expected original size to be populated, got %#v", uploaded)
	}
	if uploaded.FinalSize != int64(len("hello media")) || uploaded.Size != uploaded.FinalSize {
		t.Fatalf("expected final size fields to match, got %#v", uploaded)
	}
	if uploaded.Compressed {
		t.Fatalf("expected multipart upload to remain uncompressed in phase two")
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

func TestMediaUploadJSONBase64(t *testing.T) {
	gw := New(&config.Config{DataDir: t.TempDir(), ImageAutoCompressLimit: 32 * 1024}, "", nil, nil, nil, nil)
	body := strings.NewReader(`{"name":"demo.txt","data":"` + base64.StdEncoding.EncodeToString([]byte("hello media")) + `","mime_type":"text/plain","compress":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/media/upload", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	gw.handleMediaUpload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected media upload status: %d body=%s", rec.Code, rec.Body.String())
	}
	var uploaded mediaUploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &uploaded); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if uploaded.Name != "demo.txt" || uploaded.Path == "" {
		t.Fatalf("unexpected upload response: %#v", uploaded)
	}
	if uploaded.OriginalSize != int64(len("hello media")) || uploaded.FinalSize != int64(len("hello media")) {
		t.Fatalf("unexpected upload sizes: %#v", uploaded)
	}
	if uploaded.Compressed {
		t.Fatalf("expected json upload to remain uncompressed in phase two")
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

func TestNormalizeIncomingMediaAutoCompressesImagePayload(t *testing.T) {
	gw := New(&config.Config{DataDir: t.TempDir(), ImageAutoCompressLimit: 8 * 1024}, "", nil, nil, nil, nil)
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8((x + y) % 255), A: 255})
		}
	}
	buf := new(bytes.Buffer)
	if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	if buf.Len() <= gw.config.ImageAutoCompressLimit {
		t.Fatalf("expected fixture to exceed compression limit, got %d", buf.Len())
	}
	normalized, err := gw.normalizeIncomingMedia([]models.OutgoingMedia{{
		Type:     "image",
		Name:     "photo.jpg",
		MimeType: "image/jpeg",
		Data:     base64.StdEncoding.EncodeToString(buf.Bytes()),
	}})
	if err != nil {
		t.Fatalf("normalize incoming compressed media: %v", err)
	}
	if len(normalized) != 1 {
		t.Fatalf("expected one media item, got %d", len(normalized))
	}
	if normalized[0].Path == "" || normalized[0].URL == "" {
		t.Fatalf("expected stored media fields, got %#v", normalized[0])
	}
	if normalized[0].FileSize >= int64(buf.Len()) {
		t.Fatalf("expected compressed file size to shrink, got %d from %d", normalized[0].FileSize, buf.Len())
	}
}
