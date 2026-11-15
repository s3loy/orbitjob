package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/lib/pq"
)

// runSettle samples the projected ledger, not qualification or unobserved CRs.
// Only a successful, scoped read may print a count for the hosted wait loop.
func runSettle(args []string) error {
	flags := flag.NewFlagSet("settle", flag.ContinueOnError)
	runID := flags.String("run-id", "", "run ID")
	runRoot := flags.String("run-root", "test/load/runs", "run root")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := ValidateCleanRunID(*runID); err != nil {
		return err
	}
	created, err := loadCreatedDefinitions(filepath.Join(*runRoot, *runID, "created-definitions.json"))
	if err != nil {
		return fmt.Errorf("load settle definitions: %w", err)
	}
	dsn := os.Getenv("ORBITJOB_DSN")
	if dsn == "" {
		return fmt.Errorf("ORBITJOB_DSN is required for settling")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("open settle database: check ORBITJOB_DSN")
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	count, err := settleInFlight(ctx, db, created)
	if err != nil {
		return err
	}
	fmt.Println(count)
	return nil
}

func settleInFlight(ctx context.Context, db *sql.DB, created []CreatedDefinition) (int64, error) {
	if len(created) == 0 {
		return 0, fmt.Errorf("settle definitions are empty")
	}
	byTenant := make(map[string][]int64)
	owners := make(map[int64]string)
	for _, definition := range created {
		if err := validateTenantID(definition.Tenant); err != nil {
			return 0, fmt.Errorf("settle definition has an invalid tenant")
		}
		if definition.RevisionID <= 0 {
			return 0, fmt.Errorf("settle definition has an invalid revision")
		}
		if owner, exists := owners[definition.RevisionID]; exists {
			if owner != definition.Tenant {
				return 0, fmt.Errorf("settle revision belongs to multiple tenants")
			}
			continue
		}
		owners[definition.RevisionID] = definition.Tenant
		byTenant[definition.Tenant] = append(byTenant[definition.Tenant], definition.RevisionID)
	}
	tenants := make([]string, 0, len(byTenant))
	for tenant := range byTenant {
		tenants = append(tenants, tenant)
	}
	sort.Strings(tenants)
	var inFlight int64
	for _, tenant := range tenants {
		var observed, pending int64
		err := withTenantTx(ctx, db, tenant, func(tx *sql.Tx) error {
			return tx.QueryRowContext(ctx, `
				SELECT count(*), count(*) FILTER (WHERE phase NOT IN ('Succeeded','Failed','Canceled'))
				FROM job_run_control_plane
				WHERE revision_id = ANY($1)
			`, pq.Array(byTenant[tenant])).Scan(&observed, &pending)
		})
		if err != nil {
			// Connection/driver errors can contain credentials. The wait loop logs
			// this failure without exposing raw DSNs or driver diagnostics.
			return 0, fmt.Errorf("settle ledger query failed for tenant %s; check database connectivity and read permissions", tenant)
		}
		if observed == 0 {
			return 0, fmt.Errorf("settle ledger has no observations for tenant %s", tenant)
		}
		inFlight += pending
	}
	return inFlight, nil
}
