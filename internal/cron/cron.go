package cron

import (
	"context"
	"fmt"
	"sort"
	"sync"

	robfigcron "github.com/robfig/cron/v3"
	"github.com/chawuciren/evoduck/pkg/logger"
)

var crLog = logger.NewModuleLogger("cron")

type JobScope string

const (
	JobScopeSystem JobScope = "system"
	JobScopeAgent  JobScope = "agent"
	JobScopeUser   JobScope = "user"
)

type Handler func(context.Context) error

type Job struct {
	ID          string
	Name        string
	Scope       JobScope
	AgentID     string
	Schedule    string
	Prompt      string
	Channel     string
	Description string
	Enabled     bool
	Handler     Handler
}

type Cron struct {
	mu        sync.RWMutex
	scheduler *robfigcron.Cron
	jobs      map[string]*Job
	entryIDs  map[string]robfigcron.EntryID
}

func New() *Cron {
	return &Cron{
		scheduler: robfigcron.New(),
		jobs:      make(map[string]*Job),
		entryIDs:  make(map[string]robfigcron.EntryID),
	}
}

func (c *Cron) Register(job Job) error {
	if job.ID == "" {
		return fmt.Errorf("job id is required")
	}
	if job.Schedule == "" {
		return fmt.Errorf("job schedule is required")
	}
	if job.Handler == nil {
		return fmt.Errorf("job handler is required")
	}
	if job.Scope == "" {
		job.Scope = JobScopeSystem
	}
	if !job.Enabled {
		job.Enabled = true
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.jobs[job.ID]; exists {
		return fmt.Errorf("job already exists: %s", job.ID)
	}

	entryID, err := c.scheduler.AddFunc(job.Schedule, func() {
		crLog.Info("Job triggered", logger.Fields{
			"job_id":   job.ID,
			"scope":    string(job.Scope),
			"agent_id": job.AgentID,
		})
		if err := job.Handler(context.Background()); err != nil {
			crLog.Error("Job execution failed", logger.Fields{
				"job_id":   job.ID,
				"scope":    string(job.Scope),
				"agent_id": job.AgentID,
				"error":    err.Error(),
			})
		}
	})
	if err != nil {
		return err
	}

	jobCopy := job
	c.jobs[job.ID] = &jobCopy
	c.entryIDs[job.ID] = entryID
	crLog.Info("Job registered", logger.Fields{
		"job_id":   job.ID,
		"scope":    string(job.Scope),
		"agent_id": job.AgentID,
		"schedule": job.Schedule,
	})
	return nil
}

func (c *Cron) AddJob(job Job) error {
	return c.Register(job)
}

func (c *Cron) Exists(id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.jobs[id]
	return ok
}

func (c *Cron) Update(job Job) error {
	if job.ID == "" {
		return fmt.Errorf("job id is required")
	}
	if job.Schedule == "" {
		return fmt.Errorf("job schedule is required")
	}
	if job.Handler == nil {
		return fmt.Errorf("job handler is required")
	}
	if job.Scope == "" {
		job.Scope = JobScopeSystem
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if entryID, ok := c.entryIDs[job.ID]; ok {
		c.scheduler.Remove(entryID)
		delete(c.entryIDs, job.ID)
	}

	jobCopy := job
	c.jobs[job.ID] = &jobCopy
	if !job.Enabled {
		return nil
	}

	entryID, err := c.scheduler.AddFunc(job.Schedule, func() {
		crLog.Info("Job triggered", logger.Fields{
			"job_id":   job.ID,
			"scope":    string(job.Scope),
			"agent_id": job.AgentID,
		})
		if err := job.Handler(context.Background()); err != nil {
			crLog.Error("Job execution failed", logger.Fields{
				"job_id":   job.ID,
				"scope":    string(job.Scope),
				"agent_id": job.AgentID,
				"error":    err.Error(),
			})
		}
	})
	if err != nil {
		delete(c.jobs, job.ID)
		return err
	}

	c.entryIDs[job.ID] = entryID
	return nil
}

func (c *Cron) SetEnabled(id string, enabled bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	job, ok := c.jobs[id]
	if !ok {
		return fmt.Errorf("job not found: %s", id)
	}
	if job.Enabled == enabled {
		return nil
	}

	if entryID, ok := c.entryIDs[id]; ok {
		c.scheduler.Remove(entryID)
		delete(c.entryIDs, id)
	}

	job.Enabled = enabled
	if !enabled {
		return nil
	}

	jobCopy := *job
	entryID, err := c.scheduler.AddFunc(job.Schedule, func() {
		crLog.Info("Job triggered", logger.Fields{
			"job_id":   jobCopy.ID,
			"scope":    string(jobCopy.Scope),
			"agent_id": jobCopy.AgentID,
		})
		if err := jobCopy.Handler(context.Background()); err != nil {
			crLog.Error("Job execution failed", logger.Fields{
				"job_id":   jobCopy.ID,
				"scope":    string(jobCopy.Scope),
				"agent_id": jobCopy.AgentID,
				"error":    err.Error(),
			})
		}
	})
	if err != nil {
		job.Enabled = false
		return err
	}
	c.entryIDs[id] = entryID
	return nil
}

func (c *Cron) Remove(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	entryID, ok := c.entryIDs[id]
	if !ok {
		return false
	}

	c.scheduler.Remove(entryID)
	delete(c.entryIDs, id)
	delete(c.jobs, id)
	crLog.Info("Job removed", logger.Fields{"job_id": id})
	return true
}

func (c *Cron) Start() {
	c.scheduler.Start()
	crLog.Info("Scheduler started", nil)
}

func (c *Cron) Stop() {
	ctx := c.scheduler.Stop()
	<-ctx.Done()
	crLog.Info("Scheduler stopped", nil)
}

func (c *Cron) GetJob(id string) *Job {
	c.mu.RLock()
	defer c.mu.RUnlock()

	job := c.jobs[id]
	if job == nil {
		return nil
	}
	jobCopy := *job
	return &jobCopy
}

func (c *Cron) ListJobs() []*Job {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ids := make([]string, 0, len(c.jobs))
	for id := range c.jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	jobs := make([]*Job, 0, len(ids))
	for _, id := range ids {
		jobCopy := *c.jobs[id]
		jobs = append(jobs, &jobCopy)
	}
	return jobs
}

func (c *Cron) ListJobsByScope(scope JobScope) []*Job {
	all := c.ListJobs()
	filtered := make([]*Job, 0, len(all))
	for _, job := range all {
		if job != nil && job.Scope == scope {
			filtered = append(filtered, job)
		}
	}
	return filtered
}
