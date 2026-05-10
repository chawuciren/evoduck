package tools

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func TestImageCompressToolWritesOutput(t *testing.T) {
	inputPath := filepath.Join(t.TempDir(), "input.jpg")
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: uint8((x + y) % 255), A: 255})
		}
	}
	buf := new(bytes.Buffer)
	if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatalf("encode input jpeg: %v", err)
	}
	if err := os.WriteFile(inputPath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write input jpeg: %v", err)
	}

	tool := NewImageCompressTool()
	result, err := tool.Execute(map[string]interface{}{
		"input_path": inputPath,
		"max_bytes": len(buf.Bytes()) - 100,
	})
	if err != nil {
		t.Fatalf("execute image compress tool: %v", err)
	}
	var payload struct {
		Path         string `json:"path"`
		MimeType     string `json:"mime_type"`
		OriginalSize int64  `json:"original_size"`
		FinalSize    int64  `json:"final_size"`
		Compressed   bool   `json:"compressed"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		t.Fatalf("decode image compress payload: %v", err)
	}
	if payload.Path == "" || payload.Path == inputPath {
		t.Fatalf("expected new output path, got %#v", payload)
	}
	if payload.MimeType != "image/jpeg" {
		t.Fatalf("expected jpeg mime type, got %#v", payload)
	}
	if payload.OriginalSize == 0 || payload.FinalSize == 0 {
		t.Fatalf("expected size metadata, got %#v", payload)
	}
}
