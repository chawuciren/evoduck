package tools

import (
	"fmt"

	"github.com/chawuciren/evoduck/internal/subagent"
)

type SubagentGateway interface {
	CreateInternalSubagent(req subagent.StartInternalRequest) (*subagent.Record, error)
	CreateExternalSubagent(req subagent.StartExternalRequest) (*subagent.Record, error)
	ListSubagents(agentID, userID string) []subagent.Record
	GetSubagent(agentID, userID, id string) (*subagent.Record, error)
	CancelSubagent(agentID, userID, id string) (*subagent.Record, error)
}

type SubagentGatewayProvider func() SubagentGateway

func idParamSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]interface{}{
				"type":        "string",
				"description": "Subagent task ID.",
			},
		},
		"required": []string{"id"},
	}
}

func resolveSubagentGateway(provider SubagentGatewayProvider) (SubagentGateway, error) {
	if provider == nil {
		return nil, fmt.Errorf("subagent gateway unavailable")
	}
	gw := provider()
	if gw == nil {
		return nil, fmt.Errorf("subagent gateway unavailable")
	}
	return gw, nil
}
