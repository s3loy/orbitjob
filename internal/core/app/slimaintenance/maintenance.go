// Package slimaintenance retires SLI replay details after their durable inputs
// and queryable missing evidence no longer need them.
package slimaintenance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	DefaultBatchSize     = 128
	DefaultTenantTimeout = 10 * time.Second
)

type Cursor struct {
	PlanRunID  int64
	EventRunID int64
	EventSLIID int64
}

type Counts struct {
	ScannedPlans        int
	ScannedEvents       int
	RemovedPlans        int
	RemovedEvents       int
	RemovedLegacyEvents int
	Retained            map[string]int
}

func (c *Counts) Add(other Counts) {
	c.ScannedPlans += other.ScannedPlans
	c.ScannedEvents += other.ScannedEvents
	c.RemovedPlans += other.RemovedPlans
	c.RemovedEvents += other.RemovedEvents
	c.RemovedLegacyEvents += other.RemovedLegacyEvents
	if c.Retained == nil {
		c.Retained = make(map[string]int)
	}
	for reason, count := range other.Retained {
		c.Retained[reason] += count
	}
}

type Page struct {
	Next   Cursor
	Counts Counts
}

type Pruner interface {
	PruneRetiredEventsPage(context.Context, string, time.Time, Cursor, int) (Page, error)
}

var scanned = promauto.NewCounterVec(prometheus.CounterOpts{Name: "orbitjob_sli_maintenance_scanned_total", Help: "SLI replay records inspected by bounded maintenance pages."}, []string{"kind"})
var removed = promauto.NewCounterVec(prometheus.CounterOpts{Name: "orbitjob_sli_maintenance_removed_total", Help: "Committed SLI replay detail removals."}, []string{"kind"})
var retained = promauto.NewCounterVec(prometheus.CounterOpts{Name: "orbitjob_sli_maintenance_retained_total", Help: "SLI replay records retained by safety predicate."}, []string{"reason"})
var failures = promauto.NewCounter(prometheus.CounterOpts{Name: "orbitjob_sli_maintenance_failures_total", Help: "Failed tenant maintenance pages, including partial progress."})
var duration = promauto.NewHistogram(prometheus.HistogramOpts{Name: "orbitjob_sli_maintenance_tenant_seconds", Help: "Wall time of one bounded tenant maintenance page."})

// Maintainer rotates past protected or failing records. Its scope includes
// suspended tenants and inactive sources supplied by the operator; it does not
// discover tenants outside that configured scope.
type Maintainer struct {
	Pruner        Pruner
	Tenants       []string
	BatchSize     int
	TenantTimeout time.Duration
	Clock         func() time.Time
	cursors       map[string]Cursor
}

func (m *Maintainer) Sweep(ctx context.Context) (Counts, error) {
	var total Counts
	if m.Pruner == nil {
		return total, fmt.Errorf("SLI maintenance pruner is required")
	}
	batch := m.BatchSize
	if batch < 1 || batch > DefaultBatchSize {
		batch = DefaultBatchSize
	}
	budget := m.TenantTimeout
	if budget <= 0 {
		budget = DefaultTenantTimeout
	}
	now := time.Now
	if m.Clock != nil {
		now = m.Clock
	}
	if m.cursors == nil {
		m.cursors = make(map[string]Cursor)
	}
	var failuresErr error
	for _, tenant := range m.Tenants {
		if err := ctx.Err(); err != nil {
			return total, errors.Join(failuresErr, err)
		}
		started := time.Now()
		tenantCtx, cancel := context.WithTimeout(ctx, budget)
		page, err := m.Pruner.PruneRetiredEventsPage(tenantCtx, tenant, now().UTC(), m.cursors[tenant], batch)
		if err == nil {
			err = tenantCtx.Err()
		}
		cancel()
		duration.Observe(time.Since(started).Seconds())
		m.cursors[tenant] = page.Next
		total.Add(page.Counts)
		scanned.WithLabelValues("plan").Add(float64(page.Counts.ScannedPlans))
		scanned.WithLabelValues("event").Add(float64(page.Counts.ScannedEvents))
		removed.WithLabelValues("plan").Add(float64(page.Counts.RemovedPlans))
		removed.WithLabelValues("event").Add(float64(page.Counts.RemovedEvents))
		for reason, count := range page.Counts.Retained {
			retained.WithLabelValues(reason).Add(float64(count))
		}
		if err != nil {
			failures.Inc()
			failuresErr = errors.Join(failuresErr, fmt.Errorf("SLI maintenance tenant %s: %w", tenant, err))
		}
	}
	return total, failuresErr
}
