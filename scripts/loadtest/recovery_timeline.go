package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Checkpoints preserve source snapshots in the independent append-only journal.
// The fixture assigns the sequence and UTC timestamp; stats contain no authority
// to move a source observation into a different fault's recovery window.
type RecoveryCheckpoint struct {
	RunID                string          `json:"run_id"`
	Fault                FaultSpec       `json:"fault"`
	Nonce                string          `json:"nonce"`
	Phase                string          `json:"phase"`
	DatabaseTime         time.Time       `json:"database_time"`
	HostTime             time.Time       `json:"host_time"`
	ElapsedNanoseconds   int64           `json:"elapsed_ns"`
	ReferenceSequence    int64           `json:"reference_sequence,omitempty"`
	RoundTripNanoseconds int64           `json:"round_trip_ns,omitempty"`
	Target               *RecoveryTarget `json:"target,omitempty"`
	Run                  *RecoveryRun    `json:"run,omitempty"`
	Budget               *RecoveryBudget `json:"budget,omitempty"`
}

func (p *RecoveryProber) freshIndex(f FaultSpec) int {
	for _, i := range p.canaries(f) {
		if p.Manifest.Canaries[i].Stage == "fresh" {
			return i
		}
	}
	return -1
}

func (p *RecoveryProber) postCheckpoint(ctx context.Context, i int, value RecoveryCheckpoint) (JournalEffect, error) {
	var out JournalEffect
	c := p.Manifest.Canaries[i]
	body := map[string]any{"case_id": c.Definition.CaseID, "idempotency_key": c.Nonce + "/" + value.Phase, "checkpoint": value}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.FixtureURL+"/recovery/checkpoint", bytes.NewReader(mustJSON(body)))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return out, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		return out, fmt.Errorf("recovery checkpoint HTTP %d", response.StatusCode)
	}
	if err = json.NewDecoder(response.Body).Decode(&out); err != nil {
		return out, err
	}
	if out.Sequence <= 0 || out.ObservedAt.IsZero() || out.Checkpoint == nil || recoveryDigest(*out.Checkpoint) != recoveryDigest(value) {
		return out, fmt.Errorf("checkpoint response lacks immutable source snapshot")
	}
	return out, nil
}

func (p *RecoveryProber) checkpoint(ctx context.Context, i int, phase string, target *RecoveryTarget, run *RecoveryRun, budget *RecoveryBudget) (RecoveryCheckpoint, error) {
	if i < 0 || i >= len(p.Manifest.Canaries) {
		return RecoveryCheckpoint{}, fmt.Errorf("checkpoint has no planned canary")
	}
	c := p.Manifest.Canaries[i]
	if p.monotonicStart.IsZero() {
		p.monotonicStart = time.Now()
	}
	value := RecoveryCheckpoint{RunID: p.Manifest.RunID, Fault: c.Fault, Nonce: c.Nonce, Phase: phase, Target: target, Run: run, Budget: budget, HostTime: time.Now(), ElapsedNanoseconds: int64(time.Since(p.monotonicStart))}
	// These two native observations deliberately occur while SQL is unavailable.
	if phase == "down" || phase == "native-complete" || phase == "restored" {
		_, err := p.postCheckpoint(ctx, i, value)
		return value, err
	}
	start := time.Now()
	if err := p.DB.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&value.DatabaseTime); err != nil {
		return value, err
	}
	entry, err := p.postCheckpoint(ctx, i, value)
	if err != nil {
		return value, err
	}
	var after time.Time
	if err = p.DB.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&after); err != nil {
		return value, err
	}
	elapsed := time.Since(start)
	// SQL samples bracket this synchronous fixture write causally. UTC clocks
	// need not agree. Compare SQL facts only with these same-DB bounds and
	// reject DB clock discontinuity beyond PostgreSQL's microsecond precision.
	if after.Before(value.DatabaseTime) || after.Sub(value.DatabaseTime) > elapsed+time.Microsecond || elapsed > 10*time.Second {
		return value, fmt.Errorf("database clock bracket is inconsistent")
	}
	confirmation := RecoveryCheckpoint{RunID: p.Manifest.RunID, Fault: c.Fault, Nonce: c.Nonce, Phase: phase + "/clock", DatabaseTime: after, ReferenceSequence: entry.Sequence, RoundTripNanoseconds: int64(elapsed), HostTime: time.Now(), ElapsedNanoseconds: int64(time.Since(p.monotonicStart))}
	_, err = p.postCheckpoint(ctx, i, confirmation)
	return value, err
}

func findCheckpoint(j JournalSnapshot, c RecoveryCanary, phase string) (JournalEffect, error) {
	var result JournalEffect
	count := 0
	for _, e := range j.Entries {
		if e.CaseID == c.Definition.CaseID && e.Key == c.Nonce+"/"+phase && e.Operation == "recovery-checkpoint" {
			count++
			result = e
		}
	}
	if count != 1 || result.ObservedAt.IsZero() || result.Checkpoint == nil {
		return result, fmt.Errorf("checkpoint %s missing or duplicated", phase)
	}
	v := result.Checkpoint
	if v.RunID != j.RunID || v.Fault != c.Fault || v.Nonce != c.Nonce || v.Phase != phase {
		return result, fmt.Errorf("checkpoint is bound to another run/fault/nonce")
	}
	if phase == "down" || phase == "native-complete" || phase == "restored" {
		return result, nil
	}
	var confirmation *JournalEffect
	count = 0
	for k := range j.Entries {
		e := &j.Entries[k]
		if e.CaseID == c.Definition.CaseID && e.Key == c.Nonce+"/"+phase+"/clock" && e.Operation == "recovery-checkpoint" {
			count++
			confirmation = e
		}
	}
	if count != 1 || confirmation.Checkpoint == nil {
		return result, fmt.Errorf("checkpoint clock bracket missing or duplicated")
	}
	clock := confirmation.Checkpoint
	if clock.RunID != v.RunID || clock.Fault != v.Fault || clock.Nonce != v.Nonce || clock.Phase != phase+"/clock" || clock.ReferenceSequence != result.Sequence || confirmation.Sequence <= result.Sequence || v.DatabaseTime.IsZero() || clock.DatabaseTime.Before(v.DatabaseTime) || clock.RoundTripNanoseconds <= 0 || clock.RoundTripNanoseconds > int64(10*time.Second) || clock.DatabaseTime.Sub(v.DatabaseTime) > time.Duration(clock.RoundTripNanoseconds)+time.Microsecond || clock.ElapsedNanoseconds <= v.ElapsedNanoseconds || v.HostTime.IsZero() || clock.HostTime.Before(v.HostTime) {
		return result, fmt.Errorf("independent checkpoint clock bracket inconsistent")
	}
	return result, nil
}

func checkpointAfter(j JournalSnapshot, entry JournalEffect) time.Time {
	for _, e := range j.Entries {
		if e.Checkpoint != nil && e.Checkpoint.ReferenceSequence == entry.Sequence && e.Key == entry.Key+"/clock" {
			return e.Checkpoint.DatabaseTime
		}
	}
	return time.Time{}
}

func recoveryTimelineCheck(j JournalSnapshot, m RecoveryManifest, record FaultRecord, c RecoveryCanary, x CanaryReceipt, current RecoveryRun, runStart, runEnd time.Time) error {
	r := record.Recovery
	if r == nil {
		return fmt.Errorf("recovery observations missing")
	}
	var lastDB time.Time
	var lastElapsed int64
	for _, entry := range j.Entries {
		if entry.Sequence <= r.JournalStart.Head || entry.Sequence > r.JournalEnd.Head || entry.Checkpoint == nil || entry.Checkpoint.Fault != c.Fault {
			continue
		}
		v := entry.Checkpoint
		if v.ElapsedNanoseconds <= lastElapsed {
			return fmt.Errorf("checkpoint monotonic clock regressed")
		}
		lastElapsed = v.ElapsedNanoseconds
		if !v.DatabaseTime.IsZero() {
			if v.DatabaseTime.Before(lastDB) {
				return fmt.Errorf("checkpoint database clock regressed")
			}
			lastDB = v.DatabaseTime
		}
	}
	var fresh RecoveryCanary
	for _, candidate := range m.Canaries {
		if candidate.Fault == c.Fault && candidate.Stage == "fresh" {
			fresh = candidate
		}
	}
	fault, err := findCheckpoint(j, fresh, "fault")
	if err != nil {
		return err
	}
	restored, err := findCheckpoint(j, fresh, "restored")
	if err != nil {
		return err
	}
	ready, err := findCheckpoint(j, fresh, "ready")
	if err != nil {
		return err
	}
	submit, err := findCheckpoint(j, c, "submit")
	if err != nil {
		return err
	}
	observed, err := findCheckpoint(j, c, "observed")
	if err != nil {
		return err
	}
	injectedAt, err := time.Parse(time.RFC3339Nano, record.InjectedAt)
	if err != nil {
		return fmt.Errorf("fault injection timestamp invalid")
	}
	if fault.Checkpoint.HostTime.Before(runStart) || observed.Checkpoint.HostTime.After(runEnd) || observed.Sequence > r.JournalEnd.Head {
		return fmt.Errorf("recovery checkpoints lie outside the executed run interval")
	}
	if fault.Sequence <= r.JournalStart.Head || fault.Sequence >= restored.Sequence || restored.Sequence >= ready.Sequence || ready.Sequence >= observed.Sequence || injectedAt.Before(fault.Checkpoint.HostTime) || injectedAt.After(restored.Checkpoint.HostTime) || fault.Checkpoint.Target == nil || ready.Checkpoint.Target == nil || recoveryDigest(*fault.Checkpoint.Target) != recoveryDigest(r.Before) || recoveryDigest(*ready.Checkpoint.Target) != recoveryDigest(r.After) {
		return fmt.Errorf("fault, restoration, readiness and source identity order is inconsistent")
	}
	gate := recoveryGate(c.Fault.Name)
	if ready.Checkpoint.ElapsedNanoseconds <= restored.Checkpoint.ElapsedNanoseconds || observed.Checkpoint.ElapsedNanoseconds <= ready.Checkpoint.ElapsedNanoseconds || time.Duration(ready.Checkpoint.ElapsedNanoseconds-restored.Checkpoint.ElapsedNanoseconds) > gate.ReadyTimeout || time.Duration(observed.Checkpoint.ElapsedNanoseconds-ready.Checkpoint.ElapsedNanoseconds) > gate.BusinessTimeout || observed.Checkpoint.DatabaseTime.Sub(checkpointAfter(j, ready)) > gate.BusinessTimeout {
		return fmt.Errorf("source observations exceed the component recovery deadline")
	}
	if !x.SubmittedAfter.Equal(submit.Checkpoint.DatabaseTime) || current.CreatedAt.Before(checkpointAfter(j, submit)) || observed.Checkpoint.Run == nil || recoveryDigest(*observed.Checkpoint.Run) != recoveryDigest(x.Observed) || recoveryDigest(observed.Checkpoint.Budget) != recoveryDigest(x.Budget) || len(current.Attempts) != 1 || current.TerminalObservedAt == nil || current.TerminalObservedAt.After(observed.Checkpoint.DatabaseTime) || current.TerminalObservedAt.Before(current.CreatedAt) {
		return fmt.Errorf("canary source timestamps/snapshot do not match independent checkpoints")
	}
	var effect JournalEffect
	count := 0
	for _, e := range j.Entries {
		if e.CaseID == c.Definition.CaseID && e.Key == c.Nonce && e.Operation == "recovery" {
			effect = e
			count++
		}
	}
	if count != 1 || effect.ObservedAt.IsZero() || effect.Sequence <= submit.Sequence || effect.Sequence >= observed.Sequence {
		return fmt.Errorf("effect is outside its independently observed submission/completion interval")
	}
	if c.Stage == "fresh" {
		if submit.Sequence <= ready.Sequence || current.CreatedAt.Before(checkpointAfter(j, ready)) || current.TerminalObservedAt.Before(checkpointAfter(j, ready)) {
			return fmt.Errorf("fresh canary predates recovery readiness")
		}
	} else {
		inflight, err := findCheckpoint(j, c, "in-flight")
		if err != nil {
			return err
		}
		down, err := findCheckpoint(j, fresh, "down")
		if err != nil {
			return err
		}
		native, err := findCheckpoint(j, c, "native-complete")
		if err != nil {
			return err
		}
		if submit.Sequence >= effect.Sequence || effect.Sequence >= inflight.Sequence || inflight.Sequence >= fault.Sequence || fault.Sequence >= down.Sequence || down.Sequence >= native.Sequence || native.Sequence >= restored.Sequence || inflight.Checkpoint.Run == nil || x.BeforeFault == nil || recoveryDigest(*inflight.Checkpoint.Run) != recoveryDigest(*x.BeforeFault) || native.Checkpoint.Run == nil || !native.Checkpoint.Run.JobSucceeded || native.Checkpoint.Run.OwnedJobUID != current.OwnedJobUID || down.Checkpoint.Target == nil || down.Checkpoint.Target.UID != r.Before.UID || down.Checkpoint.Target.Desired != 0 || down.Checkpoint.Target.Ready != 0 || len(down.Checkpoint.Target.PodUIDs) != 0 {
			return fmt.Errorf("in-flight execution is not bound to the observed database outage and original Job")
		}
	}
	return nil
}
