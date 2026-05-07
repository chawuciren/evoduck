package gateway

import (
	"errors"
	"testing"

	"github.com/chawuciren/evoduck/pkg/models"
)

func TestBuildChannelEventPlan(t *testing.T) {
	plan := &models.TaskPlan{Intent: "Investigate issue"}
	event := buildChannelEvent(models.StreamEvent{Type: models.StreamEventPlan, Plan: plan}, "", "stream-1")
	if event == nil {
		t.Fatal("expected plan channel event")
	}
	if event.Type != models.ChannelEventPlan {
		t.Fatalf("expected plan event type, got %q", event.Type)
	}
	if event.Plan != plan {
		t.Fatal("expected plan pointer to be preserved")
	}
}

func TestLegacyChannelEventDeliveryIgnoresRunStart(t *testing.T) {
	g := &Gateway{}
	if err := g.deliverLegacyChannelEvent(nil, &models.NormalizedMessage{}, &models.ChannelEvent{Type: models.ChannelEventRunStart}); err != nil {
		t.Fatalf("expected run_start to be ignored by legacy delivery, got %v", err)
	}
}

func TestBuildChannelEventContentChunk(t *testing.T) {
	event := buildChannelEvent(models.StreamEvent{Type: models.StreamEventContent, Content: "hello"}, "hello", "stream-1")
	if event == nil {
		t.Fatal("expected content chunk event")
	}
	if event.Type != models.ChannelEventContentChunk {
		t.Fatalf("expected content chunk type, got %q", event.Type)
	}
	if event.Content != "hello" {
		t.Fatalf("unexpected content: %q", event.Content)
	}
}

func TestBuildChannelEventThinking(t *testing.T) {
	event := buildChannelEvent(models.StreamEvent{Type: "thinking", ThinkingContent: "reasoning"}, "", "stream-1")
	if event == nil {
		t.Fatal("expected thinking event")
	}
	if event.Type != models.ChannelEventThinking {
		t.Fatalf("expected thinking event type, got %q", event.Type)
	}
	if event.Content != "reasoning" {
		t.Fatalf("unexpected thinking content: %q", event.Content)
	}
}

func TestBuildChannelEventPlanUpdatePreservesType(t *testing.T) {
	plan := &models.TaskPlan{Intent: "Updated plan"}
	event := buildChannelEvent(models.StreamEvent{Type: models.StreamEventPlanUpdate, Plan: plan}, "", "stream-1")
	if event == nil {
		t.Fatal("expected plan_update channel event")
	}
	if event.Type != models.ChannelEventPlanUpdate {
		t.Fatalf("expected plan_update event type, got %q", event.Type)
	}
}

func TestBuildChannelEventFinalUsesAggregatedContent(t *testing.T) {
	event := buildChannelEvent(models.StreamEvent{Type: models.StreamEventDone}, "  final answer  ", "stream-1")
	if event == nil {
		t.Fatal("expected final event")
	}
	if event.Type != models.ChannelEventFinal {
		t.Fatalf("expected final event type, got %q", event.Type)
	}
	if event.Content != "final answer" {
		t.Fatalf("unexpected final content: %q", event.Content)
	}
	if !event.Done {
		t.Fatal("expected final event to be marked done")
	}
}

func TestBuildChannelEventStopUsesAggregatedContent(t *testing.T) {
	event := buildChannelEvent(models.StreamEvent{Type: "stop"}, "  final answer  ", "stream-1")
	if event == nil {
		t.Fatal("expected final event")
	}
	if event.Type != models.ChannelEventFinal {
		t.Fatalf("expected final event type, got %q", event.Type)
	}
	if event.Content != "final answer" {
		t.Fatalf("unexpected final content: %q", event.Content)
	}
}

func TestBuildChannelEventError(t *testing.T) {
	event := buildChannelEvent(models.StreamEvent{Type: models.StreamEventError, Error: errors.New("boom")}, "", "stream-1")
	if event == nil {
		t.Fatal("expected error event")
	}
	if event.Type != models.ChannelEventError {
		t.Fatalf("expected error event type, got %q", event.Type)
	}
	if event.ErrorText != "boom" {
		t.Fatalf("unexpected error text: %q", event.ErrorText)
	}
	if !event.Done {
		t.Fatal("expected error event to be done")
	}
}

func TestBuildChannelEventCancelledDefaultMessage(t *testing.T) {
	event := buildChannelEvent(models.StreamEvent{Type: models.StreamEventCancelled}, "", "stream-1")
	if event == nil {
		t.Fatal("expected cancelled event")
	}
	if event.Type != models.ChannelEventCancelled {
		t.Fatalf("expected cancelled event type, got %q", event.Type)
	}
	if event.Content != "Request cancelled." {
		t.Fatalf("unexpected cancelled content: %q", event.Content)
	}
}

func TestBuildChannelEventCancelledUsesTrimmedContent(t *testing.T) {
	event := buildChannelEvent(models.StreamEvent{Type: models.StreamEventCancelled, Content: "  stopped by user  "}, "", "stream-1")
	if event == nil {
		t.Fatal("expected cancelled event")
	}
	if event.Content != "stopped by user" {
		t.Fatalf("unexpected cancelled content: %q", event.Content)
	}
}

func TestBuildChannelEventToolEndTrimsToolName(t *testing.T) {
	event := buildChannelEvent(models.StreamEvent{Type: models.StreamEventToolEnd, ToolName: "  search  "}, "", "stream-1")
	if event == nil {
		t.Fatal("expected tool_end event")
	}
	if event.Type != models.ChannelEventToolEnd {
		t.Fatalf("expected tool_end event type, got %q", event.Type)
	}
	if event.ToolName != "search" {
		t.Fatalf("unexpected tool name: %q", event.ToolName)
	}
}

func TestBuildChannelEventToolStartTrimsToolName(t *testing.T) {
	event := buildChannelEvent(models.StreamEvent{Type: "tool_start", ToolName: "  search  "}, "", "stream-1")
	if event == nil {
		t.Fatal("expected tool_start event")
	}
	if event.Type != models.ChannelEventToolStart {
		t.Fatalf("expected tool_start event type, got %q", event.Type)
	}
	if event.ToolName != "search" {
		t.Fatalf("unexpected tool name: %q", event.ToolName)
	}
}

func TestBuildChannelEventSkipsInvalidCases(t *testing.T) {
	if event := buildChannelEvent(models.StreamEvent{Type: models.StreamEventToolEnd}, "", "stream-1"); event != nil {
		t.Fatal("expected tool_end without name to be skipped")
	}
	if event := buildChannelEvent(models.StreamEvent{Type: models.StreamEventDone}, "   ", "stream-1"); event != nil {
		t.Fatal("expected empty final content to be skipped")
	}
	if event := buildChannelEvent(models.StreamEvent{Type: "unknown"}, "", "stream-1"); event != nil {
		t.Fatal("expected unknown event to be skipped")
	}
}
