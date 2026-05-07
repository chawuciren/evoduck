package scheduler

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	cronpkg "github.com/chawuciren/evoduck/internal/cron"
	"github.com/chawuciren/evoduck/pkg/logger"
)

var schLog = logger.NewModuleLogger("scheduler")

type Service struct {
	mu        sync.RWMutex
	scheduler *cronpkg.Cron
	store     *Store
	runStore  *RunStore
	executor  Executor
	inflight  map[string]struct{}
	tasks     map[string]ScheduleRecord
}

func NewService(runtime *cronpkg.Cron, store *Store, runStore *RunStore, executor Executor) *Service {
	return &Service{
		scheduler: runtime,
		store:     store,
		runStore:  runStore,
		executor:  executor,
		inflight:  make(map[string]struct{}),
		tasks:     make(map[string]ScheduleRecord),
	}
}

func (s *Service) ListRuns(id string, limit int) ([]ScheduleRunRecord, error) {
	if s.runStore == nil {
		return []ScheduleRunRecord{}, nil
	}
	return s.runStore.List(id, limit)
}

func (s *Service) Load() error {
	if s.store == nil {
		return nil
	}
	schedules, err := s.store.LoadAll()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, schedule := range schedules {
		s.tasks[schedule.ID] = schedule
	}
	return nil
}

func (s *Service) Save() error {
	if s.store == nil {
		return nil
	}
	s.mu.RLock()
	schedules := make([]ScheduleRecord, 0, len(s.tasks))
	for _, schedule := range s.tasks {
		if schedule.Scope == ScheduleScopeSystem {
			continue
		}
		if strings.TrimSpace(schedule.ID) == "" || strings.TrimSpace(schedule.Schedule) == "" {
			continue
		}
		schedules = append(schedules, schedule)
	}
	s.mu.RUnlock()
	sort.Slice(schedules, func(i, j int) bool { return schedules[i].ID < schedules[j].ID })
	return s.store.SaveAll(schedules)
}

func (s *Service) Register(schedule ScheduleRecord) error {
	if schedule.Scope == ScheduleScopeSystem {
		return fmt.Errorf("system schedules must be registered directly on runtime scheduler: %s", schedule.ID)
	}
	schedule.TouchCreated(time.Now())
	handler := func(ctx context.Context) error {
		return s.runSchedule(schedule.ID, TriggerSourceCron)
	}
	if s.scheduler.Exists(schedule.ID) {
		if err := s.scheduler.Update(cronpkg.Job{
			ID:          schedule.ID,
			Name:        schedule.Name,
			Scope:       schedule.Scope,
			AgentID:     schedule.AgentID,
			Schedule:    schedule.Schedule,
			Prompt:      schedule.Prompt,
			Channel:     schedule.Channel,
			Description: schedule.Description,
			Enabled:     schedule.Enabled,
			Handler:     handler,
		}); err != nil {
			return err
		}
	} else {
		if err := s.scheduler.Register(cronpkg.Job{
			ID:          schedule.ID,
			Name:        schedule.Name,
			Scope:       schedule.Scope,
			AgentID:     schedule.AgentID,
			Schedule:    schedule.Schedule,
			Prompt:      schedule.Prompt,
			Channel:     schedule.Channel,
			Description: schedule.Description,
			Enabled:     schedule.Enabled,
			Handler:     handler,
		}); err != nil {
			return err
		}
	}

	s.mu.Lock()
	s.tasks[schedule.ID] = schedule
	s.mu.Unlock()
	return s.Save()
}

func (s *Service) RegisterSystem(schedule ScheduleRecord) {
	if schedule.Scope == "" {
		schedule.Scope = ScheduleScopeSystem
	}
	if schedule.ConcurrencyPolicy == "" {
		schedule.ConcurrencyPolicy = ConcurrencyPolicySkipIfRunning
	}
	schedule.Enabled = true
	schedule.TouchCreated(time.Now())
	s.mu.Lock()
	s.tasks[schedule.ID] = schedule
	s.mu.Unlock()
}

func (s *Service) RegisterLoadedTasks() error {
	s.mu.RLock()
	schedules := make([]ScheduleRecord, 0, len(s.tasks))
	for _, schedule := range s.tasks {
		schedules = append(schedules, schedule)
	}
	s.mu.RUnlock()

	for _, schedule := range schedules {
		if err := s.Register(schedule); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) List() []ScheduleRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ScheduleRecord, 0, len(s.tasks))
	for _, schedule := range s.tasks {
		out = append(out, schedule)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Service) ListByScope(scope ScheduleScope) []ScheduleRecord {
	all := s.List()
	filtered := make([]ScheduleRecord, 0, len(all))
	for _, schedule := range all {
		if schedule.Scope == scope {
			filtered = append(filtered, schedule)
		}
	}
	return filtered
}

func (s *Service) ListForOwner(scope ScheduleScope, agentID, userID string) []ScheduleRecord {
	schedules := s.ListByScope(scope)
	filtered := make([]ScheduleRecord, 0, len(schedules))
	for _, schedule := range schedules {
		if agentID != "" && schedule.AgentID != agentID {
			continue
		}
		if scope == ScheduleScopeUser && userID != "" && schedule.UserID != userID {
			continue
		}
		filtered = append(filtered, schedule)
	}
	return filtered
}

func (s *Service) Get(id string) (ScheduleRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	schedule, ok := s.tasks[id]
	return schedule, ok
}

func (s *Service) Create(schedule ScheduleRecord) (ScheduleRecord, error) {
	if schedule.ID == "" {
		return ScheduleRecord{}, fmt.Errorf("schedule id is required")
	}
	if schedule.Name == "" {
		return ScheduleRecord{}, fmt.Errorf("schedule name is required")
	}
	if schedule.Schedule == "" {
		return ScheduleRecord{}, fmt.Errorf("schedule expression is required")
	}
	if schedule.Prompt == "" {
		return ScheduleRecord{}, fmt.Errorf("schedule prompt is required")
	}
	if schedule.ConcurrencyPolicy == "" {
		schedule.ConcurrencyPolicy = ConcurrencyPolicySkipIfRunning
	}

	s.mu.RLock()
	_, exists := s.tasks[schedule.ID]
	s.mu.RUnlock()
	if exists {
		return ScheduleRecord{}, fmt.Errorf("schedule already exists: %s", schedule.ID)
	}

	if err := s.Register(schedule); err != nil {
		return ScheduleRecord{}, err
	}
	created, _ := s.Get(schedule.ID)
	return created, nil
}

func (s *Service) TriggerNow(id string, source TriggerSource) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("schedule id is required")
	}
	if source == "" {
		source = TriggerSourceManual
	}
	return s.runSchedule(id, source)
}

func (s *Service) SetEnabled(id string, enabled bool) error {
	s.mu.Lock()
	schedule, ok := s.tasks[id]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("schedule not found: %s", id)
	}
	if schedule.Enabled == enabled {
		s.mu.Unlock()
		return nil
	}
	schedule.Enabled = enabled
	schedule.UpdatedAt = time.Now().Unix()
	s.tasks[id] = schedule
	s.mu.Unlock()

	if s.scheduler != nil {
		if err := s.scheduler.SetEnabled(id, enabled); err != nil {
			return err
		}
	}
	return s.Save()
}

func (s *Service) Delete(id string) error {
	s.mu.Lock()
	if _, ok := s.tasks[id]; !ok {
		s.mu.Unlock()
		return fmt.Errorf("schedule not found: %s", id)
	}
	delete(s.tasks, id)
	delete(s.inflight, id)
	s.mu.Unlock()

	if s.scheduler != nil {
		s.scheduler.Remove(id)
	}
	return s.Save()
}

func (s *Service) runSchedule(id string, source TriggerSource) error {
	runID := fmt.Sprintf("run-%d", time.Now().UnixNano())
	startedAt := time.Now()
	s.mu.Lock()
	if _, ok := s.inflight[id]; ok {
		s.mu.Unlock()
		schLog.Info("Skipping schedule trigger while already running", logger.Fields{"schedule_id": id, "trigger_source": string(source)})
		s.appendRunRecord(ScheduleRunRecord{
			RunID:           runID,
			ScheduleID:      id,
			TriggerSource:   source,
			StartedAt:       startedAt.Unix(),
			FinishedAt:      time.Now().Unix(),
			ExecutionStatus: ExecutionStatusSkipped,
			DeliveryStatus:  DeliveryStatusNotAttempted,
			Error:           fmt.Sprintf("schedule already running: %s", id),
		})
		return fmt.Errorf("schedule already running: %s", id)
	}
	s.inflight[id] = struct{}{}
	schedule, ok := s.tasks[id]
	s.mu.Unlock()
	if !ok {
		s.finishInflight(id)
		s.appendRunRecord(ScheduleRunRecord{
			RunID:           runID,
			ScheduleID:      id,
			TriggerSource:   source,
			StartedAt:       startedAt.Unix(),
			FinishedAt:      time.Now().Unix(),
			ExecutionStatus: ExecutionStatusFailed,
			DeliveryStatus:  DeliveryStatusNotAttempted,
			Error:           fmt.Sprintf("schedule not found: %s", id),
		})
		return fmt.Errorf("schedule not found: %s", id)
	}

	req := ExecutionRequest{Schedule: schedule, TriggerSource: source}
	err := s.executor.Execute(&req)
	runRecord := ScheduleRunRecord{
		RunID:         runID,
		ScheduleID:    id,
		SessionKey:    schedule.ExecutionSessionKey,
		TriggerSource: source,
		StartedAt:     startedAt.Unix(),
		FinishedAt:    time.Now().Unix(),
		DeliveryStatus: req.DeliveryStatus,
	}
	s.mu.Lock()
	updated := s.tasks[id]
	if err != nil {
		updated.MarkRunFailure(time.Now(), source, err)
		runRecord.ExecutionStatus = ExecutionStatusFailed
		if runRecord.DeliveryStatus == "" {
			runRecord.DeliveryStatus = DeliveryStatusFailed
		}
		runRecord.Error = err.Error()
	} else {
		updated.MarkRunSuccess(time.Now(), source)
		runRecord.ExecutionStatus = ExecutionStatusSuccess
		if runRecord.DeliveryStatus == "" {
			runRecord.DeliveryStatus = DeliveryStatusUnknown
		}
	}
	s.tasks[id] = updated
	delete(s.inflight, id)
	s.mu.Unlock()
	s.appendRunRecord(runRecord)
	if saveErr := s.Save(); saveErr != nil {
		schLog.Error("Failed to persist scheduler state", logger.Fields{"schedule_id": id, "error": saveErr.Error()})
	}
	return err
}

func (s *Service) appendRunRecord(record ScheduleRunRecord) {
	if s.runStore == nil || strings.TrimSpace(record.ScheduleID) == "" {
		return
	}
	if err := s.runStore.Append(record); err != nil {
		schLog.Error("Failed to append schedule run record", logger.Fields{"schedule_id": record.ScheduleID, "run_id": record.RunID, "error": err.Error()})
	}
}

func (s *Service) finishInflight(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, id)
}
