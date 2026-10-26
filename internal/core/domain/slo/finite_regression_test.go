package slo

import (
	"math"
	"testing"
	"time"
)

func TestNormalizeRejectsNonfiniteBudgetInputs(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		base := CreateInput{Name: "availability", SLIID: 1, Target: 0.99, WindowDuration: time.Hour}
		target := base
		target.Target = value
		fast := base
		fast.AlertFastBurnRate = value
		slow := base
		slow.AlertSlowBurnRate = value
		for _, in := range []CreateInput{target, fast, slow} {
			if _, err := NormalizeCreate(in); err == nil {
				t.Fatal("accepted a non-serializable budget input")
			}
		}
	}
	if got := BudgetStatus(0, 0); got != BudgetHealthy {
		t.Fatalf("100%% target without failures must remain healthy: %s", got)
	}
}
