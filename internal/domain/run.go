package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// RunState is a stage in an experiment run's lifecycle.
//
//	planned ──► running ◄──► paused
//	running ──► completed | failed ──► signed
//	planned | running | paused ──► aborted
type RunState string

const (
	RunPlanned   RunState = "planned"
	RunRunning   RunState = "running"
	RunPaused    RunState = "paused"
	RunCompleted RunState = "completed"
	RunFailed    RunState = "failed"
	RunAborted   RunState = "aborted"
	RunSigned    RunState = "signed"
)

var runTransitions = map[RunState][]RunState{
	RunPlanned:   {RunRunning, RunAborted},
	RunRunning:   {RunPaused, RunCompleted, RunFailed, RunAborted},
	RunPaused:    {RunRunning, RunAborted},
	RunCompleted: {RunSigned},
	RunFailed:    {RunSigned},
}

var ErrInvalidTransition = errors.New("run: invalid state transition")

func (s RunState) Valid() bool {
	_, ok := runTransitions[s]
	return ok || s == RunAborted || s == RunSigned
}

// Terminal reports whether no further transitions are possible. A signed run
// is sealed: corrections happen through a new, linked run, never in place.
func (s RunState) Terminal() bool { return len(runTransitions[s]) == 0 }

// CanTransition validates a move from s to next.
func (s RunState) CanTransition(next RunState) error {
	for _, allowed := range runTransitions[s] {
		if allowed == next {
			return nil
		}
	}
	return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, s, next)
}

// Run is a single execution of an experiment protocol.
type Run struct {
	ID           string          `json:"id"`
	TenantID     string          `json:"tenant_id"`
	ExperimentID string          `json:"experiment_id"`
	NodeID       string          `json:"node_id"` // this run's vertex in the lineage graph
	State        RunState        `json:"state"`
	Params       json.RawMessage `json:"params"`
	StartedAt    *time.Time      `json:"started_at,omitempty"`
	FinishedAt   *time.Time      `json:"finished_at,omitempty"`
	CreatedBy    string          `json:"created_by"`
	CreatedAt    time.Time       `json:"created_at"`
}

// Apply moves the run to next, stamping lifecycle timestamps. Aborting and
// signing require a reason; for signing it is the §11.50 signature meaning.
func (r *Run) Apply(next RunState, reason string, at time.Time) error {
	if err := r.State.CanTransition(next); err != nil {
		return err
	}
	if (next == RunSigned || next == RunAborted) && strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: %s requires a reason", ErrInvalidTransition, next)
	}
	if next == RunRunning && r.StartedAt == nil {
		r.StartedAt = &at
	}
	if next == RunCompleted || next == RunFailed || next == RunAborted {
		r.FinishedAt = &at
	}
	r.State = next
	return nil
}
