package tools

import (
	"time"
)

type TimeTool struct{}

func NewTimeTool() *TimeTool {
	return &TimeTool{}
}

func (t *TimeTool) Name() string {
	return "time"
}

func (t *TimeTool) Description() string {
	return "Get the current time and date"
}

func (t *TimeTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"format": map[string]interface{}{
				"type":        "string",
				"description": "Time format (optional, defaults to RFC3339)",
			},
		},
	}
}

func (t *TimeTool) Execute(args map[string]interface{}) (string, error) {
	format, _ := args["format"].(string)
	if format == "" {
		format = time.RFC3339
	}
	return time.Now().Format(format), nil
}
