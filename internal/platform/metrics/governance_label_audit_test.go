package metrics

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Governance debt baselines.
//
// The 2026-09 governance rules forbid tenant_id / run_id / user-id labels and
// require help text that describes each declared label. A slice of the metric
// surface predates those rules and still carries that debt. This metrics gate
// names and cites each known violation so it suppresses exactly the existing
// set and rejects every new one.
//
// Deleting an entry once the metric is relabeled is required housekeeping.
// Any violation on a metric not listed below fails the suite, so the baseline
// is a ratchet: it can only shrink.

// labelDebtBaseline lists metrics that violate the bounded-label rule today:
// they declare a tenant-shaped label and/or producers pass a tenant-shaped
// value at a call site. Citations name the declaring file and the producing
// call sites as of 2026-09-17.
var labelDebtBaseline = map[string][]string{
	"orbitjob_api_keys_total": {
		"internal/platform/metrics/admin.go:13 declares tenant_id",
		"internal/admin/http/handler.go:1322 passes tenantID",
	},
	"orbitjob_api_keys_revoked_total": {
		"internal/platform/metrics/admin.go:19 declares tenant_id",
		"internal/admin/http/handler.go:1376 passes tenantID",
	},
	"orbitjob_policies_total": {
		"internal/platform/metrics/admin.go:27 declares tenant_id",
		"internal/admin/http/handler.go:1403 passes tenantID",
	},
	"orbitjob_check_runs_created_total": {
		"internal/platform/metrics/check.go:13 declares tenant_id",
		"internal/core/app/checkschedule/scheduler.go:80 passes tenantID",
	},
	// Both check-run metrics are currently dead (their only producer,
	// internal/core/app/checkexecute/worker.go, was deleted in the
	// kubernetes-only ledger refactor) yet still declare tenant_id. They are
	// recorded here so the label gate reports their debt accurately; the
	// producer gate — which has no baseline — fails for them independently.
	"orbitjob_check_runs_completed_total": {
		"internal/platform/metrics/check.go:16 declares tenant_id (metric currently has no producer)",
	},
	"orbitjob_check_run_duration_seconds": {
		"internal/platform/metrics/check.go:22 declares tenant_id (metric currently has no producer)",
	},
	"orbitjob_trigger_latency_seconds": {
		"internal/platform/metrics/job.go:22 declares tenant_id",
		"internal/admin/app/job/command/trigger.go:117 passes normalized.TenantID",
	},
	// The function invoke surface follows the trigger/slo siblings: tenant_id
	// for per-tenant rollups, declared 2026-09-18 with the functioninvoke use
	// case as the producer.
	"orbitjob_function_invocations_total": {
		"internal/platform/metrics/function.go:19 declares tenant_id",
		"internal/core/app/functioninvoke/invoke.go:172 passes normalized.TenantID",
	},
	"orbitjob_function_duration_seconds": {
		"internal/platform/metrics/function.go:31 declares tenant_id",
		"internal/core/app/functioninvoke/invoke.go:173 passes normalized.TenantID",
	},
	"orbitjob_ratelimit_hits_total": {
		"internal/platform/metrics/ratelimit.go:13 declares tenant_id",
		"internal/admin/http/middleware/ratelimit.go:107 passes tenantID",
	},
	"orbitjob_ratelimit_passed_total": {
		"internal/platform/metrics/ratelimit.go:19 declares tenant_id",
		"internal/admin/http/middleware/ratelimit.go:115 passes tenantID",
	},
	"orbitjob_sli_evaluations_total": {
		"internal/platform/metrics/slo.go:13 declares tenant_id",
		"internal/core/app/sloevaluate/recorder.go:81 passes tenantID",
	},
	"orbitjob_slo_snapshot_increments_total": {
		"internal/platform/metrics/slo.go:19 declares tenant_id",
		"internal/core/app/sloevaluate/recorder.go:82 passes tenantID and a numeric snapshot row id (strconv.FormatInt(s.ID, 10))",
	},
	"orbitjob_slo_budget_burn_rate": {
		"internal/platform/metrics/slo.go:25 declares tenant_id",
		"internal/core/app/sloevaluate/evaluator.go:184 passes tenantID and an SLO object id",
	},
	"orbitjob_slo_budget_status": {
		"internal/platform/metrics/slo.go:31 declares tenant_id",
		"internal/core/app/sloevaluate/evaluator.go:192 passes tenantID and an SLO object id",
	},
	"orbitjob_slo_budget_alerts_total": {
		"internal/platform/metrics/slo.go:37 declares tenant_id",
		"internal/core/app/sloevaluate/evaluator.go:226 and :246 pass tenantID",
	},
}

// helpDebtBaseline lists labeled metrics whose help text does not mention
// every declared label, as of 2026-09-17.
var helpDebtBaseline = map[string][]string{
	"orbitjob_api_keys_total":                {"internal/platform/metrics/admin.go:12"},
	"orbitjob_api_keys_revoked_total":        {"internal/platform/metrics/admin.go:18"},
	"orbitjob_policies_total":                {"internal/platform/metrics/admin.go:26"},
	"orbitjob_check_runs_created_total":      {"internal/platform/metrics/check.go:12"},
	"orbitjob_check_runs_completed_total":    {"internal/platform/metrics/check.go:18"},
	"orbitjob_check_run_duration_seconds":    {"internal/platform/metrics/check.go:24"},
	"orbitjob_trigger_latency_seconds":       {"internal/platform/metrics/job.go:20"},
	"orbitjob_sli_evaluations_total":         {"internal/platform/metrics/slo.go:12"},
	"orbitjob_slo_snapshot_increments_total": {"internal/platform/metrics/slo.go:18"},
	"orbitjob_slo_budget_burn_rate":          {"internal/platform/metrics/slo.go:24"},
	"orbitjob_slo_budget_status":             {"internal/platform/metrics/slo.go:30"},
	"orbitjob_slo_budget_alerts_total":       {"internal/platform/metrics/slo.go:36"},
}

// TestNoMetricDeclaresUnboundedLabels pins the declaration half of the
// bounded-cardinality rule: no metric may declare a tenant-, run-id- or
// user-id-shaped label. Violations on baselined legacy metrics are logged
// with their citations; anything new fails.
func TestNoMetricDeclaresUnboundedLabels(t *testing.T) {
	t.Parallel()
	res := loadScan(t)
	suppressed := map[string]bool{}
	for _, decl := range res.Decls {
		bad := deniedIdentifiers(strings.Join(decl.Labels, " "))
		if len(bad) == 0 {
			continue
		}
		cites, known := labelDebtBaseline[decl.Name]
		if !known {
			t.Errorf("%s (%s declared at %s:%d) declares unbounded label(s) %v; labels must come from fixed small sets",
				decl.Name, decl.Var, decl.File, decl.Line, bad)
			continue
		}
		suppressed[decl.Name] = true
		t.Logf("KNOWN DEBT %s (%s:%d) unbounded labels %v; e.g. %s",
			decl.Name, decl.File, decl.Line, bad, cites[0])
	}
	reportUnpaidBaseline(t, "label", labelDebtBaseline, suppressed)
}

// TestLabelCallSitesUseBoundedValues pins the producer half of the rule:
// every WithLabelValues argument is scanned for identifiers shaped like a
// tenant id, run id, user id, or a bare object id. String literals and
// bounded-set identifiers (resource, endpoint group, severity) pass; a value
// derived from a request-scoped identity fails.
func TestLabelCallSitesUseBoundedValues(t *testing.T) {
	t.Parallel()
	res := loadScan(t)
	suppressed := map[string]bool{}
	for _, site := range res.Sites {
		var bad []string
		for _, arg := range site.ArgSrc {
			bad = append(bad, deniedIdentifiers(arg)...)
		}
		if len(bad) == 0 {
			continue
		}
		decl := res.ByVar[site.Var]
		name := site.Var
		if decl != nil {
			name = decl.Name
		}
		cites, known := labelDebtBaseline[name]
		if !known {
			t.Errorf("%s:%d %s.WithLabelValues passes unbounded value(s) %v (metric %s)",
				filepath.Base(site.Path), site.Line, site.Var, bad, name)
			continue
		}
		suppressed[name] = true
		t.Logf("KNOWN DEBT %s call site %s:%d passes %v; e.g. %s",
			name, filepath.Base(site.Path), site.Line, bad, cites[len(cites)-1])
	}
	reportUnpaidBaseline(t, "label", labelDebtBaseline, suppressed)
}

// TestHelpTextDescribesItsLabels pins that a metric's help text names each
// declared label (comparison ignores underscores and case, so "by tenant"
// alone does not satisfy a tenant_id label). Legacy gaps are logged against
// the baseline; new gaps fail.
func TestHelpTextDescribesItsLabels(t *testing.T) {
	t.Parallel()
	res := loadScan(t)
	suppressed := map[string]bool{}
	for _, decl := range res.Decls {
		if len(decl.Labels) == 0 {
			continue
		}
		missing := labelsMissingFromHelp(decl.Help, decl.Labels)
		if len(missing) == 0 {
			continue
		}
		cites, known := helpDebtBaseline[decl.Name]
		if !known {
			t.Errorf("%s (%s:%d) help text does not describe label(s) %v",
				decl.Name, decl.File, decl.Line, missing)
			continue
		}
		suppressed[decl.Name] = true
		t.Logf("KNOWN DEBT %s (%s:%d) help omits %v; cite %s",
			decl.Name, decl.File, decl.Line, missing, cites[0])
	}
	reportUnpaidBaseline(t, "help", helpDebtBaseline, suppressed)
}

// reportUnpaidBaseline logs baseline entries that no longer match any
// violation: the debt has been paid and the entry should be deleted. This is
// a log, not a failure — deleting an entry is the owner's cleanup, and the
// gate's job is only to refuse new debt.
func reportUnpaidBaseline(t *testing.T, kind string, baseline map[string][]string, suppressed map[string]bool) {
	t.Helper()
	var paid []string
	for name := range baseline {
		if !suppressed[name] {
			paid = append(paid, name)
		}
	}
	sort.Strings(paid)
	for _, name := range paid {
		t.Logf("BASELINE ENTRY %s FOR %s NO LONGER MATCHES A VIOLATION - delete it from governance_label_audit_test.go", kind, name)
	}
}

// TestRedProofLabelDetectors proves both label detectors discriminate: a
// synthetic tenant-labeled fixture and a tenant-derived call site are
// flagged, while bounded controls are not. Without these proofs the denylist
// could be silently inert or over-broad.
func TestRedProofLabelDetectors(t *testing.T) {
	t.Parallel()

	// Declaration-level: tenant-shaped label flagged, bounded label not.
	badDecl := fixtureDecl("FixtureTenantMetric", "orbitjob_fixture_tenant_total", "Fixture counter.", []string{"tenant_id"})
	okDecl := fixtureDecl("FixtureResourceMetric", "orbitjob_fixture_resource_total", "Fixture counter labeled by resource.", []string{"resource"})
	if got := deniedIdentifiers("tenant_id"); len(got) == 0 {
		t.Fatalf("declaration detector did not flag a tenant_id label")
	}
	if got := deniedIdentifiers("resource"); len(got) != 0 {
		t.Fatalf("declaration detector flagged the bounded control label %q: %v", "resource", got)
	}
	if badDecl.Name == "" || okDecl.Name == "" {
		t.Fatal("fixture declarations malformed")
	}

	// Call-site level: parse a synthetic producer and run the same collector
	// the real corpus goes through.
	decls := []metricDecl{badDecl, okDecl}
	byVar := map[string]*metricDecl{"FixtureTenantMetric": &decls[0], "FixtureResourceMetric": &decls[1]}
	src := `package fixture

import "example.invalid/platform/metrics"

func produce(tenantID, group string) {
	metrics.FixtureTenantMetric.WithLabelValues(tenantID).Inc()
	metrics.FixtureResourceMetric.WithLabelValues(string(group)).Inc()
}
`
	pf := parseFixtureSource(t, "fixture_labels.go", src)
	refs := map[string][]refSite{}
	var sites []labelCallSite
	collectRefsAndSites(pf, byVar, refs, &sites)

	flagged, clean := 0, 0
	for _, site := range sites {
		hit := false
		for _, arg := range site.ArgSrc {
			if len(deniedIdentifiers(arg)) > 0 {
				hit = true
			}
		}
		switch site.Var {
		case "FixtureTenantMetric":
			if !hit {
				t.Fatalf("call-site detector did not flag the tenant-derived site: %+v", site)
			}
			flagged++
		case "FixtureResourceMetric":
			if hit {
				t.Fatalf("call-site detector flagged the bounded control site: %+v", site)
			}
			clean++
		}
	}
	if flagged != 1 || clean != 1 {
		t.Fatalf("call-site detector classified %d flagged / %d clean; want 1 / 1", flagged, clean)
	}

	// Help-level: missing label description flagged, described label not.
	if got := labelsMissingFromHelp("Total fixture operations.", []string{"widget"}); len(got) != 1 {
		t.Fatalf("help detector did not flag the undocumented label: %v", got)
	}
	if got := labelsMissingFromHelp("Total fixture operations, labeled by widget.", []string{"widget"}); len(got) != 0 {
		t.Fatalf("help detector flagged a documented label: %v", got)
	}
}
