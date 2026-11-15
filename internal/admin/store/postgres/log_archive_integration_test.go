//go:build integration

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	corepostgres "orbitjob/internal/core/store/postgres"
	"orbitjob/internal/logarchive"
	"orbitjob/internal/platform/postgrestest"
	"orbitjob/internal/platform/scan"
)

func archiveRuntimeConnection(t *testing.T, db *sql.DB) *sql.DB {
	t.Helper()
	var schema string
	if e := db.QueryRow("SELECT current_schema()").Scan(&schema); e != nil {
		t.Fatal(e)
	}
	u, e := url.Parse(postgrestest.DSN(t))
	if e != nil {
		t.Fatal("invalid integration DSN")
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	q.Set("options", "-c role=orbitjob_runtime")
	u.RawQuery = q.Encode()
	runtime, e := sql.Open("postgres", u.String())
	if e != nil {
		t.Fatal("cannot open scoped Runtime connection")
	}
	t.Cleanup(func() { _ = runtime.Close() })
	return runtime
}

func TestArchiveRLSRetentionAndAuditedCleanup(t *testing.T) {
	db := postgrestest.Open(t)
	ctx := context.Background()
	tenant, foreign := scan.GenerateID(), scan.GenerateID()
	for _, id := range []string{tenant, foreign} {
		if _, e := db.Exec(`INSERT INTO tenants(id,slug,name,status) VALUES($1,$2,'Archive tests','active')`, id, "archive-"+id); e != nil {
			t.Fatal(e)
		}
	}
	sum := sha256.Sum256([]byte(uuid.NewString()))
	occ := hex.EncodeToString(sum[:])
	source, runUID := uuid.NewString(), uuid.NewString()
	var revision, run int64
	if e := db.QueryRow(`INSERT INTO job_definition_revisions(tenant_id,source_mode,source_uid,source_namespace,source_name,generation,spec_hash,normalized_spec,is_active,actor) VALUES($1,'kubernetes',$2,'own','task',1,'hash','{}',true,'test')RETURNING id`, tenant, source).Scan(&revision); e != nil {
		t.Fatal(e)
	}
	if e := db.QueryRow(`INSERT INTO job_run_control_plane(tenant_id,source_uid,revision_id,occurrence_key,trigger,actor,phase,outcome_recorded_at)VALUES($1,$2,$3,$4,'Manual','test','Succeeded',now())RETURNING id`, tenant, source, revision, occ).Scan(&run); e != nil {
		t.Fatal(e)
	}
	id := logarchive.Identity{TenantID: tenant, SourceUID: source, OccurrenceKey: occ, Namespace: "own", SourceName: "task", RevisionID: revision, RunID: run, RunName: "task-" + occ, RunUID: runUID, CreatedAt: time.Now().UTC()}
	now := time.Now().UTC()
	until := now.Add(logarchive.Retention)
	m := logarchive.Manifest{Identity: id, State: "sealed", CompletedAt: &now, ProtectedUntil: &until}
	runtimeDB := archiveRuntimeConnection(t, db)
	writer := logarchive.Repository{DB: runtimeDB}
	adminDB := queryAdminConnection(t, db)
	reader := logarchive.Repository{DB: adminDB}
	if e := writer.Sync(ctx, m); e != nil {
		t.Fatal(e)
	}
	actual, e := reader.Resolve(ctx, tenant, run, 0, "")
	if e != nil || actual.RunUID != runUID {
		t.Fatalf("authorized archive mapping: %+v %v", actual, e)
	}
	if _, e = reader.Resolve(ctx, foreign, run, 0, ""); !errors.Is(e, logarchive.ErrMissing) {
		t.Fatalf("foreign tenant archive exposed: %v", e)
	}
	if _, e = adminDB.Exec(`UPDATE execution_log_archives SET state='deleted'`); e == nil {
		t.Fatal("HTTP DB identity can modify archive state")
	}
	actor := scan.GenerateID()
	if e = reader.RequestCleanup(ctx, id, actor); e != nil {
		t.Fatal(e)
	}
	requested, e := writer.CleanupRequested(ctx, id)
	if e != nil || !requested {
		t.Fatalf("cleanup outbox not visible: %v %v", requested, e)
	}
	var events int
	if e = db.QueryRow(`SELECT count(*)FROM audit_events WHERE tenant_id=$1 AND actor_id=$2 AND resource_type='execution_logs' AND diff->>'state'='requested'`, tenant, actor).Scan(&events); e != nil || events != 1 {
		t.Fatalf("cleanup audit did not commit atomically: %d %v", events, e)
	}
	core := corepostgres.NewControlPlaneRepository(runtimeDB)
	rows, e := core.PrunableRuns(ctx, tenant, source, 0, 0)
	if e != nil || len(rows) != 0 {
		t.Fatalf("15-day archive parent was prunable: %v %v", rows, e)
	}
	if deleted, e := core.DeleteRun(ctx, tenant, run); e != nil || deleted {
		t.Fatalf("direct delete bypassed archive protection: %v %v", deleted, e)
	}
	if _, e = db.Exec(`UPDATE execution_log_archives SET state='expired',protected_until=now()-interval '1 second' WHERE tenant_id=$1`, tenant); e != nil {
		t.Fatal(e)
	}
	rows, e = core.PrunableRuns(ctx, tenant, source, 0, 0)
	if e != nil || len(rows) != 1 {
		t.Fatalf("expired parent was not reclaimable: %v %v", rows, e)
	}
	if deleted, e := core.DeleteRun(ctx, tenant, run); e != nil || !deleted {
		t.Fatalf("expired parent delete: %v %v", deleted, e)
	}
	if actual, e = reader.Resolve(ctx, tenant, run, 0, ""); e != nil || actual.RunUID != runUID {
		t.Fatalf("archive identity depended on deleted ledger: %+v %v", actual, e)
	}
}

func TestMetadataQuotaStillAllowsExistingArchiveSeal(t *testing.T) {
	db := postgrestest.Open(t)
	ctx := context.Background()
	tenant := scan.GenerateID()
	if _, e := db.Exec(`INSERT INTO tenants(id,slug,name,status)VALUES($1,$2,'Capacity tests','active')`, tenant, "capacity-"+tenant); e != nil {
		t.Fatal(e)
	}
	id := logarchive.Identity{TenantID: tenant, SourceUID: uuid.NewString(), OccurrenceKey: uuid.NewString(), Namespace: "own", SourceName: "task", RevisionID: 1, RunID: 1, RunName: "task-run", RunUID: uuid.NewString(), CreatedAt: time.Now().UTC()}
	writer := logarchive.Repository{DB: archiveRuntimeConnection(t, db)}
	m := logarchive.Manifest{Identity: id, State: "collecting"}
	if e := writer.Sync(ctx, m); e != nil {
		t.Fatal(e)
	}
	var definition string
	if e := db.QueryRow(`SELECT pg_get_functiondef(('bound_execution_log_metadata()')::regprocedure)`).Scan(&definition); e != nil {
		t.Fatal(e)
	}
	limited := strings.ReplaceAll(definition, "268435456", "0")
	if limited == definition {
		t.Fatal("capacity negative control did not change its target")
	}
	if _, e := db.Exec(limited); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if _, e := db.Exec(definition); e != nil {
			t.Error(e)
		}
	}()
	now := time.Now().UTC()
	until := now.Add(logarchive.Retention)
	m.State = "sealed"
	m.CompletedAt = &now
	m.ProtectedUntil = &until
	if e := writer.Sync(ctx, m); e != nil {
		t.Fatalf("metadata pressure blocked existing archive seal: %v", e)
	}
	other := m
	other.Identity.OccurrenceKey = uuid.NewString()
	other.Identity.RunUID = uuid.NewString()
	other.Identity.RunID = 2
	if e := writer.Sync(ctx, other); e == nil {
		t.Fatal("metadata admission exceeded the controlled capacity floor")
	}
}
