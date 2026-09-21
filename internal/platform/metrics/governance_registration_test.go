package metrics

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// TestMetricNamesAreUnique pins that no two declarations in this package
// register the same metric name. promauto would panic the binary at init on
// a real duplicate, so this source-level check is the one that can report
// the collision readably instead of crashing every binary that links us.
func TestMetricNamesAreUnique(t *testing.T) {
	t.Parallel()
	res := loadScan(t)
	for name, count := range duplicateMetricNames(res.Decls) {
		var at []string
		for _, d := range res.Decls {
			if d.Name == name {
				at = append(at, fmt.Sprintf("%s:%d (var %s)", d.File, d.Line, d.Var))
			}
		}
		t.Errorf("metric name %s is declared %d times: %v", name, count, at)
	}
}

// TestHelpTextIsPresentAndNonEmpty pins that every metric carries help text:
// an empty help string renders as a bare series in /metrics and makes the
// surface unusable to anyone reading it without this repository.
func TestHelpTextIsPresentAndNonEmpty(t *testing.T) {
	t.Parallel()
	res := loadScan(t)
	for _, d := range res.Decls {
		if strings.TrimSpace(d.Help) == "" {
			t.Errorf("%s (%s declared at %s:%d) has an empty Help string", d.Name, d.Var, d.File, d.Line)
		}
	}
}

var (
	validMetricName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	namePrefix      = "orbitjob_"
)

// TestMetricNamingConventions pins the Prometheus naming conventions the
// surface already follows: lowercase snake case under the orbitjob_
// namespace, no double underscores, counters suffixed _total, nothing else
// suffixed _total, and duration/latency histograms denominated in seconds.
func TestMetricNamingConventions(t *testing.T) {
	t.Parallel()
	res := loadScan(t)
	for _, d := range res.Decls {
		name := d.Name
		if !validMetricName.MatchString(name) {
			t.Errorf("%s (%s:%d) is not lowercase snake case", name, d.File, d.Line)
		}
		if strings.Contains(name, "__") || strings.HasSuffix(name, "_") {
			t.Errorf("%s (%s:%d) contains a double underscore or a trailing underscore", name, d.File, d.Line)
		}
		if !strings.HasPrefix(name, namePrefix) {
			t.Errorf("%s (%s:%d) is outside the %s namespace", name, d.File, d.Line, namePrefix)
		}
		isCounter := d.Kind == kindCounter || d.Kind == kindCounterVec
		if isCounter && !strings.HasSuffix(name, "_total") {
			t.Errorf("counter %s (%s:%d) does not carry the _total unit suffix", name, d.File, d.Line)
		}
		if !isCounter && strings.HasSuffix(name, "_total") {
			t.Errorf("%s (%s:%d) is not a counter but carries the _total suffix", name, d.File, d.Line)
		}
		if (strings.Contains(name, "duration") || strings.Contains(name, "latency")) && !strings.HasSuffix(name, "_seconds") {
			t.Errorf("%s (%s:%d) measures a duration but is not denominated in _seconds", name, d.File, d.Line)
		}
	}
}

// TestMetricsAreServedByTheDefaultRegistry pins the serving path: promauto
// registers every declaration into prometheus.DefaultRegisterer, and
// promhttp.Handler() — the handler behind /metrics in
// internal/platform/health/server.go:19 and cmd/admin-api/main.go:91 —
// gathers from the default registry. For each declaration, registering a
// type-identical dummy must fail as a duplicate: client_golang reports that
// either as AlreadyRegisteredError or, when descriptor IDs collide, as the
// plain "duplicate metrics collector registration attempted" error. Both
// prove the name is already in the serving registry. If the dummy registers
// cleanly, the real metric is absent and the dummy is unregistered again.
func TestMetricsAreServedByTheDefaultRegistry(t *testing.T) {
	res := loadScan(t)
	for _, d := range res.Decls {
		dummy := dummyCollectorFor(d)
		err := prometheus.DefaultRegisterer.Register(dummy)
		if err == nil {
			prometheus.DefaultRegisterer.Unregister(dummy)
			t.Errorf("%s (%s) is declared but not registered in the default registry that /metrics serves", d.Name, d.Var)
			continue
		}
		var already *prometheus.AlreadyRegisteredError
		isAlready := errors.As(err, &already)
		isDuplicate := strings.Contains(err.Error(), "duplicate metrics collector registration attempted")
		if !isAlready && !isDuplicate {
			t.Errorf("%s (%s): registering a duplicate returned an unexpected error: %v", d.Name, d.Var, err)
		}
	}
}

// dummyCollectorFor builds a collector whose descriptor is identical to the
// declaration's (same name, help, type, and variable labels), so the
// registry's duplicate check collides with the promauto-registered original.
func dummyCollectorFor(d metricDecl) prometheus.Collector {
	switch d.Kind {
	case kindCounter:
		return prometheus.NewCounter(prometheus.CounterOpts{Name: d.Name, Help: d.Help})
	case kindCounterVec:
		return prometheus.NewCounterVec(prometheus.CounterOpts{Name: d.Name, Help: d.Help}, d.Labels)
	case kindGauge:
		return prometheus.NewGauge(prometheus.GaugeOpts{Name: d.Name, Help: d.Help})
	case kindGaugeVec:
		return prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: d.Name, Help: d.Help}, d.Labels)
	case kindHistogram:
		return prometheus.NewHistogram(prometheus.HistogramOpts{Name: d.Name, Help: d.Help})
	case kindHistogramVec:
		return prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: d.Name, Help: d.Help}, d.Labels)
	}
	panic(fmt.Sprintf("unhandled metric kind %q for %s", d.Kind, d.Name))
}

// TestRedProofUniquenessAndNamingDetectors proves the registration-side
// detectors fire on known-bad fixtures and stay silent on known-good ones.
func TestRedProofUniquenessAndNamingDetectors(t *testing.T) {
	t.Parallel()

	// Duplicate names are flagged, and only the colliding name.
	dupDecls := []metricDecl{
		fixtureDecl("FixtureDupA", "orbitjob_fixture_dup_total", "First copy.", nil),
		fixtureDecl("FixtureDupB", "orbitjob_fixture_dup_total", "Second copy.", nil),
		fixtureDecl("FixtureSolo", "orbitjob_fixture_solo_total", "Lone metric.", nil),
	}
	dups := duplicateMetricNames(dupDecls)
	if len(dups) != 1 || dups["orbitjob_fixture_dup_total"] != 2 {
		t.Fatalf("uniqueness detector found %v; want exactly orbitjob_fixture_dup_total twice", dups)
	}

	// Naming violations are each caught: uppercase, counter without _total,
	// gauge with _total, duration without seconds, foreign prefix.
	namingCases := []struct {
		name string
		kind metricKind
	}{
		{"orbitjob_UpperCase_total", kindCounter},
		{"orbitjob_missing_suffix", kindCounter},
		{"orbitjob_not_a_counter_total", kindGauge},
		{"orbitjob_duration_ms", kindHistogram},
		{"foreign_prefix_total", kindCounter},
	}
	for _, tc := range namingCases {
		if violations := namingViolations(tc.name, tc.kind); len(violations) == 0 {
			t.Fatalf("naming detector accepted known-bad name %q", tc.name)
		}
	}
	for _, ok := range []struct {
		name string
		kind metricKind
	}{
		{"orbitjob_events_total", kindCounter},
		{"orbitjob_queue_depth", kindGaugeVec},
		{"orbitjob_stage_duration_seconds", kindHistogram},
	} {
		if violations := namingViolations(ok.name, ok.kind); len(violations) != 0 {
			t.Fatalf("naming detector rejected known-good name %q: %v", ok.name, violations)
		}
	}
}

// namingViolations re-implements the per-name checks of
// TestMetricNamingConventions as a pure function so the red proof can call
// them without a scan. The convention test and this helper must stay in
// sync; the duplicate logic is deliberate, because the point of the red
// proof is that the checks as written catch bad names.
func namingViolations(name string, kind metricKind) []string {
	var v []string
	if !validMetricName.MatchString(name) {
		v = append(v, "not snake case")
	}
	if strings.Contains(name, "__") || strings.HasSuffix(name, "_") {
		v = append(v, "bad underscores")
	}
	if !strings.HasPrefix(name, namePrefix) {
		v = append(v, "missing prefix")
	}
	isCounter := kind == kindCounter || kind == kindCounterVec
	if isCounter && !strings.HasSuffix(name, "_total") {
		v = append(v, "counter without _total")
	}
	if !isCounter && strings.HasSuffix(name, "_total") {
		v = append(v, "_total on non-counter")
	}
	if (strings.Contains(name, "duration") || strings.Contains(name, "latency")) && !strings.HasSuffix(name, "_seconds") {
		v = append(v, "duration not in seconds")
	}
	return v
}
