package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chawuciren/evoduck/internal/mediautil"
)

type ImageCompressTool struct{}

func NewImageCompressTool() *ImageCompressTool {
	return &ImageCompressTool{}
}

func (t *ImageCompressTool) Name() string {
	return "image_compress"
}

func (t *ImageCompressTool) Description() string {
	return `Compress an image file on disk.

Parameters:
- input_path: Source image path
- output_path: Destination image path
- name: Optional file name override
- mime_type: Optional MIME type override
- max_bytes: Target size threshold in bytes
- overwrite: Write back to input_path when output_path is omitted`
}

func (t *ImageCompressTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"input_path": map[string]interface{}{
				"type":        "string",
				"description": "Source image path",
			},
			"output_path": map[string]interface{}{
				"type":        "string",
				"description": "Destination image path",
			},
			"name": map[string]interface{}{
				"type":        "string",
				"description": "Optional file name override",
			},
			"mime_type": map[string]interface{}{
				"type":        "string",
				"description": "Optional MIME type override",
			},
			"max_bytes": map[string]interface{}{
				"type":        "integer",
				"description": "Target size threshold in bytes",
			},
			"overwrite": map[string]interface{}{
				"type":        "boolean",
				"description": "Overwrite the source file when output_path is omitted",
			},
		},
		"required": []string{"input_path"},
	}
}

func (t *ImageCompressTool) Execute(args map[string]interface{}) (string, error) {
	inputPath := strings.TrimSpace(stringArg(args, "input_path"))
	if inputPath == "" {
		return "", fmt.Errorf("input_path is required")
	}
	result, err := mediautil.CompressImageFile(
		inputPath,
		strings.TrimSpace(stringArg(args, "output_path")),
		strings.TrimSpace(stringArg(args, "name")),
		strings.TrimSpace(stringArg(args, "mime_type")),
		intArg(args, "max_bytes"),
		boolArg(args, "overwrite"),
	)
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}
