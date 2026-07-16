package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPrometheusClient_Query(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") == "up" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"value":[1234567890,"42"]}]}}`))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer ts.Close()

	client := NewPrometheusClient(ts.URL)
	v, err := client.Query(context.Background(), "up")
	if err != nil {
		t.Fatalf("Query error: %v", err)
	}
	if v != 42 {
		t.Fatalf("Query = %v, want 42", v)
	}
}

func TestPaceController_BoostsWhenBehind(t *testing.T) {
	cfg := FeedbackConfig{
		PrometheusURL:         "http://localhost:9090",
		SampleIntervalSec:     15,
		LatencyThresholdSec:   0.5,
		HighPressureThreshold: 0.7,
		LowPressureThreshold:  0.3,
		MinPace:               0.5,
		MaxPace:               1.5,
	}
	params := TuningParameters{EffectiveWorkerCapacityMax: 10}
	pc := NewPaceController(cfg, params, 10000, 4*time.Hour)

	if pc.Pace() != 1.0 {
		t.Fatalf("initial pace = %v, want 1.0", pc.Pace())
	}

	// Initialize baseline.
	_, _, err := pc.Update(context.Background(), NewPrometheusClient("http://localhost:1"), 10, 2*time.Hour)
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	// Sleep so the next Update sees near-zero recent trigger rate.
	time.Sleep(1100 * time.Millisecond)
	pf, _, err := pc.Update(context.Background(), NewPrometheusClient("http://localhost:1"), 11, 2*time.Hour)
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if pf != cfg.MaxPace {
		t.Fatalf("pace = %v, want max %v", pf, cfg.MaxPace)
	}
}

func TestPaceController_ReducesOnHighPressure(t *testing.T) {
	cfg := FeedbackConfig{
		PrometheusURL:         "http://localhost:9090",
		SampleIntervalSec:     15,
		LatencyThresholdSec:   0.5,
		HighPressureThreshold: 0.5,
		LowPressureThreshold:  0.3,
		MinPace:               0.5,
		MaxPace:               1.5,
	}
	params := TuningParameters{EffectiveWorkerCapacityMax: 10}
	pc := NewPaceController(cfg, params, 0, 4*time.Hour)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		w.Header().Set("Content-Type", "application/json")
		switch q {
		case `sum(orbitjob_dispatcher_queue_depth)`:
			_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"value":[0,"50"]}]}}`))
		case `sum(orbitjob_worker_queue_depth)`:
			_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"value":[0,"10"]}]}}`))
		case `sum(orbitjob_worker_capacity)`:
			_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"value":[0,"10"]}]}}`))
		case `histogram_quantile(0.99, sum(rate(orbitjob_trigger_latency_seconds_bucket[1m])) by (le))`:
			_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"value":[0,"2"]}]}}`))
		default:
			_, _ = w.Write([]byte(`{"status":"success","data":{"result":[]}}`))
		}
	}))
	defer server.Close()

	client := NewPrometheusClient(server.URL)
	pf, _, err := pc.Update(context.Background(), client, 100, time.Minute)
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if pf >= 1.0 {
		t.Fatalf("pace = %v, expected reduction under high pressure", pf)
	}
}
