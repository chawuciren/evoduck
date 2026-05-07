package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chawuciren/evoduck/internal/session"
	"github.com/chawuciren/evoduck/pkg/config"
	"github.com/chawuciren/evoduck/pkg/models"
)

type stubSessionGateway struct {
	sessions map[string]*session.Session
	lastSent struct {
		sessionKey string
		content    string
		media      []models.OutgoingMedia
	}
	lastRun struct {
		agentID    string
		sessionKey string
		input      string
	}
}

func (g *stubSessionGateway) List() []session.SessionInfo {
	items := make([]session.SessionInfo, 0, len(g.sessions))
	for _, sess := range g.sessions {
		items = append(items, session.SessionInfo{
			Key:             sess.Key,
			AgentID:         "agent-a",
			UserID:          sess.GetUserID(),
			SessionKind:     sess.GetMetadataValue("session_kind"),
			MemoryPolicy:    sess.GetMetadataValue("memory_policy"),
			MessageCount:    sess.MessageCount(),
			UpdatedAt:       time.Now(),
			AgeSeconds:      1,
			IsSchedule:      sess.GetMetadataValue("session_kind") == "schedule",
			IsMemoryIgnored: sess.GetMetadataValue("memory_policy") == "ignore",
		})
	}
	return items
}

func (g *stubSessionGateway) Get(key string) (*session.Session, error) {
	sess, ok := g.sessions[key]
	if !ok {
		return nil, context.DeadlineExceeded
	}
	return sess, nil
}

func (g *stubSessionGateway) GetOrCreate(key string) *session.Session {
	if sess, ok := g.sessions[key]; ok {
		return sess
	}
	sess := session.NewSession(key, key, nil)
	g.sessions[key] = sess
	return sess
}

func (g *stubSessionGateway) SendSessionMessage(ctx context.Context, sessionKey string, content string) (int, error) {
	g.lastSent.sessionKey = sessionKey
	g.lastSent.content = content
	g.lastSent.media = nil
	g.GetOrCreate(sessionKey).Append(models.Message{Role: "assistant", Content: content})
	return 1, nil
}

func (g *stubSessionGateway) SendSessionOutgoingMessage(ctx context.Context, sessionKey string, outgoing *models.OutgoingMessage) (int, error) {
	g.lastSent.sessionKey = sessionKey
	if outgoing != nil {
		g.lastSent.content = outgoing.Content
		g.lastSent.media = append([]models.OutgoingMedia(nil), outgoing.Media...)
		g.GetOrCreate(sessionKey).Append(models.Message{Role: "assistant", Content: outgoing.Content})
	}
	return 1, nil
}

func (g *stubSessionGateway) RunSessionInput(ctx context.Context, agentID, sessionKey, input string) error {
	g.lastRun.agentID = agentID
	g.lastRun.sessionKey = sessionKey
	g.lastRun.input = input
	g.GetOrCreate(sessionKey).Append(models.Message{Role: "user", Content: input})
	g.GetOrCreate(sessionKey).Append(models.Message{Role: "assistant", Content: "ok"})
	return nil
}

func TestSessionToolPolicyAdminAlwaysAllowed(t *testing.T) {
	policy := NewSessionToolPolicy(config.SessionToolConfig{})
	for _, toolName := range []string{"sessions_list", "sessions_history", "sessions_send", "sessions_run"} {
		if !policy.IsAllowed(models.RoleAdmin, toolName) {
			t.Fatalf("expected admin to be allowed for %s", toolName)
		}
	}
	if !policy.CanAccess(models.RoleAdmin, "", "", "", "anything") {
		t.Fatal("expected admin to access any session")
	}
}

func TestSessionListToolReturnsSessionMetadata(t *testing.T) {
	policy := NewSessionToolPolicy(config.SessionToolConfig{})
	gateway := &stubSessionGateway{sessions: map[string]*session.Session{}}
	sess := gateway.GetOrCreate("agent:agent-a:user:alice:schedule:task-1")
	sess.SetMetadataValue("session_kind", "schedule")
	sess.SetMetadataValue("memory_policy", "ignore")
	sess.Append(models.Message{Role: "user", Content: "hello"})
	tool := NewSessionListTool("agent-a", func() SessionGateway { return gateway }, policy)

	result, err := tool.ExecuteWithUserContext(context.Background(), nil, models.RoleAdmin, "admin", true, t.TempDir())
	if err != nil {
		t.Fatalf("sessions_list: %v", err)
	}
	for _, expected := range []string{"\"agent_id\": \"agent-a\"", "\"user_id\": \"alice\"", "\"session_kind\": \"schedule\"", "\"memory_policy\": \"ignore\"", "\"is_schedule\": true", "\"is_memory_ignored\": true", "\"age_seconds\""} {
		if !strings.Contains(result, expected) {
			t.Fatalf("expected %s in sessions_list output, got: %s", expected, result)
		}
	}
	if strings.Contains(result, "curation") {
		t.Fatalf("did not expect curation hint in generic sessions_list output: %s", result)
	}
}

func TestSessionSendToolRespectsConfiguredEmployeeVisibility(t *testing.T) {
	policy := NewSessionToolPolicy(config.SessionToolConfig{
		Enabled: true,
		Visibility: config.SessionVisibilityConfig{
			Employee: "user",
		},
		Allow: config.SessionAllowConfig{
			Employee: []string{"sessions_send"},
		},
	})
	gateway := &stubSessionGateway{sessions: map[string]*session.Session{}}
	tool := NewSessionSendTool("agent-a", func() SessionGateway { return gateway }, policy)
	ctx := WithSessionKey(context.Background(), "agent:agent-a:user:alice:ws")

	_, err := tool.ExecuteWithUserContext(ctx, map[string]interface{}{
		"session_key": "agent:agent-a:user:alice:schedule:task-1",
		"message":     "hello alice",
	}, models.RoleEmployee, "alice", true, t.TempDir())
	if err != nil {
		t.Fatalf("expected employee send to same user session to succeed: %v", err)
	}
	if gateway.lastSent.sessionKey != "agent:agent-a:user:alice:schedule:task-1" {
		t.Fatalf("unexpected target session: %q", gateway.lastSent.sessionKey)
	}
	if gateway.lastSent.content != "hello alice" {
		t.Fatalf("unexpected sent content: %q", gateway.lastSent.content)
	}

	_, err = tool.ExecuteWithUserContext(ctx, map[string]interface{}{
		"session_key": "agent:agent-a:user:bob:ws",
		"message":     "hello bob",
	}, models.RoleEmployee, "alice", true, t.TempDir())
	if err == nil {
		t.Fatal("expected employee send to another user session to be denied")
	}
	if !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSessionSendToolDefaultsToCurrentSessionAndSupportsMedia(t *testing.T) {
	policy := NewSessionToolPolicy(config.SessionToolConfig{
		Enabled: true,
		Visibility: config.SessionVisibilityConfig{
			Employee: "self",
		},
		Allow: config.SessionAllowConfig{
			Employee: []string{"sessions_send"},
		},
	})
	gateway := &stubSessionGateway{sessions: map[string]*session.Session{}}
	tool := NewSessionSendTool("agent-a", func() SessionGateway { return gateway }, policy)
	ctx := WithSessionKey(context.Background(), "agent:agent-a:user:alice:ws")

	result, err := tool.ExecuteWithUserContext(ctx, map[string]interface{}{
		"content": "see attachment",
		"media": []map[string]interface{}{
			{
				"type":                "image",
				"name":                "demo.png",
				"encrypt_query_param": "enc=image",
				"aes_key":             "aes-image",
			},
		},
	}, models.RoleEmployee, "alice", true, t.TempDir())
	if err != nil {
		t.Fatalf("expected send to current session to succeed: %v", err)
	}
	if !strings.Contains(result, "agent:agent-a:user:alice:ws") {
		t.Fatalf("unexpected result: %q", result)
	}
	if gateway.lastSent.sessionKey != "agent:agent-a:user:alice:ws" {
		t.Fatalf("unexpected default target session: %q", gateway.lastSent.sessionKey)
	}
	if len(gateway.lastSent.media) != 1 || gateway.lastSent.media[0].Type != "image" {
		t.Fatalf("unexpected sent media: %#v", gateway.lastSent.media)
	}
}

func TestSessionSendToolRejectsAmbiguousSessionKey(t *testing.T) {
	policy := NewSessionToolPolicy(config.SessionToolConfig{})
	gateway := &stubSessionGateway{sessions: map[string]*session.Session{}}
	gateway.GetOrCreate("wecom")
	gateway.GetOrCreate("wecom:YangXinNing")
	tool := NewSessionSendTool("agent-a", func() SessionGateway { return gateway }, policy)
	ctx := WithSessionKey(context.Background(), "agent:agent-a:user:alice:ws")

	_, err := tool.ExecuteWithUserContext(ctx, map[string]interface{}{
		"session_key": "wecom",
		"content":     "hello",
	}, models.RoleAdmin, "alice", true, t.TempDir())
	if err == nil {
		t.Fatal("expected ambiguous session key error")
	}
	if !strings.Contains(err.Error(), "ambiguous") || !strings.Contains(err.Error(), "wecom:YangXinNing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSessionSendToolParametersExposeContentAndMedia(t *testing.T) {
	tool := NewSessionSendTool("agent-a", nil, NewSessionToolPolicy(config.SessionToolConfig{}))
	params := tool.Parameters()
	properties, ok := params["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected properties map, got %#v", params["properties"])
	}
	if _, ok := properties["content"]; !ok {
		t.Fatal("expected sessions_send parameters to expose content")
	}
	if _, ok := properties["media"]; !ok {
		t.Fatal("expected sessions_send parameters to expose media")
	}
	if prop, ok := properties["session_key"].(map[string]interface{}); !ok || !strings.Contains(prop["description"].(string), "Defaults to the current session") {
		t.Fatalf("expected session_key description to mention default current session, got %#v", properties["session_key"])
	}
}

func TestSessionRunToolRespectsConfiguredEmployeeVisibility(t *testing.T) {
	policy := NewSessionToolPolicy(config.SessionToolConfig{
		Enabled: true,
		Visibility: config.SessionVisibilityConfig{
			Employee: "user",
		},
		Allow: config.SessionAllowConfig{
			Employee: []string{"sessions_run"},
		},
	})
	gateway := &stubSessionGateway{sessions: map[string]*session.Session{}}
	tool := NewSessionRunTool("agent-a", func() SessionGateway { return gateway }, policy)
	ctx := WithSessionKey(context.Background(), "agent:agent-a:user:alice:ws")

	result, err := tool.ExecuteWithUserContext(ctx, map[string]interface{}{
		"session_key": "agent:agent-a:user:alice:schedule:task-1",
		"input":       "run for alice",
	}, models.RoleEmployee, "alice", true, t.TempDir())
	if err != nil {
		t.Fatalf("expected employee run to same user session to succeed: %v", err)
	}
	if !strings.Contains(result, "Triggered session run") {
		t.Fatalf("unexpected result: %q", result)
	}
	if gateway.lastRun.sessionKey != "agent:agent-a:user:alice:schedule:task-1" {
		t.Fatalf("unexpected target session: %q", gateway.lastRun.sessionKey)
	}
	if gateway.lastRun.agentID != "agent-a" {
		t.Fatalf("unexpected agent id: %q", gateway.lastRun.agentID)
	}

	_, err = tool.ExecuteWithUserContext(ctx, map[string]interface{}{
		"session_key": "agent:agent-a:user:bob:ws",
		"input":       "run for bob",
	}, models.RoleEmployee, "alice", true, t.TempDir())
	if err == nil {
		t.Fatal("expected employee run to another user session to be denied")
	}
	if !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("unexpected error: %v", err)
	}
}
