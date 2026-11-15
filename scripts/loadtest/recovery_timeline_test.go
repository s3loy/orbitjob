package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"orbitjob/internal/platform/scan"
)

func timelineFixture(t *testing.T) (JournalSnapshot, RecoveryManifest, FaultRecord, RecoveryCanary, CanaryReceipt, RecoveryRun, time.Time, time.Time) {
	t.Helper()
	c, _, x, j, start := recoveryCase(t, "operator")
	j.Entries = nil
	j.Head = 0
	now := time.Now().UTC().Truncate(time.Millisecond)
	before := RecoveryTarget{UID: scan.GenerateID(), Generation: 1, PodUIDs: []string{scan.GenerateID()}}
	after := RecoveryTarget{UID: before.UID, Generation: 2, ObservedGeneration: 2, Desired: 1, Ready: 1, Updated: 1, PodUIDs: []string{scan.GenerateID()}}
	appendEntry := func(e JournalEffect) {
		e.Sequence = int64(len(j.Entries) + 1)
		j.Entries = append(j.Entries, e)
		j.Head = e.Sequence
	}
	checkpoint := func(phase string, at time.Time, target *RecoveryTarget, run *RecoveryRun) {
		v := &RecoveryCheckpoint{RunID: j.RunID, Fault: c.Fault, Nonce: c.Nonce, Phase: phase, DatabaseTime: at.Add(-time.Millisecond), Target: target, Run: run, HostTime: at, ElapsedNanoseconds: int64(at.Sub(now) + time.Second)}
		if phase == "restored" {
			v.DatabaseTime = time.Time{}
		}
		entry := JournalEffect{CaseID: c.Definition.CaseID, Operation: "recovery-checkpoint", Key: c.Nonce + "/" + phase, ObservedAt: at, Checkpoint: v}
		appendEntry(entry)
		if phase != "restored" {
			appendEntry(JournalEffect{CaseID: c.Definition.CaseID, Operation: "recovery-checkpoint", Key: c.Nonce + "/" + phase + "/clock", ObservedAt: at.Add(2 * time.Millisecond), Checkpoint: &RecoveryCheckpoint{RunID: j.RunID, Fault: c.Fault, Nonce: c.Nonce, Phase: phase + "/clock", DatabaseTime: at.Add(time.Millisecond), ReferenceSequence: j.Head, RoundTripNanoseconds: int64(2 * time.Millisecond), HostTime: at.Add(2 * time.Millisecond), ElapsedNanoseconds: v.ElapsedNanoseconds + int64(2*time.Millisecond)}})
		}
	}
	checkpoint("fault", now, &before, nil)
	checkpoint("restored", now.Add(time.Second), nil, nil)
	checkpoint("ready", now.Add(2*time.Second), &after, nil)
	checkpoint("submit", now.Add(3*time.Second), nil, nil)
	x.SubmittedAfter = now.Add(3*time.Second - time.Millisecond)
	x.Observed.CreatedAt = now.Add(3*time.Second + 10*time.Millisecond)
	completed := now.Add(3*time.Second + 30*time.Millisecond)
	x.Observed.Attempts[0].CompletedAt = &completed
	x.Observed.TerminalObservedAt = &completed
	appendEntry(JournalEffect{CaseID: c.Definition.CaseID, Operation: "recovery", Key: c.Nonce, ObservedAt: now.Add(3*time.Second + 20*time.Millisecond)})
	checkpoint("observed", now.Add(4*time.Second), nil, &x.Observed)
	r := &FaultRecoveryReceipt{Before: before, After: after, JournalStart: start, JournalEnd: j.Cursor(), Canaries: []CanaryReceipt{x}}
	record := FaultRecord{Name: c.Fault.Name, ScheduledOffset: c.Fault.Offset, InjectedAt: now.Add(100 * time.Millisecond).Format(time.RFC3339Nano), Recovery: r}
	m := RecoveryManifest{Version: 1, RunID: j.RunID, Canaries: []RecoveryCanary{c}}
	return j, m, record, c, x, x.Observed, now.Add(-time.Second), now.Add(5 * time.Second)
}

func TestIndependentTimelineRejectsOldSuccessAndEditedStats(t *testing.T) {
	for _, negative := range []string{"none", "success before fault", "forged submission time", "fault after success", "missing checkpoint", "duplicate checkpoint", "wrong nonce", "wrong run", "cross clock", "reordered checkpoints", "outside run"} {
		t.Run(negative, func(t *testing.T) {
			j, m, record, c, x, current, start, end := timelineFixture(t)
			switch negative {
			case "success before fault":
				current.CreatedAt = start
				x.Observed.CreatedAt = start
				for i := range j.Entries {
					if j.Entries[i].Checkpoint != nil && j.Entries[i].Checkpoint.Phase == "observed" {
						copy := current
						j.Entries[i].Checkpoint.Run = &copy
					}
				}
			case "forged submission time":
				x.SubmittedAfter = current.CreatedAt.Add(-time.Millisecond)
			case "fault after success":
				record.InjectedAt = end.Format(time.RFC3339Nano)
			case "missing checkpoint":
				for i := range j.Entries {
					if j.Entries[i].Checkpoint != nil && j.Entries[i].Checkpoint.Phase == "ready" {
						j.Entries[i].Checkpoint = nil
					}
				}
			case "duplicate checkpoint":
				var entry JournalEffect
				_ = json.Unmarshal(mustJSON(j.Entries[0]), &entry)
				entry.Sequence = j.Head + 1
				j.Entries = append(j.Entries, entry)
				j.Head++
			case "wrong nonce":
				j.Entries[0].Checkpoint.Nonce = scan.GenerateID()
			case "wrong run":
				j.Entries[0].Checkpoint.RunID = "other"
			case "cross clock":
				j.Entries[1].Checkpoint.DatabaseTime = j.Entries[0].ObservedAt.Add(-time.Second)
			case "reordered checkpoints":
				j.Entries[0].Sequence = j.Head
			case "outside run":
				end = start.Add(time.Millisecond)
			}
			err := recoveryTimelineCheck(j, m, record, c, x, current, start, end)
			if (err == nil) != (negative == "none") {
				t.Fatalf("timeline error=%v", err)
			}
		})
	}
}

func TestTimelineUsesCausalBoundsAcrossIndependentUTCClocks(t *testing.T) {
	j, m, record, c, x, current, start, end := timelineFixture(t)
	// A stable offset in the fixture's display clock cannot move source facts
	// across SQL-clock bounds or the independent journal sequence.
	for i := range j.Entries {
		j.Entries[i].ObservedAt = j.Entries[i].ObservedAt.Add(30 * time.Second)
	}
	if err := recoveryTimelineCheck(j, m, record, c, x, current, start, end); err != nil {
		t.Fatal(err)
	}
	// DB samples are microsecond precision. Only the corresponding rounding
	// interval is allowed when comparing them with a monotonic duration.
	j.Entries[1].Checkpoint.RoundTripNanoseconds -= 500
	if err := recoveryTimelineCheck(j, m, record, c, x, current, start, end); err != nil {
		t.Fatal("microsecond quantization rejected", err)
	}
	j.Entries[1].Checkpoint.RoundTripNanoseconds -= 2000
	if err := recoveryTimelineCheck(j, m, record, c, x, current, start, end); err == nil {
		t.Fatal("unbounded DB clock step passed as rounding")
	}
}

func TestShippedFixturePreservesDuplicateCheckpointEvidence(t *testing.T) {
	base := startJournalFixture(t, false)
	c, _, _, _, _ := recoveryCase(t, "operator")
	prober := RecoveryProber{Manifest: RecoveryManifest{RunID: "run", Canaries: []RecoveryCanary{c}}, FixtureURL: base}
	value := RecoveryCheckpoint{RunID: "run", Fault: c.Fault, Nonce: c.Nonce, Phase: "restored", HostTime: time.Now(), ElapsedNanoseconds: 1}
	for i := 0; i < 2; i++ {
		if _, err := prober.postCheckpoint(context.Background(), 0, value); err != nil {
			t.Fatal(err)
		}
	}
	j, err := readJournalSnapshot(context.Background(), base, "run")
	if err != nil {
		t.Fatal(err)
	}
	if j.Head != 2 {
		t.Fatal("checkpoint dedupe hid a repeated stage")
	}
	if _, err := findCheckpoint(j, c, "restored"); err == nil {
		t.Fatal("duplicate independent checkpoints accepted")
	}
}
