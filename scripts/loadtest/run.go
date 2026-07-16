package main

import (
	"context"
	"fmt"
	"log/slog"
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
// the peak burst. Definitions are cycled so the schedule covers the full phase
// duration even when there are fewer definitions than time slots. A monotonic
// global event index guarantees unique idempotency keys across cycles and burst.
// When cases is empty, burst events are emitted as placeholders with JobID=0 so
// schedule shape (count, span, ordering) stays testable without a live run.
func BuildPhaseSchedule(cfg Config, cases []CreatedDefinition) PhaseSchedule {
	var events []TriggerEvent
	eventIndex := 0
	caseIndex := 0
	for _, phase := range cfg.Phases {
		if phase.RatePerMinute == 0 || len(cases) == 0 {
			continue
		}
		interval := time.Minute / time.Duration(phase.RatePerMinute)
		for offset := phase.Offset; offset < phase.Offset+phase.Duration; offset += interval {
			c := cases[caseIndex%len(cases)]
			events = append(events, TriggerEvent{
				At:             offset,
				JobID:          c.JobID,
				DefinitionCase: c.CaseID,
				Tenant:         c.Tenant,
				IdempotencyKey: fmt.Sprintf("v020-%s-%06d", c.CaseID, eventIndex),
				Origin:         "manual",
				Phase:          phase.Name,
			})
			caseIndex++
			eventIndex++
		}
	}
	events = append(events, burstEvents(cfg, cases, eventIndex)...)
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
			IdempotencyKey: fmt.Sprintf("v020-burst-%06d", startIndex+i),
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
	api        map[string]*APIClient
	schedule   PhaseSchedule
	maxActive  map[string]int
	pace       *PaceController
	promClient *PrometheusClient
	stop       chan struct{}
	triggered  atomic.Int64
	accepted   atomic.Int64
	rejected   atomic.Int64
}

func NewRunEngine(api map[string]*APIClient, schedule PhaseSchedule, maxActive map[string]int) *RunEngine {
	return &RunEngine{api: api, schedule: schedule, maxActive: maxActive, stop: make(chan struct{})}
}

func NewRunEngineWithPace(api map[string]*APIClient, schedule PhaseSchedule, maxActive map[string]int, pace *PaceController, promClient *PrometheusClient) *RunEngine {
	return &RunEngine{api: api, schedule: schedule, maxActive: maxActive, pace: pace, promClient: promClient, stop: make(chan struct{})}
}

func (e *RunEngine) Run(ctx context.Context, clock func() time.Duration) error {
	active := 0
	var activeMu sync.Mutex
	dynamic := e.pace != nil

	if dynamic && e.promClient != nil {
		ticker := time.NewTicker(time.Duration(e.pace.cfg.SampleIntervalSec) * time.Second)
		defer ticker.Stop()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-e.stop:
					return
				case <-ticker.C:
					pf, pressure, err := e.pace.Update(ctx, e.promClient, e.accepted.Load(), clock())
					if err == nil {
						slog.Debug("pace controller update", "pace", pf, "pressure", pressure)
					}
				}
			}
		}()
	}

	var lastReal time.Duration
	virtual := time.Duration(0)
	progressLast := time.Duration(0)

	for _, event := range e.schedule.Events {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-e.stop:
			return nil
		default:
		}

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-e.stop:
				return nil
			default:
			}
			nowReal := clock()
			dt := nowReal - lastReal
			if dt < 0 {
				dt = 0
			}
			if dynamic {
				virtual += time.Duration(float64(dt) * e.pace.Pace())
			} else {
				virtual += dt
			}
			lastReal = nowReal
			if virtual >= event.At {
				break
			}
			time.Sleep(time.Millisecond)
		}

		skip := false
		for {
			activeMu.Lock()
			limit := e.maxActive[event.Phase]
			if limit > 0 && active >= limit {
				activeMu.Unlock()
				if !dynamic {
					skip = true
					break
				}
				time.Sleep(time.Millisecond)
				continue
			}
			active++
			activeMu.Unlock()
			break
		}
		if skip {
			continue
		}

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

		if nowReal := clock(); nowReal-progressLast >= time.Minute {
			progressLast = nowReal
			pace := 1.0
			if e.pace != nil {
				pace = e.pace.Pace()
			}
			fmt.Printf("loadtest progress: triggered=%d accepted=%d rejected=%d pace=%.2f\n", e.triggered.Load(), e.accepted.Load(), e.rejected.Load(), pace)
		}
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
