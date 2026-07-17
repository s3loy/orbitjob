package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
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
	// The engine consumes events in list order and fires anything whose At is
	// already past. Burst events are generated last but scheduled inside the
	// run window, so the merged schedule must be time-ordered — otherwise the
	// burst goes off in a lump at the end of the run and hits the trigger
	// rate limiter.
	sort.SliceStable(events, func(i, j int) bool { return events[i].At < events[j].At })
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
	skipped    atomic.Int64

	rejectRateLimited atomic.Int64
	rejectServer      atomic.Int64
	rejectTransport   atomic.Int64
	rejectOther       atomic.Int64
}

// RejectionBreakdown splits rejected triggers by cause so a long soak run can
// tell rate limiting apart from server faults, transport failures, and
// schedule/config problems (missing tenant client or placeholder events).
type RejectionBreakdown struct {
	RateLimited int64
	Server      int64
	Transport   int64
	Other       int64
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
			e.skipped.Add(1)
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
				e.recordRejection("other", ev, errors.New("no api client or placeholder event"))
				return
			}
			_, err := client.TriggerJob(ctx, ev.JobID, ev.Tenant, ev.IdempotencyKey)
			if err == nil {
				e.accepted.Add(1)
			} else {
				e.recordRejection(classifyRejection(err), ev, err)
			}
		}(event)

		if nowReal := clock(); nowReal-progressLast >= time.Minute {
			progressLast = nowReal
			pace := 1.0
			if e.pace != nil {
				pace = e.pace.Pace()
			}
			fmt.Printf("loadtest progress: triggered=%d accepted=%d rejected=%d skipped=%d pace=%.2f\n",
				e.triggered.Load(), e.accepted.Load(), e.rejected.Load(), e.skipped.Load(), pace)
		}
	}
	return nil
}

// classifyRejection buckets a trigger error for the breakdown counters.
func classifyRejection(err error) string {
	var terr *TriggerError
	if errors.As(err, &terr) {
		switch {
		case terr.StatusCode == 0:
			return "transport"
		case terr.StatusCode == http.StatusTooManyRequests:
			return "rate_limited"
		case terr.StatusCode >= 500:
			return "server"
		}
		return "other"
	}
	return "transport"
}

// recordRejection increments the total and per-cause counters and logs a
// throttled line: the first 20 rejections individually, then every 100th, so
// an 8h soak stays readable while early failures remain visible.
func (e *RunEngine) recordRejection(kind string, ev TriggerEvent, err error) {
	e.rejected.Add(1)
	switch kind {
	case "rate_limited":
		e.rejectRateLimited.Add(1)
	case "server":
		e.rejectServer.Add(1)
	case "transport":
		e.rejectTransport.Add(1)
	default:
		e.rejectOther.Add(1)
	}
	if n := e.rejected.Load(); n <= 20 || n%100 == 0 {
		fmt.Printf("loadtest rejected: kind=%s job=%d tenant=%s phase=%s err=%v\n", kind, ev.JobID, ev.Tenant, ev.Phase, err)
	}
}

func (e *RunEngine) Stop() {
	close(e.stop)
}

func (e *RunEngine) Stats() (triggered, accepted, rejected, skipped int64) {
	return e.triggered.Load(), e.accepted.Load(), e.rejected.Load(), e.skipped.Load()
}

func (e *RunEngine) Rejections() RejectionBreakdown {
	return RejectionBreakdown{
		RateLimited: e.rejectRateLimited.Load(),
		Server:      e.rejectServer.Load(),
		Transport:   e.rejectTransport.Load(),
		Other:       e.rejectOther.Load(),
	}
}

// strings is used for burst prefix matching.
var _ = strings.HasPrefix
