package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type TriggerEvent struct {
	At             time.Duration
	JobID          int64
	DefinitionCase string
	Tenant         string
	IdempotencyKey string
	Origin         string
	Phase          string
}

type PhaseSchedule struct {
	Events []TriggerEvent
}

// BuildPhaseSchedule assigns created definitions to phase time slots and appends
// the peak burst. When cases is empty, burst events are emitted as placeholders
// with JobID=0 so schedule shape (count, span, ordering) stays testable without
// a live prepared run.
func BuildPhaseSchedule(cfg Config, cases []CreatedDefinition) PhaseSchedule {
	var events []TriggerEvent
	caseIndex := 0
	for _, phase := range cfg.Phases {
		if phase.RatePerMinute == 0 {
			continue
		}
		interval := time.Minute / time.Duration(phase.RatePerMinute)
		for offset := phase.Offset; offset < phase.Offset+phase.Duration; offset += interval {
			if caseIndex >= len(cases) {
				break
			}
			c := cases[caseIndex]
			events = append(events, TriggerEvent{
				At:             offset,
				JobID:          c.JobID,
				DefinitionCase: c.CaseID,
				Tenant:         c.Tenant,
				IdempotencyKey: fmt.Sprintf("v020-%s-%04d", c.CaseID, caseIndex),
				Origin:         "manual",
				Phase:          phase.Name,
			})
			caseIndex++
		}
	}
	events = append(events, burstEvents(cfg, cases, caseIndex)...)
	return PhaseSchedule{Events: events}
}

func burstEvents(cfg Config, cases []CreatedDefinition, startIndex int) []TriggerEvent {
	var events []TriggerEvent
	if cfg.Burst.Count == 0 {
		return events
	}
	interval := cfg.Burst.SubmitWithin / time.Duration(cfg.Burst.Count)
	for i := 0; i < cfg.Burst.Count; i++ {
		ev := TriggerEvent{
			At:             phaseOffset(cfg, cfg.Burst.Phase) + cfg.Burst.Offset + time.Duration(i)*interval,
			IdempotencyKey: fmt.Sprintf("v020-burst-%04d", startIndex+i),
			Origin:         "manual",
			Phase:          cfg.Burst.Phase,
		}
		if len(cases) > 0 {
			c := cases[(startIndex+i)%len(cases)]
			ev.JobID = c.JobID
			ev.DefinitionCase = c.CaseID
			ev.Tenant = c.Tenant
		}
		events = append(events, ev)
	}
	return events
}

func phaseOffset(cfg Config, name string) time.Duration {
	for _, phase := range cfg.Phases {
		if phase.Name == name {
			return phase.Offset
		}
	}
	return 0
}

func CountBurst(events []TriggerEvent) int {
	count := 0
	for _, event := range events {
		if event.Phase == "peak" && event.Origin == "manual" && strings.HasPrefix(event.IdempotencyKey, "v020-burst-") {
			count++
		}
	}
	return count
}

type RunEngine struct {
	api       map[string]*APIClient
	schedule  PhaseSchedule
	maxActive map[string]int
	stop      chan struct{}
	triggered atomic.Int64
	accepted  atomic.Int64
	rejected  atomic.Int64
}

func NewRunEngine(api map[string]*APIClient, schedule PhaseSchedule, maxActive map[string]int) *RunEngine {
	return &RunEngine{api: api, schedule: schedule, maxActive: maxActive, stop: make(chan struct{})}
}

func (e *RunEngine) Run(ctx context.Context, clock func() time.Duration) error {
	active := 0
	var activeMu sync.Mutex
	for _, event := range e.schedule.Events {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-e.stop:
			return nil
		default:
		}
		for clock() < event.At {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Millisecond):
			}
		}
		activeMu.Lock()
		limit := e.maxActive[event.Phase]
		if limit > 0 && active >= limit {
			activeMu.Unlock()
			continue
		}
		active++
		activeMu.Unlock()

		e.triggered.Add(1)
		go func(ev TriggerEvent) {
			defer func() {
				activeMu.Lock()
				active--
				activeMu.Unlock()
			}()
			client := e.api[ev.Tenant]
			if client == nil || ev.JobID == 0 {
				e.rejected.Add(1)
				return
			}
			_, err := client.TriggerJob(ctx, ev.JobID, ev.Tenant, ev.IdempotencyKey)
			if err == nil {
				e.accepted.Add(1)
			} else {
				e.rejected.Add(1)
			}
		}(event)
	}
	return nil
}

func (e *RunEngine) Stop() {
	close(e.stop)
}

func (e *RunEngine) Stats() (triggered, accepted, rejected int64) {
	return e.triggered.Load(), e.accepted.Load(), e.rejected.Load()
}

// strings is used for burst prefix matching.
var _ = strings.HasPrefix
