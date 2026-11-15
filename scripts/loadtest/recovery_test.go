package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"orbitjob/internal/platform/scan"
)

func TestRecoveryManifestPersistsNonceOutsideWorkloadCorpus(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{RunID: "recovery-run", Duration: time.Hour, Tenants: []string{scan.GenerateID()}, Faults: FaultsConfig{Plan: []FaultPhase{{Name: "operator", Offset: time.Minute}, {Name: "postgres", Offset: 2 * time.Minute}}}}
	lock, err := LoadImageLock("../../test/load/config/images.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err = generateRecoveryManifest(dir, cfg, lock); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "recovery.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = generateRecoveryManifest(dir, cfg, lock); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "recovery.json"))
	if string(before) != string(after) {
		t.Fatal("regenerated immutable nonce")
	}
	m, err := loadRecoveryManifest(dir, cfg)
	if err != nil || len(m.Canaries) != 3 {
		t.Fatalf("manifest=%+v err=%v", m, err)
	}
	for _, c := range m.Canaries {
		if c.Definition.ProductTriggerType != "manual" || c.Definition.ScheduledJob.Schedule != neverFiresCron || !strings.Contains(strings.Join(c.Definition.ScheduledJob.Args, " "), c.Nonce) {
			t.Fatal("canary lacks pinned nonce payload")
		}
	}
	if _, err = os.Stat(filepath.Join(dir, "definitions.json")); !os.IsNotExist(err) {
		t.Fatal("recovery generation wrote offered workload corpus")
	}
	for _, mutation := range []string{"tenant", "nonce", "stage", "offset", "payload", "extra"} {
		t.Run(mutation, func(t *testing.T) {
			var changed RecoveryManifest
			if err := json.Unmarshal(before, &changed); err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "tenant":
				changed.Canaries[0].Definition.Tenant = scan.GenerateID()
			case "nonce":
				changed.Canaries[0].Nonce = scan.GenerateID()
			case "stage":
				changed.Canaries[0].Stage = "pending"
			case "offset":
				changed.Canaries[0].Fault.Offset++
			case "payload":
				changed.Canaries[0].Definition.ScheduledJob.Args = []string{"unrelated"}
			case "extra":
				changed.Canaries = append(changed.Canaries, changed.Canaries[0])
			}
			if err := writeStableJSON(filepath.Join(dir, "recovery.json"), changed); err != nil {
				t.Fatal(err)
			}
			if _, err := loadRecoveryManifest(dir, cfg); err == nil {
				t.Fatal("tampered manifest accepted")
			}
		})
	}
}

func TestShippedJournalDetectsRestartEvenWithFreshRunEffects(t *testing.T) {
	first := startJournalFixture(t, false)
	before, err := readJournalSnapshot(context.Background(), first, "run-one")
	if err != nil {
		t.Fatal(err)
	}
	post := func(base, key string) {
		t.Helper()
		response, err := http.Post(base+"/ledger", "application/json", strings.NewReader(`{"case_id":"run-case","operation":"delivery","idempotency_key":"`+key+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
	}
	post(first, "original")
	current, err := readJournalSnapshot(context.Background(), first, "run-one")
	if err != nil {
		t.Fatal(err)
	}
	start := before.Cursor()
	if err = verifyJournalContinuity(&start, current); err != nil || current.Head != 1 {
		t.Fatalf("healthy journal %v %+v", err, current)
	}
	second := startJournalFixture(t, false)
	post(second, "new-after-restart")
	restarted, err := readJournalSnapshot(context.Background(), second, "run-one")
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Head != 1 || verifyJournalContinuity(&start, restarted) == nil {
		t.Fatal("partial new effects masked lost journal epoch")
	}
}

func TestJournalContinuityFailsClosed(t *testing.T) {
	baseline := JournalCursor{RunID: "run", BootID: scan.GenerateID(), Head: 0}
	valid := JournalSnapshot{Version: 1, RunID: baseline.RunID, BootID: baseline.BootID, Head: 1, Entries: []JournalEffect{{Sequence: 1, CaseID: "case", Operation: "recovery", Key: scan.GenerateID()}}}
	for _, mutation := range []string{"none", "missing baseline", "wrong run", "wrong epoch", "gap", "truncated", "cursor regressed", "missing key", "unsupported version"} {
		t.Run(mutation, func(t *testing.T) {
			var s JournalSnapshot
			_ = json.Unmarshal(mustJSON(valid), &s)
			start := baseline
			ptr := &start
			switch mutation {
			case "missing baseline":
				ptr = nil
			case "wrong run":
				s.RunID = "other"
			case "wrong epoch":
				s.BootID = scan.GenerateID()
			case "gap":
				s.Entries[0].Sequence = 2
			case "truncated":
				s.Head = 2
			case "cursor regressed":
				start.Head = 2
			case "missing key":
				s.Entries[0].Key = ""
			case "unsupported version":
				s.Version = 2
			}
			err := verifyJournalContinuity(ptr, s)
			if (err == nil) != (mutation == "none") {
				t.Fatalf("continuity err=%v", err)
			}
		})
	}
}

func recoveryCase(t *testing.T, name string) (RecoveryCanary, PreparedCanary, CanaryReceipt, JournalSnapshot, JournalCursor) {
	t.Helper()
	cfg := Config{RunID: "run", Tenants: []string{scan.GenerateID()}}
	f := FaultSpec{Name: name, Offset: time.Minute}
	c := RecoveryCanary{Fault: f, Stage: "fresh", Nonce: scan.GenerateID()}
	c.Definition = recoveryDefinition(cfg, f, c.Stage, c.Nonce, "curl@sha256:source")
	b := PreparedCanary{CaseID: c.Definition.CaseID, Tenant: c.Definition.Tenant, Namespace: TenantNamespace(c.Definition.Tenant), SourceUID: scan.GenerateID(), RevisionID: 7}
	trigger := TriggerResponse{Namespace: b.Namespace, Name: jobRunCRName(b.CaseID, c.Nonce), OccurrenceKey: c.Nonce, Trigger: "Manual"}
	now := time.Now().UTC()
	r := RecoveryRun{ID: 5, Tenant: b.Tenant, Revision: b.RevisionID, SourceUID: b.SourceUID, Occurrence: c.Nonce, Phase: phaseSucceeded, Trigger: "Manual", CreatedAt: now, CRUID: scan.GenerateID(), OwnedJobUID: scan.GenerateID(), JobSucceeded: true}
	r.Attempts = []RecoveryAttempt{{Number: 1, Phase: phaseSucceeded, JobName: "attempt", JobUID: r.OwnedJobUID, OwnerUID: r.CRUID}}
	x := CanaryReceipt{CaseID: b.CaseID, Trigger: trigger, SubmittedAfter: now.Add(-time.Second), Observed: r}
	start := JournalCursor{RunID: cfg.RunID, BootID: scan.GenerateID()}
	journal := JournalSnapshot{Version: 1, RunID: start.RunID, BootID: start.BootID, Head: 1, Entries: []JournalEffect{{Sequence: 1, CaseID: b.CaseID, Operation: "recovery", Key: c.Nonce}}}
	return c, b, x, journal, start
}

func TestRecoveryCanaryRequiresIndependentExactFacts(t *testing.T) {
	for _, mutation := range []string{"none", "wrong tenant", "wrong source", "wrong revision", "wrong occurrence", "wrong Job UID", "missing original owner", "stale observation", "no observation", "journal missing marker", "journal duplicate", "foreign journal", "wrong terminal", "extra attempt"} {
		t.Run(mutation, func(t *testing.T) {
			c, b, x, journal, start := recoveryCase(t, "operator")
			var current RecoveryRun
			_ = json.Unmarshal(mustJSON(x.Observed), &current)
			want := VerdictInconclusive
			switch mutation {
			case "none":
				want = VerdictPass
			case "wrong tenant":
				current.Tenant = scan.GenerateID()
			case "wrong source":
				current.SourceUID = scan.GenerateID()
			case "wrong revision":
				current.Revision++
			case "wrong occurrence":
				current.Occurrence = scan.GenerateID()
			case "wrong Job UID":
				current.OwnedJobUID = scan.GenerateID()
			case "missing original owner":
				current.Attempts[0].OwnerUID = ""
			case "stale observation":
				x.SubmittedAfter = x.Observed.CreatedAt.Add(time.Second)
			case "no observation":
				x.Observed = RecoveryRun{}
			case "journal missing marker":
				journal.Entries = nil
				journal.Head = 0
			case "journal duplicate":
				extra := journal.Entries[0]
				extra.Sequence = 2
				journal.Entries = append(journal.Entries, extra)
				journal.Head = 2
				want = VerdictFail
			case "foreign journal":
				journal.Entries[0].CaseID = "other"
			case "wrong terminal":
				current.Phase = phaseFailed
				want = VerdictFail
			case "extra attempt":
				current.Attempts = append(current.Attempts, current.Attempts[0])
				want = VerdictFail
			}
			status, message := evaluateRecoveryCanary(c, b, x, current, nil, journal, start)
			if status != want {
				t.Fatalf("status=%s want=%s: %s", status, want, message)
			}
		})
	}
}

func TestSchedulerRecoveryNeedsActualFreshSLOEvaluation(t *testing.T) {
	for _, mutation := range []string{"none", "missing evaluation", "stale evaluation", "wrong source", "wrong SLO", "no event", "no samples", "incorrect result", "saved evaluation missing"} {
		t.Run(mutation, func(t *testing.T) {
			c, b, x, journal, start := recoveryCase(t, "scheduler")
			b.SLIID, b.SLOID = 8, 9
			budget := RecoveryBudget{SLOID: b.SLOID, SLIID: b.SLIID, SourceUID: b.SourceUID, Version: 1, EventAt: x.Observed.CreatedAt.Add(time.Second), EvaluatedAt: x.Observed.CreatedAt.Add(2 * time.Second), Good: 1, Total: 1, Remaining: 0.01, Status: "healthy"}
			x.Budget = &budget
			var fresh RecoveryBudget
			_ = json.Unmarshal(mustJSON(budget), &fresh)
			ptr := &fresh
			switch mutation {
			case "missing evaluation":
				ptr = nil
			case "stale evaluation":
				fresh.EvaluatedAt = x.SubmittedAfter.Add(-time.Second)
			case "wrong source":
				fresh.SourceUID = scan.GenerateID()
			case "wrong SLO":
				fresh.SLOID++
			case "no event":
				fresh.EventAt = time.Time{}
			case "no samples":
				fresh.Total = 0
			case "incorrect result":
				fresh.Remaining = 1
			case "saved evaluation missing":
				x.Budget = nil
			}
			status, message := evaluateRecoveryCanary(c, b, x, x.Observed, ptr, journal, start)
			if (status == VerdictPass) != (mutation == "none") {
				t.Fatalf("status=%s: %s", status, message)
			}
		})
	}
}

func TestPostgresRecoveryPreservesOriginalInFlightJob(t *testing.T) {
	c, b, x, journal, start := recoveryCase(t, "postgres")
	c.Stage = "pending"
	before := x.Observed
	before.Phase = "Running"
	before.JobSucceeded = false
	x.BeforeFault = &before
	if status, message := evaluateRecoveryCanary(c, b, x, x.Observed, nil, journal, start); status != VerdictPass {
		t.Fatal(status, message)
	}
	before.OwnedJobUID = scan.GenerateID()
	if status, _ := evaluateRecoveryCanary(c, b, x, x.Observed, nil, journal, start); status == VerdictPass {
		t.Fatal("replacement Job proved original in-flight recovery")
	}
}

func TestRecoveryWaitStopsOnCancellationAndSourceError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := waitRecoveryObservation(ctx, time.Hour, func(context.Context) (bool, error) { calls++; return true, nil })
	if err != context.Canceled || calls != 0 {
		t.Fatal("canceled probe performed a new observation", err, calls)
	}
	start := time.Now()
	err = waitRecoveryObservation(context.Background(), 10*time.Millisecond, func(context.Context) (bool, error) { return false, nil })
	if err != context.DeadlineExceeded || time.Since(start) > time.Second {
		t.Fatal("wall-clock budget ignored", err)
	}
	sourceError := errors.New("source unavailable")
	if err := waitRecoveryObservation(context.Background(), time.Hour, func(context.Context) (bool, error) { return false, sourceError }); !errors.Is(err, sourceError) {
		t.Fatal("source error was hidden", err)
	}
}

func TestShippedRecoveryEffectHoldsUntilExplicitRelease(t *testing.T) {
	base := startJournalFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	response := make(chan int, 1)
	go func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/recovery/effect", strings.NewReader(`{"case_id":"pending-canary","operation":"recovery","idempotency_key":"original-effect","hold":true}`))
		if err != nil {
			response <- 0
			return
		}
		req.Header.Set("Content-Type", "application/json")
		result, err := http.DefaultClient.Do(req)
		if err != nil {
			response <- 0
			return
		}
		_ = result.Body.Close()
		response <- result.StatusCode
	}()
	for {
		journal, err := readJournalSnapshot(ctx, base, "held-run")
		if err != nil {
			t.Fatal(err)
		}
		if journal.Head == 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("held effect never appeared")
		case <-time.After(time.Millisecond):
		}
	}
	select {
	case code := <-response:
		t.Fatal("canary completed before database fault release", code)
	case <-time.After(20 * time.Millisecond):
	}
	prober := RecoveryProber{FixtureURL: base}
	if err := prober.release(ctx, "pending-canary"); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-response:
		if code != http.StatusCreated {
			t.Fatal("held effect did not complete", code)
		}
	case <-ctx.Done():
		t.Fatal("release did not unblock workload")
	}
}

func TestTargetRecoveryRequiresNewGenerationAndOriginalPodsGone(t *testing.T) {
	before := RecoveryTarget{UID: scan.GenerateID(), Generation: 3, PodUIDs: []string{scan.GenerateID()}}
	after := RecoveryTarget{UID: before.UID, Generation: 4, ObservedGeneration: 4, Desired: 1, Ready: 1, Updated: 1, PodUIDs: []string{scan.GenerateID()}}
	if !targetReplaced(before, after) {
		t.Fatal("complete replacement rejected")
	}
	after.PodUIDs = before.PodUIDs
	if targetReplaced(before, after) {
		t.Fatal("old ready Pod proved replacement")
	}
	after.PodUIDs = []string{scan.GenerateID()}
	after.ObservedGeneration = 3
	if targetReplaced(before, after) {
		t.Fatal("unobserved generation proved replacement")
	}
}

func TestNormalScheduleCompletionDrainsFaultUntilUserCancellation(t *testing.T) {
	down, restored := fakeFaultKubectl(t)
	engine := NewRunEngine(nil, PhaseSchedule{Duration: time.Minute}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type outcome struct {
		records []FaultRecord
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		records, err := runEngineWithFaults(ctx, engine, func() time.Duration {
			if _, err := os.Stat(down); err == nil {
				return time.Minute
			}
			return 0
		}, []FaultSpec{{Name: "postgres", DisconnectFor: time.Hour}})
		done <- outcome{records, err}
	}()
	for engine.VirtualElapsed() != time.Minute {
		select {
		case <-ctx.Done():
			t.Fatal("schedule did not finish")
		case <-time.After(time.Millisecond):
		}
	}
	select {
	case result := <-done:
		t.Fatalf("normal completion canceled in-flight fault: %+v", result)
	case <-time.After(20 * time.Millisecond):
	}
	if _, err := os.Stat(restored); !os.IsNotExist(err) {
		t.Fatal("normal completion prematurely canceled the disconnect")
	}
	cancel()
	select {
	case result := <-done:
		if len(result.records) != 1 || result.records[0].InjectError == "" {
			t.Fatal("cancellation was not recorded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled run failed to drain")
	}
	if _, err := os.Stat(restored); err != nil {
		t.Fatal("cancellation did not restore database")
	}
}
