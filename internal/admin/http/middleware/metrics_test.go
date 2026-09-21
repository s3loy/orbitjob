package middleware

import (
	stdhttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"

	"orbitjob/internal/platform/metrics"
)

func newMetricsRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestMetrics())
	r.GET("/api/v1/jobs/:id", func(c *gin.Context) {
		c.JSON(stdhttp.StatusOK, gin.H{})
	})
	return r
}

func TestRequestMetrics_CountsWithRouteTemplate(t *testing.T) {
	r := newMetricsRouter()

	// The counter is process-global; a fixed label tuple per test keeps the
	// assertions independent of whatever other requests ran before.
	before := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("GET", "/api/v1/jobs/:id", "200"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/jobs/run_123", nil)
	r.ServeHTTP(w, req)

	if w.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	after := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("GET", "/api/v1/jobs/:id", "200"))
	if after-before != 1 {
		t.Fatalf("expected the request to be counted once, delta %v", after-before)
	}
	// The path label must be the route template: a raw id in the label would
	// grow the series without bound.
	if raw := testutil.CollectAndCount(metrics.HTTPRequestsTotal, "orbitjob_http_requests_total"); raw == 0 {
		t.Fatal("expected the metric family to be collected")
	}
}

func TestRequestMetrics_CountsUnmatchedRoutesAsConstant(t *testing.T) {
	r := newMetricsRouter()

	before := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("GET", unmatchedRoute, "404"))

	// Each request carries a distinct raw path: if the middleware labeled by
	// the raw path instead of the route template, every one of these would
	// mint a new series.
	for _, raw := range []string{"/api/v1/nope/1", "/api/v1/nope/2", "/api/v1/nope/3"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(stdhttp.MethodGet, raw, nil))
		if w.Code != stdhttp.StatusNotFound {
			t.Fatalf("expected 404 for %s, got %d", raw, w.Code)
		}
	}

	after := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("GET", unmatchedRoute, "404"))
	if after-before != 3 {
		t.Fatalf("expected 3 unmatched requests counted under one label, delta %v", after-before)
	}
}

func TestRequestMetrics_ExpositionMatchesMetricsEndpoint(t *testing.T) {
	r := newMetricsRouter()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(stdhttp.MethodGet, "/api/v1/jobs/run_abc", nil))

	// The default gatherer is what gin.WrapH(promhttp.Handler()) serves at
	// /metrics, so gathering from it pins the family exactly as the endpoint
	// renders it: name, help, counter type, and the label set for this
	// request. Values are not pinned: the counter is process-global and other
	// tests legitimately share it.
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "orbitjob_http_requests_total" {
			continue
		}
		if got, want := mf.GetHelp(), "Total HTTP requests served by the admin API, counted by request method, route path template, and response status code"; got != want {
			t.Fatalf("unexpected help: %q", got)
		}
		if mf.GetType() != dto.MetricType_COUNTER {
			t.Fatalf("expected counter type, got %v", mf.GetType())
		}
		for _, m := range mf.GetMetric() {
			labels := map[string]string{}
			for _, lp := range m.GetLabel() {
				labels[lp.GetName()] = lp.GetValue()
			}
			if labels["method"] == "GET" && labels["path"] == "/api/v1/jobs/:id" && labels["status"] == "200" {
				if m.GetCounter().GetValue() < 1 {
					t.Fatal("expected the request to be counted at least once")
				}
				return
			}
		}
		t.Fatal("no series carried the expected method/path/status labels")
	}
	t.Fatal("orbitjob_http_requests_total family missing from the default gatherer")
}
