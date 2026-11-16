package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"orbitjob/internal/core/domain/function"
	corepostgres "orbitjob/internal/core/store/postgres"
)

type ownedFunction struct {
	Case           string `json:"case"`
	ID             int64  `json:"id"`
	Tenant         string `json:"tenant_id"`
	Group          string `json:"resource_group_id,omitempty"`
	Name           string `json:"name"`
	ExpectRevision bool   `json:"expect_revision"`
}
type manifest struct {
	Version   int             `json:"version"`
	Context   string          `json:"context"`
	Namespace string          `json:"namespace"`
	Owner     string          `json:"owner"`
	Image     string          `json:"image"`
	Created   time.Time       `json:"created_at"`
	Cleaned   bool            `json:"cleaned"`
	Functions []ownedFunction `json:"functions"`
	Runs      []ownedRun      `json:"observed_jobruns,omitempty"`
}

func loadManifest(path string) (*manifest, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("manifest must be a private regular file (0600)")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, errors.New("invalid ownership manifest JSON")
	}
	if err := validateManifest(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func validateManifest(m *manifest) error {
	if m.Version != 1 || m.Context != fixtureContext || m.Namespace != fixtureNamespace || m.Image == "" {
		return errors.New("manifest is not for this fixture installation")
	}
	if _, err := uuid.Parse(m.Owner); err != nil {
		return errors.New("manifest owner must be a UUID")
	}
	if len(m.Functions) != 7 {
		return errors.New("manifest must contain exactly seven planned Functions")
	}
	ids, cases := map[int64]bool{}, map[string]bool{}
	for _, row := range m.Functions {
		if len(row.Tenant) != 26 || row.ID < 0 || row.Name != "m4-"+m.Owner[:8]+"-"+row.Case || cases[row.Case] {
			return errors.New("manifest has an invalid Function identity")
		}
		if row.Group != "" && len(row.Group) != 26 {
			return errors.New("invalid manifest group")
		}
		if _, ok := caseCommand(row.Case); !ok {
			return errors.New("unknown fixture case")
		}
		if row.ID > 0 && ids[row.ID] {
			return errors.New("manifest contains duplicate owned IDs")
		}
		ids[row.ID], cases[row.Case] = true, true
	}
	return nil
}

func withTenant(ctx context.Context, db *sql.DB, tenant string, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenant); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func seed(ctx context.Context, env *environment, o options, m *manifest) error {
	if m == nil {
		if len(o.tenant) != 26 || len(o.foreignTenant) != 26 || o.tenant == o.foreignTenant || len(o.group) != 26 {
			return errors.New("seed requires distinct existing --tenant / --foreign-tenant IDs and a primary --group (26 characters each)")
		}
		for _, tenant := range []string{o.tenant, o.foreignTenant} {
			if err := withTenant(ctx, env.db, tenant, func(tx *sql.Tx) error {
				var exists bool
				if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM tenants WHERE id=$1 AND status='active')", tenant).Scan(&exists); err != nil {
					return err
				}
				if !exists {
					return errors.New("tenant is not active or visible")
				}
				return nil
			}); err != nil {
				return fmt.Errorf("existing active tenant required: %w", err)
			}
		}
		if err := withTenant(ctx, env.db, o.tenant, func(tx *sql.Tx) error {
			var exists bool
			if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM resource_groups WHERE tenant_id=$1 AND id=$2)", o.tenant, o.group).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return errors.New("group does not belong to the primary tenant")
			}
			return nil
		}); err != nil {
			return err
		}
		image := o.image
		if image == "" {
			data, err := os.ReadFile("deploy/kind/fixtures/console-jobs.json")
			if err != nil {
				return errors.New("run from the repository root or supply --image")
			}
			var fixture struct {
				Items []struct {
					Spec struct{ JobTemplate struct{ Image string } }
				}
			}
			if err := json.Unmarshal(data, &fixture); err != nil || len(fixture.Items) == 0 {
				return errors.New("cannot read existing Console fixture image")
			}
			image = fixture.Items[0].Spec.JobTemplate.Image
		}
		if image == "" {
			return errors.New("fixture image is empty")
		}
		m = &manifest{Version: 1, Context: o.context, Namespace: o.namespace, Owner: uuid.NewString(), Image: image, Created: time.Now().UTC()}
		for _, kind := range []string{"success", "failed", "retry", "long", "paused", "grouped", "foreign"} {
			row := ownedFunction{Case: kind, Tenant: o.tenant, Name: "m4-" + m.Owner[:8] + "-" + kind, ExpectRevision: kind != "paused" && kind != "foreign"}
			if kind == "grouped" {
				row.Group = o.group
			}
			if kind == "foreign" {
				row.Tenant = o.foreignTenant
			}
			m.Functions = append(m.Functions, row)
		}
		if err := writeManifest(o.manifest, m); err != nil {
			return err
		}
	}
	if m.Cleaned {
		return errors.New("this manifest was cleaned; use a new manifest for a new test")
	}
	if err := verifyOwned(ctx, env.db, m); err != nil {
		return err
	}
	repo := corepostgres.NewFunctionRepository(env.db)
	for index := range m.Functions {
		row := &m.Functions[index]
		if row.ID != 0 {
			_, live, err := repo.GetForTenant(ctx, row.Tenant, row.ID)
			if err != nil {
				return err
			}
			if !live {
				return errors.New("a recorded definition is already retired; finish cleanup and use a new manifest")
			}
			continue
		}
		// Recover the commit-to-manifest crash window using the prewritten owner.
		if err := withTenant(ctx, env.db, row.Tenant, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `SELECT id FROM functions WHERE tenant_id=$1 AND name=$2
				AND labels->>$3=$4 AND labels->>$5=$6 AND deleted_at IS NULL`, row.Tenant, row.Name, ownerLabel, m.Owner, caseLabel, row.Case).Scan(&row.ID)
		}); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if row.ID == 0 {
			command, _ := caseCommand(row.Case)
			def := function.Definition{Name: row.Name, Description: "Test-only M4 Console fixture", Status: function.StatusActive,
				ResourceGroupID: row.Group, Image: m.Image, Command: []string{"sh", "-c", command}, TimeoutSeconds: 120,
				HistorySuccess: 20, HistoryFailed: 20, Version: 1, Labels: map[string]any{ownerLabel: m.Owner, caseLabel: row.Case}}
			if row.Case == "paused" {
				def.Status = function.StatusPaused
			}
			if row.Case == "retry" {
				def.RetryLimit = 1
			}
			if row.Case == "long" {
				def.TimeoutSeconds = 360
			}
			created, err := repo.CreateForTenant(ctx, row.Tenant, def)
			if err != nil {
				return err
			}
			row.ID = created.ID
		}
		if err := writeManifest(o.manifest, m); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(os.Stdout, "Owned definitions are recorded. Waiting for the real operator projection; no invocation was sent."); err != nil {
		return err
	}
	return waitRevisions(ctx, env.db, o, m)
}

func caseCommand(kind string) (string, bool) {
	switch kind {
	case "failed":
		return "echo m4-failed; sleep 2; exit 1", true
	case "retry":
		return `echo "$HOSTNAME"; sleep 2; case "${HOSTNAME%-*}" in *-1) exit 1;; *) exit 0;; esac`, true
	case "long":
		return "echo m4-long; sleep 300", true
	case "success", "paused", "grouped", "foreign":
		return "echo m4-success; sleep 2; exit 0", true
	}
	return "", false
}

func verifyOwned(ctx context.Context, db *sql.DB, m *manifest) error {
	if err := validateManifest(m); err != nil {
		return err
	}
	for _, row := range m.Functions {
		if row.ID == 0 {
			continue
		}
		if err := withTenant(ctx, db, row.Tenant, func(tx *sql.Tx) error {
			var name, owner, kind, group string
			if err := tx.QueryRowContext(ctx, `SELECT name, labels->>$3, labels->>$4, COALESCE(resource_group_id,'')
				FROM functions WHERE tenant_id=$1 AND id=$2`, row.Tenant, row.ID, ownerLabel, caseLabel).Scan(&name, &owner, &kind, &group); err != nil {
				return err
			}
			if name != row.Name || owner != m.Owner || kind != row.Case || group != row.Group {
				return errors.New("owned ID does not match the recorded owner, case, name and group")
			}
			return nil
		}); err != nil {
			return fmt.Errorf("verify owned Function %d: %w", row.ID, err)
		}
	}
	return nil
}

type functionStatus struct {
	ownedFunction
	Revision         int64  `json:"active_revision_id"`
	Namespace        string `json:"execution_namespace,omitempty"`
	Generation       int64  `json:"execution_generation"`
	TerminalOutcomes int    `json:"recorded_terminal_outcomes"`
}

func statuses(ctx context.Context, db *sql.DB, m *manifest) ([]functionStatus, bool, error) {
	all := make([]functionStatus, 0, len(m.Functions))
	ready := true
	for _, row := range m.Functions {
		item := functionStatus{ownedFunction: row}
		if row.ID == 0 {
			ready = false
			all = append(all, item)
			continue
		}
		err := withTenant(ctx, db, row.Tenant, func(tx *sql.Tx) error {
			err := tx.QueryRowContext(ctx, `SELECT id, source_namespace, generation FROM job_definition_revisions
				WHERE tenant_id=$1 AND source_mode='function' AND source_uid=$2 AND is_active`, row.Tenant, function.SourceUID(row.ID)).Scan(&item.Revision, &item.Namespace, &item.Generation)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			return tx.QueryRowContext(ctx, "SELECT count(*) FROM function_runs WHERE tenant_id=$1 AND function_id=$2", row.Tenant, row.ID).Scan(&item.TerminalOutcomes)
		})
		if err != nil {
			return nil, false, err
		}
		if row.ExpectRevision && (item.Revision == 0 || item.Namespace != fixtureNamespace || item.Generation != 1) {
			ready = false
		}
		all = append(all, item)
	}
	return all, ready, nil
}

func printStatus(ctx context.Context, db *sql.DB, m *manifest) error {
	rows, _, err := statuses(ctx, db, m)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(rows)
}

func waitRevisions(ctx context.Context, db *sql.DB, o options, m *manifest) error {
	if m.Cleaned {
		return errors.New("fixture is cleaned; no new execution should be requested")
	}
	waitCtx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	for {
		rows, ready, err := statuses(waitCtx, db, m)
		if err != nil {
			return err
		}
		if ready {
			return json.NewEncoder(os.Stdout).Encode(rows)
		}
		select {
		case <-waitCtx.Done():
			return errors.New("active revision wait expired; inspect operator leadership, namespace mapping, DB grants and sync logs; definitions remain owned")
		case <-time.After(time.Second):
		}
	}
}
