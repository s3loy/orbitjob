package v1alpha1

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The CRD at charts/orbitjob/crds/workloads.orbitjob.io_jobruns.yaml is the write
// gate in front of the run ledger: the API server rejects a JobRun whose spec
// does not answer "who triggered this run". These tests pin the schema's
// required sets, bounds and enums so that loosening one silently goes red.

const (
	crdContractFile = "../../../../charts/orbitjob/crds/workloads.orbitjob.io_jobruns.yaml"
	ledgerBaseline  = "../../../../db/migrations/0001_baseline.up.sql"
)

func loadCRDDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read CRD %s: %v", path, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse CRD %s: %v", path, err)
	}
	if doc == nil {
		t.Fatalf("CRD %s parsed to an empty document", path)
	}
	return doc
}

func writeMutatedCRD(t *testing.T, doc map[string]any) string {
	t.Helper()
	raw, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("serialize mutated CRD: %v", err)
	}
	path := filepath.Join(t.TempDir(), "workloads.orbitjob.io_jobruns.yaml")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write mutated CRD: %v", err)
	}
	return path
}

// crdWalk navigates the parsed schema without panicking: a missing node becomes
// a violation, never a crash, so the checker stays total under mutation.
type crdWalk struct {
	vs []string
}

func (w *crdWalk) mapAt(from map[string]any, path ...string) map[string]any {
	cur := from
	full := ""
	for _, key := range path {
		if full == "" {
			full = key
		} else {
			full += "." + key
		}
		next, ok := cur[key].(map[string]any)
		if !ok {
			w.vs = append(w.vs, "missing mapping at "+full)
			return map[string]any{}
		}
		cur = next
	}
	return cur
}

func (w *crdWalk) strAt(from map[string]any, key string) string {
	v, _ := from[key].(string)
	return v
}

func (w *crdWalk) intAt(from map[string]any, key string) (int, bool) {
	switch n := from[key].(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	}
	return 0, false
}

func (w *crdWalk) setOf(from map[string]any, key string) map[string]bool {
	out := map[string]bool{}
	items, ok := from[key].([]any)
	if !ok {
		w.vs = append(w.vs, "missing list at "+key)
		return out
	}
	for _, item := range items {
		if s, ok := item.(string); ok {
			out[s] = true
		}
	}
	return out
}

// reportSetDiff appends one violation per missing or unexpected entry, so a
// mutation of the set produces exactly one violation.
func (w *crdWalk) reportSetDiff(where string, got, want map[string]bool) {
	for name := range want {
		if !got[name] {
			w.vs = append(w.vs, where+" must contain "+name)
		}
	}
	for name := range got {
		if !want[name] {
			w.vs = append(w.vs, where+" must not contain "+name)
		}
	}
}

// crdContractViolations returns every way the document departs from the schema
// contract. An empty result means the contract holds.
func crdContractViolations(doc map[string]any) []string {
	w := &crdWalk{}
	if got := w.strAt(doc, "kind"); got != "CustomResourceDefinition" {
		w.vs = append(w.vs, fmt.Sprintf("kind = %q, want CustomResourceDefinition", got))
	}
	crdSpec := w.mapAt(doc, "spec")
	if got := w.strAt(crdSpec, "group"); got != "workloads.orbitjob.io" {
		w.vs = append(w.vs, fmt.Sprintf("spec.group = %q, want workloads.orbitjob.io", got))
	}

	versions, ok := crdSpec["versions"].([]any)
	if !ok || len(versions) != 1 {
		w.vs = append(w.vs, "spec.versions must hold exactly the v1alpha1 version")
		return w.vs
	}
	version, ok := versions[0].(map[string]any)
	if !ok {
		w.vs = append(w.vs, "spec.versions[0] must be a mapping")
		return w.vs
	}
	if got := w.strAt(version, "name"); got != "v1alpha1" {
		w.vs = append(w.vs, fmt.Sprintf("version name = %q, want v1alpha1", got))
	}
	if served, ok := version["served"].(bool); !ok || !served {
		w.vs = append(w.vs, "v1alpha1 must be served")
	}
	if storage, ok := version["storage"].(bool); !ok || !storage {
		w.vs = append(w.vs, "v1alpha1 must be the storage version")
	}

	root := w.mapAt(version, "schema", "openAPIV3Schema")
	w.reportSetDiff("the root schema required", w.setOf(root, "required"), map[string]bool{"spec": true})

	rootProps := w.mapAt(root, "properties")
	specNode := w.mapAt(rootProps, "spec")
	specRequired := map[string]bool{
		"scheduledJobRef": true, "definitionRevision": true, "trigger": true,
		"actor": true, "occurrenceKey": true,
	}
	w.reportSetDiff("spec.required", w.setOf(specNode, "required"), specRequired)

	props := w.mapAt(specNode, "properties")

	sjr := w.mapAt(props, "scheduledJobRef")
	w.reportSetDiff("spec.scheduledJobRef.required", w.setOf(sjr, "required"), map[string]bool{"name": true, "uid": true})
	sjrProps := w.mapAt(sjr, "properties")
	for _, field := range []string{"name", "uid"} {
		p := w.mapAt(sjrProps, field)
		if min, ok := w.intAt(p, "minLength"); !ok || min < 1 {
			w.vs = append(w.vs, fmt.Sprintf("spec.scheduledJobRef.%s.minLength must be at least 1", field))
		}
	}

	revisionNode := w.mapAt(props, "definitionRevision")
	if got := w.strAt(revisionNode, "type"); got != "integer" {
		w.vs = append(w.vs, fmt.Sprintf("spec.definitionRevision.type = %q, want integer", got))
	}
	if min, ok := w.intAt(revisionNode, "minimum"); !ok || min < 1 {
		w.vs = append(w.vs, "spec.definitionRevision.minimum must be at least 1: revision ids start at 1")
	}

	trigger := w.mapAt(props, "trigger")
	// The enum the ledger can actually answer. Schedule, Manual, Retry and
	// Check shipped with the checks workstream; Workflow and Function land
	// with the workflow walker and the function invoke surface. A value here
	// without its producer is the drift this pin exists to catch.
	w.reportSetDiff("spec.trigger.enum", w.setOf(trigger, "enum"),
		map[string]bool{
			"Schedule": true, "Manual": true, "Retry": true, "Check": true,
			"Workflow": true, "Function": true,
		})

	actor := w.mapAt(props, "actor")
	if got := w.strAt(actor, "type"); got != "string" {
		w.vs = append(w.vs, fmt.Sprintf("spec.actor.type = %q, want string", got))
	}
	if min, ok := w.intAt(actor, "minLength"); !ok || min < 1 {
		w.vs = append(w.vs, "spec.actor.minLength must be at least 1: the ledger column is NOT NULL with a non-empty CHECK")
	}
	if max, ok := w.intAt(actor, "maxLength"); !ok || max != 255 {
		w.vs = append(w.vs, fmt.Sprintf("spec.actor.maxLength must equal the run ledger actor column width 255, got %d", max))
	}

	occ := w.mapAt(props, "occurrenceKey")
	if min, ok := w.intAt(occ, "minLength"); !ok || min < 1 {
		w.vs = append(w.vs, "spec.occurrenceKey.minLength must be at least 1: the ledger deduplicates on it")
	}

	timeout := w.mapAt(props, "timeoutSeconds")
	if got := w.strAt(timeout, "type"); got != "integer" {
		w.vs = append(w.vs, fmt.Sprintf("spec.timeoutSeconds.type = %q, want integer", got))
	}
	if min, ok := w.intAt(timeout, "minimum"); !ok || min != 0 {
		w.vs = append(w.vs, "spec.timeoutSeconds.minimum must be 0: no timeout is expressed as 0, not negative")
	}

	cancel := w.mapAt(props, "cancelRequested")
	if got := w.strAt(cancel, "type"); got != "boolean" {
		w.vs = append(w.vs, fmt.Sprintf("spec.cancelRequested.type = %q, want boolean: the stop intent is a flag, not free text", got))
	}

	return w.vs
}

// ledgerActorColumnWidth extracts the width of job_run_control_plane.actor from
// the baseline migration (db/migrations/0001_baseline.up.sql:275). The match is
// anchored below the CREATE TABLE line so the other actor column
// (job_definition_revisions.actor, line 242) cannot satisfy it.
func ledgerActorColumnWidth(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read baseline migration %s: %v", path, err)
	}
	table := "CREATE TABLE IF NOT EXISTS job_run_control_plane"
	idx := strings.Index(string(raw), table)
	if idx < 0 {
		t.Fatalf("baseline migration no longer declares %s; re-anchor the width pin", table)
	}
	tail := string(raw)[idx:]
	re := regexp.MustCompile(`actor\s+VARCHAR\((\d+)\)`)
	m := re.FindStringSubmatch(tail)
	if m == nil {
		t.Fatalf("no actor VARCHAR(n) column under %s; re-anchor the width pin", table)
	}
	width, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("parse actor column width %q: %v", m[1], err)
	}
	return width
}

func crdActorMaxLength(t *testing.T, doc map[string]any) int {
	t.Helper()
	actor, ok := doc["spec"].(map[string]any)["versions"].([]any)[0].(map[string]any)["schema"].(map[string]any)["openAPIV3Schema"].(map[string]any)["properties"].(map[string]any)["spec"].(map[string]any)["properties"].(map[string]any)["actor"].(map[string]any)
	if !ok {
		t.Fatal("spec.actor is not a mapping; the width pin lost its target")
	}
	max, ok := actor["maxLength"].(int)
	if !ok {
		t.Fatalf("spec.actor.maxLength = %v, want an integer", actor["maxLength"])
	}
	return max
}

func TestCRDSchemaContractHoldsOnTheShippedCRD(t *testing.T) {
	if violations := crdContractViolations(loadCRDDoc(t, crdContractFile)); len(violations) > 0 {
		t.Fatalf("the shipped CRD departs from the schema contract:\n%s", strings.Join(violations, "\n"))
	}
}

func TestCRDActorWidthMatchesTheLedgerColumn(t *testing.T) {
	crdWidth := crdActorMaxLength(t, loadCRDDoc(t, crdContractFile))
	sqlWidth := ledgerActorColumnWidth(t, ledgerBaseline)
	if crdWidth != sqlWidth {
		t.Fatalf("CRD spec.actor.maxLength = %d but job_run_control_plane.actor is VARCHAR(%d): widen both or neither",
			crdWidth, sqlWidth)
	}
}

// TestCRDSchemaContractRedProofs mutates a copy of the CRD in a temp directory
// and requires the checker to catch each mutation with exactly one violation.
// A case that produced no violation would mean the assertion it targets cannot
// fail and is therefore not a test.
func TestCRDSchemaContractRedProofs(t *testing.T) {
	tests := []struct {
		name          string
		wantViolation string
		mutate        func(doc map[string]any)
	}{
		{
			name:          "actor dropped from spec.required",
			wantViolation: "spec.required must contain actor",
			mutate: func(doc map[string]any) {
				mutateListRemove(t, doc, "actor",
					"spec", "versions", 0, "schema", "openAPIV3Schema", "properties", "spec", "required")
			},
		},
		{
			name:          "actor maxLength widened off the ledger column",
			wantViolation: "spec.actor.maxLength must equal the run ledger actor column width 255, got 1024",
			mutate: func(doc map[string]any) {
				mutateSet(t, doc, 1024,
					"spec", "versions", 0, "schema", "openAPIV3Schema", "properties", "spec", "properties", "actor", "maxLength")
			},
		},
		{
			name:          "actor minLength dropped",
			wantViolation: "spec.actor.minLength must be at least 1",
			mutate: func(doc map[string]any) {
				mutateSet(t, doc, 0,
					"spec", "versions", 0, "schema", "openAPIV3Schema", "properties", "spec", "properties", "actor", "minLength")
			},
		},
		{
			name:          "occurrenceKey allowed empty",
			wantViolation: "spec.occurrenceKey.minLength must be at least 1",
			mutate: func(doc map[string]any) {
				mutateSet(t, doc, 0,
					"spec", "versions", 0, "schema", "openAPIV3Schema", "properties", "spec", "properties", "occurrenceKey", "minLength")
			},
		},
		{
			name:          "trigger enum gains an unshipped value",
			wantViolation: "spec.trigger.enum must not contain Random",
			mutate: func(doc map[string]any) {
				mutateListAppend(t, doc, "Random",
					"spec", "versions", 0, "schema", "openAPIV3Schema", "properties", "spec", "properties", "trigger", "enum")
			},
		},
		{
			name:          "trigger enum loses Manual",
			wantViolation: "spec.trigger.enum must contain Manual",
			mutate: func(doc map[string]any) {
				mutateListRemove(t, doc, "Manual",
					"spec", "versions", 0, "schema", "openAPIV3Schema", "properties", "spec", "properties", "trigger", "enum")
			},
		},
		{
			name:          "scheduledJobRef.uid no longer required",
			wantViolation: "spec.scheduledJobRef.required must contain uid",
			mutate: func(doc map[string]any) {
				mutateListRemove(t, doc, "uid",
					"spec", "versions", 0, "schema", "openAPIV3Schema", "properties", "spec", "properties", "scheduledJobRef", "required")
			},
		},
		{
			name:          "definitionRevision accepts zero",
			wantViolation: "spec.definitionRevision.minimum must be at least 1",
			mutate: func(doc map[string]any) {
				mutateSet(t, doc, 0,
					"spec", "versions", 0, "schema", "openAPIV3Schema", "properties", "spec", "properties", "definitionRevision", "minimum")
			},
		},
		{
			name:          "cancelRequested stops being boolean",
			wantViolation: "spec.cancelRequested.type = \"string\", want boolean",
			mutate: func(doc map[string]any) {
				mutateSet(t, doc, "string",
					"spec", "versions", 0, "schema", "openAPIV3Schema", "properties", "spec", "properties", "cancelRequested", "type")
			},
		},
		{
			name:          "spec itself becomes optional",
			wantViolation: "the root schema required must contain spec",
			mutate: func(doc map[string]any) {
				mutateListRemove(t, doc, "spec",
					"spec", "versions", 0, "schema", "openAPIV3Schema", "required")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := loadCRDDoc(t, crdContractFile)
			tt.mutate(doc)
			violations := crdContractViolations(loadCRDDoc(t, writeMutatedCRD(t, doc)))
			if len(violations) != 1 {
				t.Fatalf("want exactly one violation, got %d: %v", len(violations), violations)
			}
			if !strings.Contains(violations[0], tt.wantViolation) {
				t.Fatalf("violation %q does not report %q", violations[0], tt.wantViolation)
			}
		})
	}
}

// TestCRDActorWidthPinRedProof mutates a copy of the baseline migration so the
// run ledger column is wider than the CRD bound, and requires the width pin to
// detect the divergence. It also proves the extraction is anchored to
// job_run_control_plane and not satisfied by the other actor column.
func TestCRDActorWidthPinRedProof(t *testing.T) {
	raw, err := os.ReadFile(ledgerBaseline)
	if err != nil {
		t.Fatalf("read baseline migration: %v", err)
	}
	table := "CREATE TABLE IF NOT EXISTS job_run_control_plane"
	idx := strings.Index(string(raw), table)
	if idx < 0 {
		t.Fatalf("baseline migration no longer declares %s", table)
	}
	mutated := string(raw)[:idx] +
		strings.Replace(string(raw)[idx:], "actor VARCHAR(255) NOT NULL", "actor VARCHAR(512) NOT NULL", 1)
	if mutated == string(raw) {
		t.Fatal("the run table's actor column no longer reads VARCHAR(255); update the red proof")
	}
	path := filepath.Join(t.TempDir(), "0001_baseline.up.sql")
	if err := os.WriteFile(path, []byte(mutated), 0o644); err != nil {
		t.Fatalf("write mutated migration: %v", err)
	}

	crdWidth := crdActorMaxLength(t, loadCRDDoc(t, crdContractFile))
	sqlWidth := ledgerActorColumnWidth(t, path)
	if sqlWidth != 512 {
		t.Fatalf("mutated ledger width = %d, want 512; the extraction is not reading the run table", sqlWidth)
	}
	if sqlWidth == crdWidth {
		t.Fatalf("width pin cannot fail: CRD %d equals mutated ledger %d", crdWidth, sqlWidth)
	}
}

// --- navigation for mutations ------------------------------------------------

// walkTo steps through mappings by string and lists by int index, failing the
// test when the shape has drifted rather than silently skipping a mutation.
func walkTo(t *testing.T, doc map[string]any, path ...any) any {
	t.Helper()
	cur := any(doc)
	for i, step := range path {
		switch s := step.(type) {
		case string:
			m, ok := cur.(map[string]any)
			if !ok {
				t.Fatalf("cannot apply mutation at step %d (%q): parent is not a mapping", i, s)
			}
			cur = m[s]
		case int:
			l, ok := cur.([]any)
			if !ok {
				t.Fatalf("cannot apply mutation at step %d: parent is not a list", i)
			}
			if s < 0 || s >= len(l) {
				t.Fatalf("cannot apply mutation at step %d: index %d out of range", i, s)
			}
			cur = l[s]
		default:
			t.Fatalf("unsupported mutation step %v", step)
		}
	}
	return cur
}

func mutateSet(t *testing.T, doc map[string]any, value any, path ...any) {
	t.Helper()
	parent, ok := walkTo(t, doc, path[:len(path)-1]...).(map[string]any)
	if !ok {
		t.Fatal("mutation target parent is not a mapping")
	}
	key, ok := path[len(path)-1].(string)
	if !ok {
		t.Fatal("mutation target key must be a string")
	}
	parent[key] = value
}

func mutateListRemove(t *testing.T, doc map[string]any, target string, path ...any) {
	t.Helper()
	parent, ok := walkTo(t, doc, path[:len(path)-1]...).(map[string]any)
	if !ok {
		t.Fatal("mutation target parent is not a mapping")
	}
	key, ok := path[len(path)-1].(string)
	if !ok {
		t.Fatal("mutation target key must be a string")
	}
	items, ok := parent[key].([]any)
	if !ok {
		t.Fatalf("mutation target %q is not a list", key)
	}
	out := items[:0]
	removed := false
	for _, item := range items {
		if s, isStr := item.(string); isStr && s == target {
			removed = true
			continue
		}
		out = append(out, item)
	}
	if !removed {
		t.Fatalf("mutation target %q not present in the list", target)
	}
	parent[key] = out
}

func mutateListAppend(t *testing.T, doc map[string]any, value string, path ...any) {
	t.Helper()
	parent, ok := walkTo(t, doc, path[:len(path)-1]...).(map[string]any)
	if !ok {
		t.Fatal("mutation target parent is not a mapping")
	}
	key, ok := path[len(path)-1].(string)
	if !ok {
		t.Fatal("mutation target key must be a string")
	}
	items, _ := parent[key].([]any)
	parent[key] = append(items, value)
}
