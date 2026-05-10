package tools

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestMediaStoreToolStoresBase64Payload(t *testing.T) {
	tool, err := NewMediaStoreTool(t.TempDir())
	if err != nil {
		t.Fatalf("new media store tool: %v", err)
	}
	result, err := tool.Execute(map[string]interface{}{
		"name":      "demo.txt",
		"mime_type": "text/plain",
		"data":      base64.StdEncoding.EncodeToString([]byte("hello media")),
		"compress":  true,
	})
	if err != nil {
		t.Fatalf("execute media store tool: %v", err)
	}
	var payload struct {
		ID           string `json:"id"`
		URL          string `json:"url"`
		Path         string `json:"path"`
		Name         string `json:"name"`
		MimeType     string `json:"mime_type"`
		OriginalSize int64  `json:"original_size"`
		FinalSize    int64  `json:"final_size"`
		Compressed   bool   `json:"compressed"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("decode media store tool result: %v", err)
	}
	if payload.ID == "" || payload.URL == "" || payload.Path == "" {
		t.Fatalf("unexpected media store payload: %#v", payload)
	}
	if payload.Name != "demo.txt" || payload.MimeType != "text/plain" {
		t.Fatalf("unexpected media store metadata: %#v", payload)
	}
	if payload.OriginalSize != int64(len("hello media")) || payload.FinalSize != int64(len("hello media")) {
		t.Fatalf("unexpected media store sizes: %#v", payload)
	}
	if payload.Compressed {
		t.Fatalf("expected media store tool to remain uncompressed in phase two")
	}
}
