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
	DefinitionCase string
	Tenant         string
	IdempotencyKey string
	Origin         string
	Phase          string
}

type PhaseSchedule struct {
	Events []TriggerEvent
}

func BuildPhaseSchedule(cfg Config) PhaseSchedule {
	var events []TriggerEvent
	caseIndex := 0
	manualCases := manualTriggerCases(cfg)
	for _, phase := range cfg.Phases {
		if phase.RatePerMinute == 0 {
			continue
		}
		interval := time.Minute / time.Duration(phase.RatePerMinute)
		for offset := phase.Offset; offset < phase.Offset+phase.Duration; offset += interval {
			if caseIndex >= len(manualCases) {
				break
			}
			def := manualCases[caseIndex]
			events = append(events, TriggerEvent{
				At:             offset,
				DefinitionCase: def.CaseID,
				Tenant:         def.Tenant,
				IdempotencyKey: fmt.Sprintf("v020-%s-%04d", def.CaseID, caseIndex),
				Origin:         def.TriggerOrigin,
				Phase:          phase.Name,
			})
			caseIndex++
		}
	}
	events = append(events, burstEvents(cfg, caseIndex)...)
	return PhaseSchedule{Events: events}
}

func manualTriggerCases(cfg Config) []Definition {
	// Placeholder: in the real run, this is populated from the generated manifest.
	// The scheduler only consumes the abstract event list, decoupling it from generation.
	return nil
}

func burstEvents(cfg Config, startIndex int) []TriggerEvent {
	var events []TriggerEvent
	if cfg.Burst.Count == 0 {
		return events
	}
	interval := cfg.Burst.SubmitWithin / time.Duration(cfg.Burst.Count)
	for i := 0; i < cfg.Burst.Count; i++ {
		events = append(events, TriggerEvent{
			At:             phaseOffset(cfg, cfg.Burst.Phase) + cfg.Burst.Offset + time.Duration(i)*interval,
			IdempotencyKey: fmt.Sprintf("v020-burst-%04d", startIndex+i),
			Origin:         "manual",
			Phase:          cfg.Burst.Phase,
		})
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
	api       *APIClient
	schedule  PhaseSchedule
	maxActive map[string]int
	stop      chan struct{}
	triggered atomic.Int64
	accepted  atomic.Int64
	rejected  atomic.Int64
	mu        sync.Mutex
}

func NewRunEngine(api *APIClient, schedule PhaseSchedule, maxActive map[string]int) *RunEngine {
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
		// Wait until event time.
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
			_, err := e.api.TriggerJob(ctx, 1, ev.Tenant, ev.IdempotencyKey)
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
