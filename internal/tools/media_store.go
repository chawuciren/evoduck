package tools

import (
	"encoding/json"
	"fmt"

	"github.com/chawuciren/evoduck/internal/mediautil"
)

type MediaStoreTool struct {
	store *mediautil.Store
}

func NewMediaStoreTool(dataDir string) (*MediaStoreTool, error) {
	store, err := mediautil.NewStore(dataDir)
	if err != nil {
		return nil, err
	}
	return &MediaStoreTool{store: store}, nil
}

func (t *MediaStoreTool) Name() string {
	return "media_store"
}

func (t *MediaStoreTool) Description() string {
	return `Store media from a local path or base64 payload in the shared media store.

Parameters:
- path: Local file path
- data: Base64-encoded file content
- name: File name override
- mime_type: MIME type override
- compress: Whether compression is allowed
- max_bytes: Compression threshold in bytes`
}

func (t *MediaStoreTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Local file path to store",
			},
			"data": map[string]interface{}{
				"type":        "string",
				"description": "Base64-encoded file content",
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Optional file name override",
			},
			"mime_type": map[string]interface{}{
				"type":        "string",
				"description": "Optional MIME type override",
			},
			"compress": map[string]interface{}{
				"type":        "boolean",
				"description": "Whether compression is allowed",
			},
			"max_bytes": map[string]interface{}{
				"type":        "integer",
				"description": "Compression threshold in bytes",
			},
		},
	}
}

func (t *MediaStoreTool) Execute(args map[string]interface{}) (string, error) {
	if t == nil || t.store == nil {
		return "", fmt.Errorf("media store is not configured")
	}
	result, err := mediautil.StoreMedia(t.store, mediautil.StoreInput{
		Path:     stringArg(args, "path"),
		Data:     stringArg(args, "data"),
		Name:     stringArg(args, "name"),
		MimeType: stringArg(args, "mime_type"),
		Compress: boolArgDefault(args, "compress", true),
		MaxBytes: intArg(args, "max_bytes"),
	})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}
