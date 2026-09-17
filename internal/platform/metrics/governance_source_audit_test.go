package metrics

import (
	"fmt"
	"go/parser"
	"go/token"
	"testing"
)

// TestMetricDeclarationsAreParsable is the precondition for every other
// governance rule: the scan enumerates exported metric variables by parsing
// this package's sources, and it fails closed on anything it cannot read.
// A variable added here that promauto cannot parse would otherwise ship
// unaudited.
func TestMetricDeclarationsAreParsable(t *testing.T) {
	t.Parallel()
	res := loadScan(t)
	t.Logf("audited %d metric declarations across the default-build surface", len(res.Decls))
	for _, d := range res.Decls {
		if d.Name == "" {
			t.Errorf("%s:%d var %s has no Prometheus metric name", d.File, d.Line, d.Var)
		}
	}
}

// TestEveryExportedMetricHasAProducerOutsideTheMetricsPackage pins the rule
// that surfaced 19 dead metrics during the 2026-09 rebuild: a metric variable
// no production code references is a series that is registered, never
// incremented, and served forever at zero. Evidence is any selector
// reference in a build-matching, non-test file under internal/ or cmd/
// outside this package; test files do not count as producers.
//
// This test carries no exemption list. If it fails, the fix is either to wire
// a producer or to delete the metric, the way the 19 predecessors were
// deleted.
func TestEveryExportedMetricHasAProducerOutsideTheMetricsPackage(t *testing.T) {
	t.Parallel()
	res := loadScan(t)
	for _, dead := range unreferencedMetrics(res.Decls, res.Refs) {
		t.Errorf("dead metric: %s (%s declared at %s:%d) has no reference in any non-test producer under internal/ or cmd/",
			dead.Name, dead.Var, dead.File, dead.Line)
	}
	if len(res.Refs) == 0 && len(res.Decls) > 0 {
		t.Errorf("no producer file imports the metrics package; producer discovery is broken or the corpus is empty")
	}
}

// TestLabelCallSitesMatchDeclaredLabels pins the runtime invariant behind the
// label vectors: WithLabelValues must be called with exactly as many
// arguments as the declaration declares labels, otherwise the producer panics
// the first time it takes the code path.
func TestLabelCallSitesMatchDeclaredLabels(t *testing.T) {
	t.Parallel()
	res := loadScan(t)
	for _, site := range res.Sites {
		decl := res.ByVar[site.Var]
		if decl == nil {
			t.Errorf("%s:%d call site for %s does not match any declared metric variable", site.Path, site.Line, site.Var)
			continue
		}
		if got, want := len(site.ArgSrc), len(decl.Labels); got != want {
			t.Errorf("%s:%d %s.WithLabelValues passes %d value(s), but %s (%s) declares %d label(s)",
				site.Path, site.Line, site.Var, got, decl.Name, decl.File, want)
		}
	}
}

// TestEveryDeclaredMetricVarIsReferencedByACallSite strengthens the coverage
// rule for labeled metrics: a vec that is referenced (passed around, logged)
// but never has WithLabelValues called produces no series at all. Labeled
// metrics therefore need at least one label call site; plain metrics need
// only the reference proven by the test above.
func TestEveryDeclaredLabeledMetricHasALabelCallSite(t *testing.T) {
	t.Parallel()
	res := loadScan(t)
	sitedVars := make(map[string]bool, len(res.Sites))
	for _, site := range res.Sites {
		sitedVars[site.Var] = true
	}
	for _, decl := range res.Decls {
		if len(decl.Labels) > 0 && !sitedVars[decl.Var] {
			t.Errorf("labeled metric %s (%s declared at %s:%d) has no WithLabelValues call site anywhere in the corpus",
				decl.Name, decl.Var, decl.File, decl.Line)
		}
	}
}

// parseFixtureSource parses an in-test synthetic producer file so the red
// proofs can exercise the detectors against known-bad input without touching
// production files.
func parseFixtureSource(t *testing.T, name, src string) prodFile {
	t.Helper()
	pf := prodFile{Path: name, Src: []byte(src), FSet: token.NewFileSet()}
	f, err := parser.ParseFile(pf.FSet, name, src, 0)
	if err != nil {
		t.Fatalf("fixture %s does not parse: %v", name, err)
	}
	pf.AST = f
	pf.Alias = "metrics"
	return pf
}

func fixtureDecl(varName, metricName, help string, labels []string) metricDecl {
	kind := kindCounterVec
	if len(labels) == 0 {
		kind = kindCounter
	}
	return metricDecl{Var: varName, Name: metricName, Help: help, Labels: labels, Kind: kind, File: "fixture.go", Line: 1}
}

// TestRedProofProducerDetector proves the producer-coverage detector can
// fail: on a synthetic corpus where one fixture metric is referenced and one
// is not, exactly the unreferenced one is flagged. A detector that passed
// everything would be decoration, not a test.
func TestRedProofProducerDetector(t *testing.T) {
	t.Parallel()
	decls := []metricDecl{
		fixtureDecl("FixtureLiveMetric", "orbitjob_fixture_live_total", "Fixture that has a producer.", nil),
		fixtureDecl("FixtureDeadMetric", "orbitjob_fixture_dead_total", "Fixture with no producer anywhere.", nil),
	}
	byVar := map[string]*metricDecl{"FixtureLiveMetric": &decls[0], "FixtureDeadMetric": &decls[1]}
	src := `package fixture

import "example.invalid/platform/metrics"

func produce() {
	metrics.FixtureLiveMetric.Inc()
}
`
	pf := parseFixtureSource(t, "fixture_producer.go", src)
	refs := make(map[string][]refSite)
	var sites []labelCallSite
	collectRefsAndSites(pf, byVar, refs, &sites)

	dead := unreferencedMetrics(decls, refs)
	if len(dead) != 1 || dead[0].Var != "FixtureDeadMetric" {
		t.Fatalf("producer detector flagged %v; want exactly FixtureDeadMetric", names(dead))
	}
	// A chained call records the var reference from more than one AST node,
	// so assert presence, not an exact count.
	if len(refs["FixtureLiveMetric"]) == 0 {
		t.Fatalf("live fixture should have at least one recorded reference")
	}
}

func names(decls []metricDecl) []string {
	out := make([]string, 0, len(decls))
	for _, d := range decls {
		out = append(out, fmt.Sprintf("%s(%s)", d.Var, d.Name))
	}
	return out
}

// TestRedProofArityDetector proves the arity detector fires on a mismatch and
// stays silent on a correct call.
func TestRedProofArityDetector(t *testing.T) {
	t.Parallel()
	decls := []metricDecl{
		fixtureDecl("FixtureTwoLabels", "orbitjob_fixture_two_labels_total", "Fixture with two labels.", []string{"a", "b"}),
	}
	byVar := map[string]*metricDecl{"FixtureTwoLabels": &decls[0]}
	bad := parseFixtureSource(t, "fixture_arity_bad.go", `package fixture

import "example.invalid/platform/metrics"

func wrong() {
	metrics.FixtureTwoLabels.WithLabelValues("only-one").Inc()
}
`)
	good := parseFixtureSource(t, "fixture_arity_good.go", `package fixture

import "example.invalid/platform/metrics"

func right() {
	metrics.FixtureTwoLabels.WithLabelValues("one", "two").Inc()
}
`)
	var sites []labelCallSite
	refs := map[string][]refSite{}
	collectRefsAndSites(bad, byVar, refs, &sites)
	collectRefsAndSites(good, byVar, refs, &sites)
	badSites, goodSites := 0, 0
	for _, s := range sites {
		if len(s.ArgSrc) != len(byVar[s.Var].Labels) {
			badSites++
		} else {
			goodSites++
		}
	}
	if badSites != 1 || goodSites != 1 {
		t.Fatalf("arity detector classified %d bad / %d good call sites; want 1 / 1", badSites, goodSites)
	}
}
