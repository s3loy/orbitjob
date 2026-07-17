package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// PrometheusClient queries a Prometheus HTTP API.
type PrometheusClient struct {
	baseURL string
	client  *http.Client
}

func NewPrometheusClient(baseURL string) *PrometheusClient {
	return &PrometheusClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *PrometheusClient) Query(ctx context.Context, q string) (float64, error) {
	u, err := url.Parse(c.baseURL + "/api/v1/query")
	if err != nil {
		return 0, err
	}
	v := url.Values{}
	v.Set("query", q)
	u.RawQuery = v.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("prometheus query %q: status %d", q, resp.StatusCode)
	}

	var body struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Value []any `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, fmt.Errorf("decode prometheus response: %w", err)
	}
	if body.Status != "success" {
		return 0, fmt.Errorf("prometheus query failed: %s", body.Status)
	}
	if len(body.Data.Result) == 0 || len(body.Data.Result[0].Value) < 2 {
		return 0, nil
	}
	s, ok := body.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("unexpected prometheus value type")
	}
	v64, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parse prometheus value %q: %w", s, err)
	}
	return v64, nil
}

// PaceController adjusts a pace factor based on Prometheus signals and progress
// toward the qualification minimum.
type PaceController struct {
	mu           sync.Mutex
	cfg          FeedbackConfig
	params       TuningParameters
	minInstances int
	duration     time.Duration

	paceFactor  float64
	dynamicMax  float64
	emaPressure float64

	lastUpdate       time.Time
	lastAttempted    int64
	consecQueryError int
}

func NewPaceController(cfg FeedbackConfig, params TuningParameters, minInstances int, duration time.Duration) *PaceController {
	return &PaceController{
		cfg:          cfg,
		params:       params,
		minInstances: minInstances,
		duration:     duration,
		lastUpdate:   time.Now(),
		paceFactor:   1.0,
		dynamicMax:   cfg.MaxPace,
	}
}

// Pace returns the current pace factor. Values >1 run faster than the nominal
// schedule; values <1 stretch the schedule.
func (pc *PaceController) Pace() float64 {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	return pc.paceFactor
}

// Update samples Prometheus and the engine's attempt/accept counters, then
// updates the pace factor. Query errors are counted: after three consecutive
// samples with failures the pace freezes instead of accelerating into an
// unobserved system. Empty results (metric missing) read as zero, which is
// distinct from a failed query.
func (pc *PaceController) Update(ctx context.Context, client *PrometheusClient, attempted, accepted int64, elapsed time.Duration) (float64, float64, error) {
	now := time.Now()
	dt := now.Sub(pc.lastUpdate).Seconds()
	if dt <= 0 {
		dt = 1
	}
	currentAttemptRate := float64(attempted-pc.lastAttempted) / dt
	pc.lastUpdate = now
	pc.lastAttempted = attempted

	queryErrs := 0
	query := func(q string) float64 {
		v, err := client.Query(ctx, q)
		if err != nil {
			queryErrs++
		}
		return v
	}
	queueDepth := query("sum(orbitjob_dispatcher_queue_depth)")
	workerDepth := query("sum(orbitjob_worker_queue_depth)")
	workerCapacity := query("sum(orbitjob_worker_capacity)")
	latency := query("histogram_quantile(0.99, sum(rate(orbitjob_trigger_latency_seconds_bucket[5m])) by (le))")
	rateLimitHits := query("sum(rate(orbitjob_ratelimit_hits_total{endpoint_group=\"trigger\"}[1m]))")
	poolRejected := query("sum(rate(orbitjob_worker_pool_rejected_total[1m]))")

	if queryErrs > 0 {
		pc.consecQueryError++
	} else {
		pc.consecQueryError = 0
	}
	if pc.consecQueryError >= 3 {
		pc.mu.Lock()
		pf := pc.paceFactor
		pc.mu.Unlock()
		return pf, pc.emaPressure, fmt.Errorf("prometheus queries failed for %d consecutive samples; pace frozen at %.2f", pc.consecQueryError, pf)
	}

	cap := float64(pc.params.EffectiveWorkerCapacityMax)
	if cap < 1 {
		cap = 1
	}
	queueRatio := (queueDepth + workerDepth) / (cap * 2)
	if queueRatio > 1 {
		queueRatio = 1
	}

	latencyRatio := latency / pc.cfg.LatencyThresholdSec
	if latencyRatio > 1 {
		latencyRatio = 1
	}

	var rateLimitRatio, rejectionRatio float64
	if currentAttemptRate > 0 {
		rateLimitRatio = rateLimitHits / currentAttemptRate
		if rateLimitRatio > 1 {
			rateLimitRatio = 1
		}
		rejectionRatio = poolRejected / currentAttemptRate
		if rejectionRatio > 1 {
			rejectionRatio = 1
		}
	}

	// Adaptive capacity backoff is a pressure signal: when the controller has
	// reduced capacity below the max, it did so because the DB is hot. A
	// missing series (static mode) reads as full capacity, i.e. no signal.
	capacityRatio := 0.0
	if workerCapacity > 0 && workerCapacity < cap {
		capacityRatio = 1.0 - workerCapacity/cap
	}

	pressure := 0.4*queueRatio + 0.3*latencyRatio + 0.15*rateLimitRatio + 0.1*rejectionRatio + 0.05*capacityRatio
	if pressure > 1 {
		pressure = 1
	}

	// EMA smoothing to avoid oscillation.
	alpha := 0.3
	if pc.emaPressure == 0 {
		pc.emaPressure = pressure
	} else {
		pc.emaPressure = alpha*pressure + (1-alpha)*pc.emaPressure
	}

	// Progress guard: boost if we risk missing the minimum instance target.
	requiredRate := 0.0
	if pc.minInstances > 0 && elapsed > 0 && elapsed < pc.duration {
		remaining := pc.duration - elapsed
		shortfall := int64(pc.minInstances) - accepted
		if shortfall > 0 {
			requiredRate = float64(shortfall) / remaining.Seconds()
		}
	}

	pc.mu.Lock()
	defer pc.mu.Unlock()

	pf := pc.paceFactor
	if requiredRate > 0 && currentAttemptRate < requiredRate*1.1 {
		pf = pc.cfg.MaxPace
	} else if pc.emaPressure > pc.cfg.HighPressureThreshold {
		pf *= 0.9
	} else if pc.emaPressure < pc.cfg.LowPressureThreshold {
		pf *= 1.1
	}

	pf = math.Max(pc.cfg.MinPace, math.Min(pc.dynamicMax, pf))
	pc.paceFactor = pf
	return pf, pc.emaPressure, nil
}
