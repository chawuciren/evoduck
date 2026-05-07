package scheduler

import "testing"

func TestRunStoreAppendAndListNewestFirst(t *testing.T) {
	store := NewRunStore(t.TempDir())
	if err := store.Append(ScheduleRunRecord{
		RunID:           "run-1",
		ScheduleID:      "sched-1",
		SessionKey:      "agent:agent-test:user:admin:schedule:sched-1",
		TriggerSource:   TriggerSourceCron,
		StartedAt:       10,
		FinishedAt:      11,
		ExecutionStatus: ExecutionStatusSuccess,
		DeliveryStatus:  DeliveryStatusNotAttempted,
	}); err != nil {
		t.Fatalf("append first run: %v", err)
	}
	if err := store.Append(ScheduleRunRecord{
		RunID:           "run-2",
		ScheduleID:      "sched-1",
		SessionKey:      "agent:agent-test:user:admin:schedule:sched-1",
		TriggerSource:   TriggerSourceManual,
		StartedAt:       20,
		FinishedAt:      21,
		ExecutionStatus: ExecutionStatusFailed,
		DeliveryStatus:  DeliveryStatusNotAttempted,
		Error:           "boom",
	}); err != nil {
		t.Fatalf("append second run: %v", err)
	}
	items, err := store.List("sched-1", 1)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 run, got %d", len(items))
	}
	if items[0].RunID != "run-2" {
		t.Fatalf("expected newest run first, got %q", items[0].RunID)
	}
}
