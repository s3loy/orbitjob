package metrics

// Governance scan infrastructure for the metrics package.
//
// The tests built on this file pin the platform's metric governance rules:
//
//  1. Every exported metric variable has at least one producer outside
//     internal/platform/metrics. This is the rule that would have caught the
//     19 dead metrics deleted in the 2026-09 metric surface rebuild.
//  2. Labels are bounded: no metric declares, and no producer passes, a
//     tenant id, run id, or user id. Label values come from fixed small sets
//     such as resource names (scheduledjobs|jobruns|jobs).
//  3. Help text is present, non-empty, and describes the declared labels.
//  4. Metric names are unique, follow Prometheus naming conventions, and are
//     registered in the default registry that promhttp.Handler serves
//     (internal/platform/health/server.go:19, cmd/admin-api/main.go:91).
//
// Everything is discovered at run time: declarations are parsed from this
// package's own sources and producers are walked from internal/ and cmd/, so
// a metric added or deleted concurrently is picked up on the next run. Files
// excluded by //go:build constraints are skipped on both sides (etcd.go
// carries //go:build etcd), mirroring what the default build compiles.
// Known limitation: a run with -tags etcd links the etcd metrics, but the
// scan still enumerates only the default-build surface.

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type metricKind string

const (
	kindCounter      metricKind = "counter"
	kindCounterVec   metricKind = "counter_vec"
	kindGauge        metricKind = "gauge"
	kindGaugeVec     metricKind = "gauge_vec"
	kindHistogram    metricKind = "histogram"
	kindHistogramVec metricKind = "histogram_vec"
)

func (k metricKind) isVec() bool { return strings.HasSuffix(string(k), "_vec") }

// metricDecl is one exported metric variable parsed from this package.
type metricDecl struct {
	Var    string     // exported Go variable name, e.g. APIKeysTotal
	Name   string     // Prometheus metric name, e.g. orbitjob_api_keys_total
	Help   string     // declared Help string ("" if absent; a rule test flags that)
	Labels []string   // declared label names; empty for plain metrics
	Kind   metricKind // counter / gauge / histogram, or the _vec variant
	File   string     // base name of the declaring file
	Line   int        // line of the declaration
}

// prodFile is one build-matching, non-test producer file under internal/ or
// cmd/ that imports this package.
type prodFile struct {
	Path      string
	Src       []byte
	FSet      *token.FileSet
	AST       *ast.File
	Alias     string // local package name used in selectors ("metrics" unless aliased)
	DotImport bool   // import . "…/metrics" (references are bare identifiers)
}

// refSite is any selector reference to a metric variable in a producer file.
type refSite struct {
	Path string
	Line int
}

// labelCallSite is one WithLabelValues-style call on a metric variable.
type labelCallSite struct {
	Var    string
	Path   string
	Line   int
	ArgSrc []string // source text of each argument expression
}

type scanResult struct {
	PkgDir   string
	RepoRoot string
	Decls    []metricDecl
	ByVar    map[string]*metricDecl
	Files    []prodFile
	Refs     map[string][]refSite // metric var -> selector references
	Sites    []labelCallSite      // label call sites across the corpus
}

var (
	scanOnce   sync.Once
	scanShared scanResult
	scanErr    error
)

// loadScan runs the full scan once for the test binary and fails the calling
// test if the scan itself could not be completed. The scan is fail-closed: an
// exported variable in this package that cannot be parsed as a metric is an
// error, not a silent skip, because an unparsable metric is an unaudited one.
func loadScan(t *testing.T) scanResult {
	t.Helper()
	scanOnce.Do(func() { scanShared, scanErr = scanAll() })
	if scanErr != nil {
		t.Fatalf("governance scan failed: %v", scanErr)
	}
	return scanShared
}

func scanAll() (scanResult, error) {
	var res scanResult

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return res, fmt.Errorf("runtime.Caller could not locate this test file")
	}
	res.PkgDir = filepath.Dir(thisFile)
	res.RepoRoot = filepath.Clean(filepath.Join(res.PkgDir, "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(res.RepoRoot, "go.mod")); err != nil {
		return res, fmt.Errorf("repo root %s does not contain go.mod: %w", res.RepoRoot, err)
	}

	decls, err := parseMetricsPackage(res.PkgDir)
	if err != nil {
		return res, err
	}
	if len(decls) == 0 {
		return res, fmt.Errorf("no metric declarations found in %s; the scan refuses to audit an empty surface", res.PkgDir)
	}
	res.Decls = decls
	res.ByVar = make(map[string]*metricDecl, len(decls))
	for i := range decls {
		res.ByVar[decls[i].Var] = &decls[i]
	}

	files, refs, sites, err := scanProducers(res.RepoRoot, res.PkgDir, res.ByVar)
	if err != nil {
		return res, err
	}
	res.Files = files
	res.Refs = refs
	res.Sites = sites
	return res, nil
}

// parseMetricsPackage parses every build-matching non-test .go file in the
// metrics package directory and extracts the exported metric declarations.
func parseMetricsPackage(dir string) ([]metricDecl, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read metrics package dir: %w", err)
	}
	var decls []metricDecl
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		match, err := build.Default.MatchFile(dir, name)
		if err != nil {
			return nil, fmt.Errorf("build constraints for %s: %w", name, err)
		}
		if !match {
			continue // excluded from the default build (e.g. //go:build etcd)
		}
		got, err := parseMetricDecls(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		decls = append(decls, got...)
	}
	return decls, nil
}

var constructorKinds = map[string]metricKind{
	"NewCounter":      kindCounter,
	"NewCounterVec":   kindCounterVec,
	"NewGauge":        kindGauge,
	"NewGaugeVec":     kindGaugeVec,
	"NewHistogram":    kindHistogram,
	"NewHistogramVec": kindHistogramVec,
}

func parseMetricDecls(path string) ([]metricDecl, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	base := filepath.Base(path)
	var decls []metricDecl
	for _, d := range f.Decls {
		gen, ok := d.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != len(vs.Values) {
				continue
			}
			for i, nameIdent := range vs.Names {
				if !nameIdent.IsExported() {
					continue
				}
				decl, err := declFromValue(fset, base, nameIdent.Name, vs.Values[i])
				if err != nil {
					return nil, fmt.Errorf("%s: %w", path, err)
				}
				decls = append(decls, decl)
			}
		}
	}
	return decls, nil
}

func declFromValue(fset *token.FileSet, file string, varName string, value ast.Expr) (metricDecl, error) {
	decl := metricDecl{Var: varName, File: file, Line: fset.Position(value.Pos()).Line}
	call, ok := value.(*ast.CallExpr)
	if !ok {
		return decl, fmt.Errorf("var %s is exported but not a promauto.New* call; it would be invisible to governance auditing", varName)
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return decl, fmt.Errorf("var %s: unsupported constructor shape", varName)
	}
	kind, ok := constructorKinds[sel.Sel.Name]
	if !ok {
		return decl, fmt.Errorf("var %s: constructor %s is not a known promauto metric constructor", varName, sel.Sel.Name)
	}
	decl.Kind = kind
	wantArgs := 1
	if kind.isVec() {
		wantArgs = 2
	}
	if len(call.Args) != wantArgs {
		return decl, fmt.Errorf("var %s: expected %d argument(s) for %s, found %d", varName, wantArgs, sel.Sel.Name, len(call.Args))
	}

	opts, ok := call.Args[0].(*ast.CompositeLit)
	if !ok {
		return decl, fmt.Errorf("var %s: first argument is not a struct literal of options", varName)
	}
	for _, elt := range opts.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, _ := kv.Key.(*ast.Ident)
		lit, ok := kv.Value.(*ast.BasicLit)
		if !ok || key == nil || lit.Kind != token.STRING {
			continue
		}
		val, uerr := strconv.Unquote(lit.Value)
		if uerr != nil {
			return decl, fmt.Errorf("var %s: unquote option %s: %w", varName, key.Name, uerr)
		}
		switch key.Name {
		case "Name":
			decl.Name = val
		case "Help":
			decl.Help = val
		}
	}
	if decl.Name == "" {
		return decl, fmt.Errorf("var %s declares no metric Name", varName)
	}

	if kind.isVec() {
		labels, ok := call.Args[1].(*ast.CompositeLit)
		if !ok {
			return decl, fmt.Errorf("var %s: second argument is not a []string literal of label names", varName)
		}
		for _, elt := range labels.Elts {
			lit, ok := elt.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return decl, fmt.Errorf("var %s: label names must be string literals", varName)
			}
			val, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return decl, fmt.Errorf("var %s: unquote label name: %w", varName, uerr)
			}
			decl.Labels = append(decl.Labels, val)
		}
	}
	return decl, nil
}

const metricsImportNeedle = "platform/metrics"

// scanProducers walks internal/ and cmd/ for build-matching, non-test files
// that import this package, and collects every selector reference and label
// call site. Concurrent deletions are tolerated: a file or directory that
// disappears mid-walk is skipped, so the corpus is whatever exists at read
// time.
func scanProducers(repoRoot, pkgDir string, byVar map[string]*metricDecl) ([]prodFile, map[string][]refSite, []labelCallSite, error) {
	roots := []string{
		filepath.Join(repoRoot, "internal"),
		filepath.Join(repoRoot, "cmd"),
	}
	var files []prodFile
	for _, root := range roots {
		if _, err := os.Stat(root); err != nil {
			continue // tree absent; nothing to walk
		}
		werr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() {
					return filepath.SkipDir
				}
				return nil // vanished mid-walk; audit what exists
			}
			name := d.Name()
			if d.IsDir() {
				switch name {
				case ".git", "testdata", "vendor", "node_modules":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			if filepath.Dir(path) == pkgDir {
				return nil // declarations are not producers of themselves
			}
			match, merr := build.Default.MatchFile(filepath.Dir(path), name)
			if merr != nil || !match {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil || !bytes.Contains(data, []byte(metricsImportNeedle)) {
				return nil
			}
			pf, perr := parseProducer(path, data)
			if perr != nil {
				return perr
			}
			if pf.Alias == "" && !pf.DotImport {
				return nil // import path mentioned in a string or comment only
			}
			files = append(files, pf)
			return nil
		})
		if werr != nil {
			return nil, nil, nil, werr
		}
	}

	refs := make(map[string][]refSite)
	var sites []labelCallSite
	for _, pf := range files {
		collectRefsAndSites(pf, byVar, refs, &sites)
	}
	return files, refs, sites, nil
}

func parseProducer(path string, data []byte) (prodFile, error) {
	pf := prodFile{Path: path, Src: data, FSet: token.NewFileSet()}
	f, err := parser.ParseFile(pf.FSet, path, data, 0)
	if err != nil {
		return pf, fmt.Errorf("parse %s: %w", path, err)
	}
	pf.AST = f
	for _, imp := range f.Imports {
		pathVal, uerr := strconv.Unquote(imp.Path.Value)
		if uerr != nil || !strings.Contains(pathVal, metricsImportNeedle) {
			continue
		}
		switch {
		case imp.Name == nil:
			pf.Alias = "metrics" // the imported package's declared name
		case imp.Name.Name == ".":
			pf.DotImport = true
		default:
			pf.Alias = imp.Name.Name
		}
	}
	return pf, nil
}

// collectRefsAndSites records every selector reference to a known metric var
// (evidence for the producer-coverage rule) and every WithLabelValues-style
// call with its argument source text (input to the label audits).
func collectRefsAndSites(pf prodFile, byVar map[string]*metricDecl, refs map[string][]refSite, sites *[]labelCallSite) {
	ast.Inspect(pf.AST, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.SelectorExpr:
			if v := resolveMetricVar(pf, node.X, node.Sel.Name, byVar); v != "" {
				refs[v] = append(refs[v], refSite{Path: pf.Path, Line: pf.FSet.Position(node.Pos()).Line})
			}
			return true
		case *ast.Ident:
			if pf.DotImport && byVar[node.Name] != nil {
				refs[node.Name] = append(refs[node.Name], refSite{Path: pf.Path, Line: pf.FSet.Position(node.Pos()).Line})
			}
			return true
		case *ast.CallExpr:
			sel, ok := node.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "WithLabelValues", "GetMetricWithLabelValues":
				v := resolveMetricVar(pf, sel.X, "", byVar)
				if v == "" {
					return true
				}
				site := labelCallSite{Var: v, Path: pf.Path, Line: pf.FSet.Position(node.Pos()).Line}
				for _, arg := range node.Args {
					start := pf.FSet.Position(arg.Pos()).Offset
					end := pf.FSet.Position(arg.End()).Offset
					if start >= 0 && end <= len(pf.Src) && start < end {
						site.ArgSrc = append(site.ArgSrc, string(pf.Src[start:end]))
					}
				}
				*sites = append(*sites, site)
			}
			return true
		}
		return true
	})
}

// resolveMetricVar matches a receiver expression such as `metrics.APIKeysTotal`
// (or, for dot imports, a bare identifier) against the declared metric vars.
// recv may be an ast.Expr when only the var identity matters.
func resolveMetricVar(pf prodFile, recv ast.Expr, selName string, byVar map[string]*metricDecl) string {
	switch x := recv.(type) {
	case *ast.SelectorExpr:
		// Chained receiver like promauto.With(...).NewCounterVec is not a var.
		if id, ok := x.X.(*ast.Ident); ok && id.Name == pf.Alias {
			if byVar[x.Sel.Name] != nil {
				return x.Sel.Name
			}
		}
	case *ast.Ident:
		if x.Name == pf.Alias && selName != "" && byVar[selName] != nil {
			return selName
		}
	}
	return ""
}

var identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// identifiersIn returns every Go identifier appearing in a source fragment.
func identifiersIn(src string) []string {
	return identRe.FindAllString(src, -1)
}

// normalizeToken reduces an identifier or label name to lowercase
// alphanumeric characters so that "TenantID", "tenant_id" and "tenant-id"
// compare equal.
func normalizeToken(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// denySubstr matches identifier shapes that carry unbounded cardinality:
// anything tenant-, run-id- or user-id-shaped. The list is deliberately
// conservative; new shapes go here with a comment saying why.
var denySubstr = []string{"tenant", "runid", "userid", "accountid", "ownerid", "orgid", "memberid"}

// denyExact matches bare identifiers that alone are cardinality hazards when
// used as a label value, including the generic "id" (a raw row or object id).
var denyExact = []string{"run", "user", "owner", "account", "org", "id"}

// deniedIdentifiers returns the identifiers in src that violate the bounded
// label-value rule.
func deniedIdentifiers(src string) []string {
	var bad []string
	for _, ident := range identifiersIn(src) {
		n := normalizeToken(ident)
		denied := false
		for _, d := range denySubstr {
			if strings.Contains(n, d) {
				denied = true
				break
			}
		}
		if !denied {
			for _, d := range denyExact {
				if n == d {
					denied = true
					break
				}
			}
		}
		if denied {
			bad = append(bad, ident)
		}
	}
	return bad
}

// labelsMissingFromHelp returns declared labels whose normalized name does
// not appear in the normalized help text.
func labelsMissingFromHelp(help string, labels []string) []string {
	hn := normalizeToken(help)
	var missing []string
	for _, l := range labels {
		if !strings.Contains(hn, normalizeToken(l)) {
			missing = append(missing, l)
		}
	}
	return missing
}

// duplicateMetricNames maps a metric name to how often it is declared.
func duplicateMetricNames(decls []metricDecl) map[string]int {
	counts := make(map[string]int)
	for _, d := range decls {
		counts[d.Name]++
	}
	for name, n := range counts {
		if n < 2 {
			delete(counts, name)
		}
	}
	return counts
}

// unreferencedMetrics returns declarations with no selector reference in the
// producer corpus: exported metrics no production code touches.
func unreferencedMetrics(decls []metricDecl, refs map[string][]refSite) []metricDecl {
	var dead []metricDecl
	for _, d := range decls {
		if len(refs[d.Var]) == 0 {
			dead = append(dead, d)
		}
	}
	return dead
}
