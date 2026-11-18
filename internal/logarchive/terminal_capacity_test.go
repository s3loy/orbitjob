package logarchive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	api "orbitjob/api/kubernetes/workloads/v1alpha1"
)

func TestObserverSealsAndReclaimsArchiveWithFullReplayManifest(t *testing.T) {
	now := time.Date(2026, 11, 19, 1, 0, 0, 0, time.UTC)
	finished := now.Add(-time.Minute)
	limits := testLimits(&now)
	dir := t.TempDir()
	s, err := Open(dir, limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	startArchive := func() {
		t.Helper()
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		s, err = Open(dir, limits)
		if err != nil {
			t.Fatal(err)
		}
	}
	id := identity(510)
	id.SourceName = "job"
	id.CreatedAt = now.Add(-10 * time.Minute)
	run := &api.JobRun{
		ObjectMeta: metav1.ObjectMeta{Name: id.RunName, Namespace: id.Namespace, UID: types.UID(id.RunUID), CreationTimestamp: metav1.NewTime(id.CreatedAt)},
		Spec: api.JobRunSpec{
			ScheduledJobRef:    api.ObjectReference{Name: id.SourceName, UID: id.SourceUID},
			DefinitionRevision: id.RevisionID,
			OccurrenceKey:      id.OccurrenceKey,
		},
	}
	run.Status.Attempt = 1
	if err = s.Register(id); err != nil {
		t.Fatal(err)
	}
	if err = s.Append(id, runEvents(id, run, "Running")); err != nil {
		t.Fatal(err)
	}
	output := func(first, count int) []Record {
		records := make([]Record, count)
		for i := range records {
			records[i] = Record{ID: fmt.Sprintf("%032x", first+i), Kind: "stdout", Attempt: 1, Text: "ordinary output\n"}
		}
		return records
	}
	for i := 0; i < 7000; i += 1000 {
		if err = s.Append(id, output(i, 1000)); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Append(id, output(7000, 400)); err != nil {
		t.Fatal(err)
	}
	for i := 7400; i < 8000; i++ {
		if err = s.Append(id, output(i, 1)); err != nil {
			break
		}
	}
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("fixture did not exhaust the default replay manifest: %v", err)
	}
	before, err := s.Snapshot(id)
	if err != nil || before.CompletedAt != nil || before.State != "partial" {
		t.Fatalf("unexpected archive state before terminal observation: %s %v", before.State, err)
	}
	if err = s.Append(id, runEvents(id, run, "Succeeded")); !errors.Is(err, ErrCapacity) {
		t.Fatalf("terminal event unexpectedly fit in full manifest: %v", err)
	}

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	observe := func(protection *time.Time) {
		t.Helper()
		mock.ExpectBegin()
		mock.ExpectExec("SELECT set_config").WithArgs(id.TenantID).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT COALESCE").WithArgs(id.TenantID, id.RevisionID, id.OccurrenceKey, id.SourceUID, id.Namespace, id.SourceName).
			WillReturnRows(sqlmock.NewRows([]string{"id", "workflow_id", "phase", "finished"}).AddRow(id.RunID, 0, "Succeeded", finished))
		mock.ExpectCommit()
		mock.ExpectBegin()
		mock.ExpectExec("SELECT set_config").WithArgs(id.TenantID).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO execution_log_archives").
			WithArgs(id.TenantID, id.Key(), id.RunID, id.FunctionID, nil, sqlmock.AnyArg(), "partial", protection, finished).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		mock.ExpectBegin()
		mock.ExpectExec("SELECT set_config").WithArgs(id.TenantID).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery("SELECT cleanup_requested_at IS NOT NULL").WithArgs(id.TenantID, id.Key()).
			WillReturnRows(sqlmock.NewRows([]string{"cleanup_requested"}).AddRow(false))
		mock.ExpectCommit()
		o := &Observer{Store: s, Repository: Repository{DB: db}, Namespaces: map[string]string{id.Namespace: id.TenantID}}
		if _, err := o.register(t.Context(), run); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	}
	observe(nil)
	startArchive()
	completed, err := s.Snapshot(id)
	if err != nil || completed.Outcome != "Succeeded" || completed.CompletedAt == nil || !completed.CompletedAt.Equal(finished) || completed.SealedAt != nil {
		t.Fatalf("completion was lost across restart: outcome=%q completed=%v sealed=%v error=%v", completed.Outcome, completed.CompletedAt, completed.SealedAt, err)
	}
	now = now.Add(4 * time.Second)
	until := now.Add(Retention)
	observe(&until)
	startArchive()
	sealed, err := s.Snapshot(id)
	if err != nil || sealed.SealedAt == nil || !sealed.SealedAt.Equal(now) || sealed.ProtectedUntil == nil || !sealed.ProtectedUntil.Equal(until) || sealed.State != "partial" || len(sealed.Gaps) == 0 {
		t.Fatalf("seal or loss was lost across restart: seal=%v protection=%v state=%q gaps=%v error=%v", sealed.SealedAt, sealed.ProtectedUntil, sealed.State, sealed.Gaps, err)
	}
	if err = s.Append(id, output(6999, 5)); err != nil {
		t.Fatalf("regrouped replay into a full manifest was rejected: %v", err)
	}
	after, err := s.Snapshot(id)
	if err != nil || after.OutputBytes != before.OutputBytes || after.RetainedBytes != before.RetainedBytes || after.Next != before.Next || len(after.Seen) != len(before.Seen) {
		t.Fatal("terminal fallback or replay changed admitted evidence")
	}
	now = until.Add(-time.Second)
	if n, err := s.Sweep(); err != nil || n != 0 {
		t.Fatalf("reclaimed protected archive: %d, %v", n, err)
	}
	now = until
	if n, err := s.Sweep(); err != nil || n != 1 {
		t.Fatalf("did not reclaim expired archive: %d, %v", n, err)
	}
	startArchive()
	p, err := s.Read(id, Query{})
	if err != nil || p.State != "expired" || p.Outcome != "Succeeded" || p.RetainedBytes != 0 || len(p.Records) != 0 {
		t.Fatalf("unexpected expired summary: %+v, %v", p, err)
	}
}

func TestCompletionDoesNotFallBackOnFilesystemError(t *testing.T) {
	now := time.Now()
	s := openTest(t, &now)
	id := identity(511)
	if err := s.Register(id); err != nil {
		t.Fatal(err)
	}
	statfsErr := errors.New("statfs failed")
	s.statfs = func(string, *syscall.Statfs_t) error { return statfsErr }
	if err := s.Complete(id, "Succeeded", now); !errors.Is(err, statfsErr) {
		t.Fatalf("filesystem failure treated as ordinary capacity: %v", err)
	}
	m, err := s.Snapshot(id)
	if err != nil || m.CompletedAt != nil {
		t.Fatalf("completion was published despite filesystem failure: %v, %v", m.CompletedAt, err)
	}
}

func TestTornSealControlRecordKeepsCompletionWithoutAuthorizingExpiry(t *testing.T) {
	now := time.Now()
	limits := testLimits(&now)
	dir := t.TempDir()
	s, err := Open(dir, limits)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	id := identity(512)
	if err = s.Register(id); err != nil {
		t.Fatal(err)
	}
	// Force completion to use the control block and simulate a partial seal write.
	s.limits.MetadataBytes = s.metadata
	if err = s.Complete(id, "Failed", now); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, id.Key(), "control"), os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteAt([]byte("incomplete seal"), controlLossBytes+terminalSlotSize)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("prepare torn control write: write=%v close=%v", err, closeErr)
	}
	report, err := Inspect(t.Context(), dir, limits)
	if err != nil || len(report.Findings) != 1 || report.Findings[0].Component != "control" {
		t.Fatalf("torn control entry was not reported: %+v, %v", report, err)
	}
	s, err = Open(dir, limits)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.Snapshot(id)
	if err != nil || m.CompletedAt == nil || !m.CompletedAt.Equal(now) || m.Outcome != "Failed" || m.SealedAt != nil || m.ProtectedUntil != nil || m.State != "partial" {
		t.Fatalf("torn control entry changed terminal evidence: %+v, %v", m, err)
	}
	now = now.Add(2 * Retention)
	if n, err := s.Sweep(); err != nil || n != 0 {
		t.Fatalf("torn seal authorized archive reclamation: %d, %v", n, err)
	}
}
