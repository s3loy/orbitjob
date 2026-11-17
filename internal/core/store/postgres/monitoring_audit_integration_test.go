//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/lib/pq"

	"orbitjob/internal/core/domain/audit"
	"orbitjob/internal/core/domain/check"
	"orbitjob/internal/core/domain/sli"
	"orbitjob/internal/core/domain/slo"
	"orbitjob/internal/platform/scan"
)

type monitoringAuditFixture struct {
	owner, admin                          *sql.DB
	tenantID, otherTenant, group, actorID string
	checks                                *CheckRepository
	slis                                  *SLIRepository
	slos                                  *SLORepository
	check                                 check.Snapshot
	sli                                   sli.Snapshot
	slo                                   slo.Snapshot
}

func newMonitoringAuditFixture(t *testing.T, kind string) *monitoringAuditFixture {
	t.Helper()
	f := &monitoringAuditFixture{owner: OpenStoreDB(t), tenantID: scan.GenerateID(),
		otherTenant: scan.GenerateID(), group: scan.GenerateID(), actorID: scan.GenerateID()}
	for _, id := range []string{f.tenantID, f.otherTenant} {
		if _, err := f.owner.ExecContext(t.Context(), `INSERT INTO tenants(id,slug,name) VALUES($1::char(26),$1::text,'Audit test')`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.owner.ExecContext(t.Context(), `INSERT INTO resource_groups(id,tenant_id,slug,name) VALUES($1,$2,'monitoring','Monitoring')`, f.group, f.tenantID); err != nil {
		t.Fatal(err)
	}
	f.admin = openRoleDB(t, f.owner, "orbitjob_admin")
	f.checks, f.slis, f.slos = NewCheckRepository(f.admin), NewSLIRepository(f.admin), NewSLORepository(f.admin)
	ctx := audit.WithActor(t.Context(), f.actorID, kind)
	var err error
	f.check, err = f.checks.Create(ctx, f.checkSpec("health"))
	if err != nil {
		t.Fatal(err)
	}
	f.sli, err = f.slis.Create(ctx, f.tenantID, f.sliSpec("availability"))
	if err != nil {
		t.Fatal(err)
	}
	f.slo, err = f.slos.Create(ctx, f.tenantID, f.sloSpec("availability-slo"))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *monitoringAuditFixture) checkSpec(name string) check.CreateSpec {
	interval := 60
	return check.CreateSpec{TenantID: f.tenantID, ResourceGroupID: f.group, Name: name,
		CheckType: check.CheckTypeHTTPHealth, CheckConfig: map[string]any{"url": "https://example.com/health", "headers": map[string]any{"Authorization": "private-check-value"}},
		AssertionRules: []check.AssertionRule{}, ScheduleType: check.ScheduleTypeInterval,
		IntervalSec: &interval, Timezone: "UTC", TimeoutSec: 5, Labels: map[string]any{}}
}

func (f *monitoringAuditFixture) sliSpec(name string) sli.CreateSpec {
	return sli.CreateSpec{Name: name, ResourceGroupID: f.group, SLIType: sli.TypeAvailability,
		SourceType: "job_run", SourceConfig: map[string]any{"source_uid": check.CheckSourceUID(f.check.ID)},
		Aggregation: "ratio", GoodEventCriteria: map[string]any{"status": "success"}}
}

func (f *monitoringAuditFixture) sloSpec(name string) slo.CreateSpec {
	return slo.CreateSpec{Name: name, ResourceGroupID: f.group, SLIID: f.sli.ID, Target: .99,
		WindowType: "rolling", WindowDuration: time.Hour, AlertFastBurnRate: 14.4, AlertSlowBurnRate: 6}
}

func TestMonitoringAuditRecordsAllManagementActions(t *testing.T) {
	for _, kind := range []string{"user", "api_key", "system"} {
		t.Run(kind, func(t *testing.T) {
			f := newMonitoringAuditFixture(t, kind)
			ctx := audit.WithActor(t.Context(), f.actorID, kind)
			var err error
			for _, action := range []string{check.ActionPause, check.ActionResume} {
				f.check, err = f.checks.ChangeStatus(ctx, f.tenantID, f.group, f.check.ID, f.check.Version, action)
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, status := range []string{slo.StatusPaused, slo.StatusActive} {
				f.slo, err = f.slos.ChangeStatus(ctx, f.tenantID, f.group, f.slo.ID, f.slo.Version, status)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := f.slos.Delete(ctx, f.tenantID, f.group, f.slo.ID, f.slo.Version); err != nil {
				t.Fatal(err)
			}
			if err := f.slis.Delete(ctx, f.tenantID, f.group, f.sli.ID, f.sli.Version); err != nil {
				t.Fatal(err)
			}
			if err := f.checks.Delete(ctx, f.tenantID, f.group, f.check.ID, f.check.Version); err != nil {
				t.Fatal(err)
			}
			rows, err := f.owner.QueryContext(t.Context(), `SELECT actor_type,actor_id,event_type,resource_type,resource_id,diff FROM audit_events WHERE tenant_id=$1 ORDER BY id`, f.tenantID)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			events := map[string]int{}
			ids := map[string]int64{"check": f.check.ID, "sli": f.sli.ID, "slo": f.slo.ID}
			for rows.Next() {
				var actorType, actorID, event, resourceType, resourceID string
				var raw []byte
				if err := rows.Scan(&actorType, &actorID, &event, &resourceType, &resourceID, &raw); err != nil {
					t.Fatal(err)
				}
				if actorType != kind || actorID != f.actorID || resourceID != strconv.FormatInt(ids[resourceType], 10) {
					t.Fatalf("wrong actor/resource for %s: %s %s %s", event, actorType, actorID, resourceID)
				}
				var diff map[string]any
				if err := json.Unmarshal(raw, &diff); err != nil {
					t.Fatal(err)
				}
				if diff["version"] == nil && (diff["from_version"] == nil || diff["to_version"] == nil) {
					t.Fatalf("missing configuration version for %s: %s", event, raw)
				}
				for _, sensitive := range []string{"check_config", "source_config", "good_event_criteria", "description"} {
					if _, present := diff[sensitive]; present {
						t.Fatalf("audit contains %s", sensitive)
					}
				}
				events[event]++
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			want := map[string]int{"check.created": 1, "check.status_changed": 2, "check.deleted": 1,
				"sli.created": 1, "sli.deleted": 1, "slo.created": 1, "slo.status_changed": 2, "slo.deleted": 1}
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("audit events = %v, want %v", events, want)
			}
		})
	}
}

func (f *monitoringAuditFixture) state(t *testing.T) map[string]string {
	t.Helper()
	state := map[string]string{}
	for _, table := range []string{"checks", "slis", "slos", "sli_configuration_versions", "sli_observation_state", "audit_events"} {
		var raw string
		if err := f.owner.QueryRowContext(t.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r)::text),'[]'::jsonb)::text FROM `+table+` r`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		state[table] = raw
	}
	return state
}

func TestMonitoringAuditFailureRollsBackEveryMutation(t *testing.T) {
	f := newMonitoringAuditFixture(t, "api_key")
	blockedActor := scan.GenerateID()
	if _, err := f.owner.ExecContext(t.Context(), fmt.Sprintf(`ALTER TABLE audit_events ADD CONSTRAINT reject_monitoring_audit CHECK(actor_id <> '%s') NOT VALID`, blockedActor)); err != nil {
		t.Fatal(err)
	}
	type mutation struct {
		name string
		run  func(context.Context) error
	}
	mutations := []mutation{
		{"check create", func(ctx context.Context) error {
			_, err := f.checks.Create(ctx, f.checkSpec("rejected-check"))
			return err
		}},
		{"check pause", func(ctx context.Context) error {
			_, err := f.checks.ChangeStatus(ctx, f.tenantID, f.group, f.check.ID, 1, check.ActionPause)
			return err
		}},
		{"check resume", func(ctx context.Context) error {
			_, err := f.checks.ChangeStatus(ctx, f.tenantID, f.group, f.check.ID, 1, check.ActionResume)
			return err
		}},
		{"check delete", func(ctx context.Context) error { return f.checks.Delete(ctx, f.tenantID, f.group, f.check.ID, 1) }},
		{"sli create", func(ctx context.Context) error {
			_, err := f.slis.Create(ctx, f.tenantID, f.sliSpec("rejected-sli"))
			return err
		}},
		{"sli delete", func(ctx context.Context) error { return f.slis.Delete(ctx, f.tenantID, f.group, f.sli.ID, 1) }},
		{"slo create", func(ctx context.Context) error {
			_, err := f.slos.Create(ctx, f.tenantID, f.sloSpec("rejected-slo"))
			return err
		}},
		{"slo pause", func(ctx context.Context) error {
			_, err := f.slos.ChangeStatus(ctx, f.tenantID, f.group, f.slo.ID, 1, slo.StatusPaused)
			return err
		}},
		{"slo resume", func(ctx context.Context) error {
			_, err := f.slos.ChangeStatus(ctx, f.tenantID, f.group, f.slo.ID, 1, slo.StatusActive)
			return err
		}},
		{"slo delete", func(ctx context.Context) error { return f.slos.Delete(ctx, f.tenantID, f.group, f.slo.ID, 1) }},
	}
	for _, mutation := range mutations {
		for _, identity := range []struct {
			name string
			ctx  context.Context
			err  error
		}{
			{"audit failure", audit.WithActor(t.Context(), blockedActor, "user"), nil},
			{"missing actor", t.Context(), audit.ErrActorRequired},
			{"invalid actor", audit.WithActor(t.Context(), blockedActor, "untrusted"), audit.ErrActorRequired},
		} {
			t.Run(mutation.name+"/"+identity.name, func(t *testing.T) {
				status := "active"
				if mutation.name == "check resume" {
					status = "paused"
				}
				if _, err := f.owner.ExecContext(t.Context(), `UPDATE checks SET status=$1 WHERE id=$2`, status, f.check.ID); err != nil {
					t.Fatal(err)
				}
				before := f.state(t)
				err := mutation.run(identity.ctx)
				if err == nil || (identity.err != nil && !errors.Is(err, identity.err)) {
					t.Fatalf("mutation error = %v, want rejected mutation (%v)", err, identity.err)
				}
				if identity.err == nil {
					var rejected *pq.Error
					if !errors.As(err, &rejected) || rejected.Constraint != "reject_monitoring_audit" {
						t.Fatalf("mutation failed before the injected audit failure: %v", err)
					}
				}
				if after := f.state(t); !reflect.DeepEqual(after, before) {
					t.Fatalf("failed mutation changed domain/audit state: before=%v after=%v", before, after)
				}
			})
		}
	}
}

func TestMonitoringAuditRespectsTenantGroupAndVersion(t *testing.T) {
	f := newMonitoringAuditFixture(t, "api_key")
	ctx := audit.WithActor(t.Context(), f.actorID, "api_key")
	otherGroup := scan.GenerateID()
	for _, boundary := range []struct {
		name, tenant, group string
		version             int
	}{
		{"foreign tenant", f.otherTenant, "", 1},
		{"foreign group", f.tenantID, otherGroup, 1},
		{"stale version", f.tenantID, f.group, 99},
	} {
		for _, action := range []struct {
			name string
			run  func() error
		}{
			{"check status", func() error {
				_, err := f.checks.ChangeStatus(ctx, boundary.tenant, boundary.group, f.check.ID, boundary.version, check.ActionPause)
				return err
			}},
			{"check delete", func() error {
				return f.checks.Delete(ctx, boundary.tenant, boundary.group, f.check.ID, boundary.version)
			}},
			{"sli delete", func() error { return f.slis.Delete(ctx, boundary.tenant, boundary.group, f.sli.ID, boundary.version) }},
			{"slo status", func() error {
				_, err := f.slos.ChangeStatus(ctx, boundary.tenant, boundary.group, f.slo.ID, boundary.version, slo.StatusPaused)
				return err
			}},
			{"slo delete", func() error { return f.slos.Delete(ctx, boundary.tenant, boundary.group, f.slo.ID, boundary.version) }},
		} {
			t.Run(boundary.name+"/"+action.name, func(t *testing.T) {
				before := f.state(t)
				if err := action.run(); err == nil {
					t.Fatal("out-of-scope or stale mutation succeeded")
				}
				if after := f.state(t); !reflect.DeepEqual(after, before) {
					t.Fatal("rejected boundary mutation changed state")
				}
			})
		}
	}
	paused, err := f.checks.ChangeStatus(ctx, f.tenantID, f.group, f.check.ID, f.check.Version, check.ActionPause)
	if err != nil {
		t.Fatal(err)
	}
	before := f.state(t)
	if _, err := f.checks.ChangeStatus(ctx, f.tenantID, f.group, paused.ID, paused.Version, check.ActionPause); err == nil {
		t.Fatal("repeated check pause succeeded")
	}
	if after := f.state(t); !reflect.DeepEqual(after, before) {
		t.Fatal("repeated check pause changed state")
	}
}
