package check

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"orbitjob/internal/domain/validation"
)

// boundsBaseInput is a fully valid create input; fault cases below override a
// single field so the asserted error can be attributed to that field.
func boundsBaseInput() CreateInput {
	cron := "*/5 * * * *"
	return CreateInput{
		Name:         "check",
		TenantID:     "00000000000000000000000001",
		CheckType:    CheckTypeHTTPHealth,
		CheckConfig:  map[string]any{"url": "http://api.local/health"},
		ScheduleType: ScheduleTypeCron,
		CronExpr:     &cron,
	}
}

// TestNormalizeCreateFieldFaults covers one invalid field at a time. Every
// rejection must be a validation.Error naming the offending field, because the
// API layer maps that field into the error body returned to callers.
func TestNormalizeCreateFieldFaults(t *testing.T) {
	tooLongName := strings.Repeat("n", 129)
	longDesc := strings.Repeat("d", 513)
	tooShortTenant := strings.Repeat("t", 25)
	tooLongTenant := strings.Repeat("t", 27)
	veryLongTenant := strings.Repeat("t", 65)
	longTimezone := strings.Repeat("z", 65)
	longCron := strings.Repeat("a", 129)
	badCron := "61 * * * *" // minute 61 is outside 0-59
	shortCron := "* * * *"  // standard parser needs five fields

	tests := []struct {
		name      string
		override  func(in *CreateInput)
		wantField string
	}{
		{"empty name", func(in *CreateInput) { in.Name = "" }, "name"},
		{"blank name", func(in *CreateInput) { in.Name = "   " }, "name"},
		{"name above max length", func(in *CreateInput) { in.Name = tooLongName }, "name"},
		{"description above max length", func(in *CreateInput) {
			d := longDesc
			in.Description = &d
		}, "description"},
		{"empty tenant id", func(in *CreateInput) { in.TenantID = "" }, "tenant_id"},
		{"blank tenant id", func(in *CreateInput) { in.TenantID = "   " }, "tenant_id"},
		{"tenant id below required length", func(in *CreateInput) { in.TenantID = tooShortTenant }, "tenant_id"},
		{"tenant id above required length", func(in *CreateInput) { in.TenantID = tooLongTenant }, "tenant_id"},
		{"tenant id far above required length", func(in *CreateInput) { in.TenantID = veryLongTenant }, "tenant_id"},
		{"missing probe url", func(in *CreateInput) { in.CheckConfig = nil }, "check_config.url"},
		{"empty probe url", func(in *CreateInput) { in.CheckConfig = map[string]any{"url": ""} }, "check_config.url"},
		{"non-string probe url", func(in *CreateInput) { in.CheckConfig = map[string]any{"url": 8080} }, "check_config.url"},
		{"relative probe url", func(in *CreateInput) { in.CheckConfig = map[string]any{"url": "api.local/health"} }, "check_config.url"},
		{"non-http probe scheme", func(in *CreateInput) { in.CheckConfig = map[string]any{"url": "ftp://api.local/health"} }, "check_config.url"},
		{"non-string probe method", func(in *CreateInput) {
			in.CheckConfig = map[string]any{"url": "http://api.local/health", "method": 42}
		}, "check_config.method"},
		{"unsafe probe method", func(in *CreateInput) {
			in.CheckConfig = map[string]any{"url": "http://api.local/health", "method": "DELETE"}
		}, "check_config.method"},
		{"string expected status", func(in *CreateInput) {
			in.CheckConfig = map[string]any{"url": "http://api.local/health", "expected_status": "200 OK"}
		}, "check_config.expected_status"},
		{"expected status below range", func(in *CreateInput) {
			in.CheckConfig = map[string]any{"url": "http://api.local/health", "expected_status": 99}
		}, "check_config.expected_status"},
		{"expected status above range", func(in *CreateInput) {
			in.CheckConfig = map[string]any{"url": "http://api.local/health", "expected_status": 600}
		}, "check_config.expected_status"},
		{"fractional expected status", func(in *CreateInput) {
			in.CheckConfig = map[string]any{"url": "http://api.local/health", "expected_status": 200.5}
		}, "check_config.expected_status"},
		{"unknown timezone", func(in *CreateInput) { in.Timezone = "Mars/Olympus" }, "timezone"},
		{"timezone above max length", func(in *CreateInput) { in.Timezone = longTimezone }, "timezone"},
		{"empty check type", func(in *CreateInput) { in.CheckType = "" }, "check_type"},
		{"unknown check type", func(in *CreateInput) { in.CheckType = "tcp_ping" }, "check_type"},
		{"negative timeout", func(in *CreateInput) { in.TimeoutSec = -1 }, "timeout_sec"},
		{"negative retry limit", func(in *CreateInput) { in.RetryLimit = -1 }, "retry_limit"},
		{"negative priority", func(in *CreateInput) { in.Priority = -1 }, "priority"},
		{"cron schedule without expression", func(in *CreateInput) { in.CronExpr = nil }, "cron_expr"},
		{"empty cron expression", func(in *CreateInput) {
			e := ""
			in.CronExpr = &e
		}, "cron_expr"},
		{"blank cron expression", func(in *CreateInput) {
			e := "   "
			in.CronExpr = &e
		}, "cron_expr"},
		{"out of range cron field", func(in *CreateInput) {
			e := badCron
			in.CronExpr = &e
		}, "cron_expr"},
		{"cron expression with too few fields", func(in *CreateInput) {
			e := shortCron
			in.CronExpr = &e
		}, "cron_expr"},
		{"cron expression above max length", func(in *CreateInput) {
			e := longCron
			in.CronExpr = &e
		}, "cron_expr"},
		{"interval schedule without interval", func(in *CreateInput) {
			in.ScheduleType = ScheduleTypeInterval
			in.CronExpr = nil
			in.IntervalSec = nil
		}, "interval_sec"},
		{"interval below one second", func(in *CreateInput) {
			in.ScheduleType = ScheduleTypeInterval
			in.CronExpr = nil
			zero := 0
			in.IntervalSec = &zero
		}, "interval_sec"},
		{"negative interval", func(in *CreateInput) {
			in.ScheduleType = ScheduleTypeInterval
			in.CronExpr = nil
			neg := -5
			in.IntervalSec = &neg
		}, "interval_sec"},
		{"interval just below the minimum cadence", func(in *CreateInput) {
			in.ScheduleType = ScheduleTypeInterval
			in.CronExpr = nil
			below := MinimumIntervalSec - 1
			in.IntervalSec = &below
		}, "interval_sec"},
		// Assertion-rule faults name the offending rule by index. These cases
		// used to pin the bug where the index verb was never substituted and
		// every rule fault published a literal %d; they now pin the fixed
		// behavior the API error body needs.
		{"empty assertion metric", func(in *CreateInput) {
			in.AssertionRules = []AssertionRule{{Metric: "", Operator: ">", Severity: "warning"}}
		}, "assertion_rules[0].metric"},
		{"blank assertion metric", func(in *CreateInput) {
			in.AssertionRules = []AssertionRule{{Metric: "   ", Operator: ">", Severity: "warning"}}
		}, "assertion_rules[0].metric"},
		{"assertion metric above max length", func(in *CreateInput) {
			in.AssertionRules = []AssertionRule{{Metric: strings.Repeat("m", 65), Operator: ">", Severity: "warning"}}
		}, "assertion_rules[0].metric"},
		{"unknown assertion operator", func(in *CreateInput) {
			in.AssertionRules = []AssertionRule{{Metric: "status_code", Operator: "=>", Severity: "warning"}}
		}, "assertion_rules[0].operator"},
		{"empty assertion operator", func(in *CreateInput) {
			in.AssertionRules = []AssertionRule{{Metric: "status_code", Operator: "", Severity: "warning"}}
		}, "assertion_rules[0].operator"},
		{"unknown assertion severity", func(in *CreateInput) {
			in.AssertionRules = []AssertionRule{{Metric: "status_code", Operator: ">", Severity: "info"}}
		}, "assertion_rules[0].severity"},
		{"assertion severity is case sensitive", func(in *CreateInput) {
			in.AssertionRules = []AssertionRule{{Metric: "status_code", Operator: ">", Severity: "WARNING"}}
		}, "assertion_rules[0].severity"},
		{"fault in second rule carries its index", func(in *CreateInput) {
			in.AssertionRules = []AssertionRule{
				{Metric: "status_code", Operator: "==", Severity: "warning"},
				{Metric: "latency", Operator: ">", Severity: "fatal"},
			}
		}, "assertion_rules[1].severity"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := boundsBaseInput()
			tt.override(&in)
			_, err := NormalizeCreate(time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC), in)
			if err == nil {
				t.Fatal("expected error")
			}
			assertValidationField(t, err, tt.wantField)
		})
	}
}

// TestNormalizeCreateNormalizesValues locks what normalization does to legal
// input: trims, defaults, and the empty-collection guarantees the store relies
// on when it marshals JSONB columns.
func TestNormalizeCreateNormalizesValues(t *testing.T) {
	tests := []struct {
		name     string
		override func(in *CreateInput)
		verify   func(t *testing.T, spec CreateSpec)
	}{
		{"name is trimmed", func(in *CreateInput) { in.Name = "  api-health  " },
			func(t *testing.T, spec CreateSpec) { assertEquals(t, "name", spec.Name, "api-health") }},
		{"name at max length passes", func(in *CreateInput) { in.Name = strings.Repeat("n", 128) },
			func(t *testing.T, spec CreateSpec) {
				if len(spec.Name) != 128 {
					t.Fatalf("name length = %d, want 128", len(spec.Name))
				}
			}},
		{"description is trimmed", func(in *CreateInput) {
			d := "  probe  "
			in.Description = &d
		}, func(t *testing.T, spec CreateSpec) {
			if spec.Description == nil || *spec.Description != "probe" {
				t.Fatalf("description = %v, want 'probe'", spec.Description)
			}
		}},
		{"blank description becomes nil", func(in *CreateInput) {
			d := "   "
			in.Description = &d
		}, func(t *testing.T, spec CreateSpec) {
			if spec.Description != nil {
				t.Fatalf("description = %q, want nil", *spec.Description)
			}
		}},
		{"description at max length passes", func(in *CreateInput) {
			d := strings.Repeat("d", 512)
			in.Description = &d
		}, func(t *testing.T, spec CreateSpec) {
			if spec.Description == nil || len(*spec.Description) != 512 {
				t.Fatalf("description length = %v, want 512", spec.Description)
			}
		}},
		{"tenant id is trimmed", func(in *CreateInput) { in.TenantID = "  00000000000000000000000001  " },
			func(t *testing.T, spec CreateSpec) {
				assertEquals(t, "tenant_id", spec.TenantID, "00000000000000000000000001")
			}},
		{"tenant id at required length passes", func(in *CreateInput) {
			in.TenantID = strings.Repeat("t", TenantIDLength)
		}, func(t *testing.T, spec CreateSpec) {
			if len(spec.TenantID) != TenantIDLength {
				t.Fatalf("tenant id length = %d, want %d", len(spec.TenantID), TenantIDLength)
			}
		}},
		{"resource group id is trimmed", func(in *CreateInput) { in.ResourceGroupID = "  rg-1  " },
			func(t *testing.T, spec CreateSpec) {
				assertEquals(t, "resource_group_id", spec.ResourceGroupID, "rg-1")
			}},
		{"empty timezone falls back to UTC", func(in *CreateInput) { in.Timezone = "" },
			func(t *testing.T, spec CreateSpec) { assertEquals(t, "timezone", spec.Timezone, DefaultTimezone) }},
		{"explicit timezone is kept", func(in *CreateInput) { in.Timezone = "Asia/Shanghai" },
			func(t *testing.T, spec CreateSpec) { assertEquals(t, "timezone", spec.Timezone, "Asia/Shanghai") }},
		{"empty schedule type falls back to cron", func(in *CreateInput) { in.ScheduleType = "" },
			func(t *testing.T, spec CreateSpec) {
				assertEquals(t, "schedule_type", spec.ScheduleType, ScheduleTypeCron)
			}},
		{"nil labels become empty map", func(in *CreateInput) { in.Labels = nil },
			func(t *testing.T, spec CreateSpec) {
				if spec.Labels == nil || len(spec.Labels) != 0 {
					t.Fatalf("labels = %v, want empty non-nil map", spec.Labels)
				}
			}},
		{"nil assertion rules become empty slice", func(in *CreateInput) { in.AssertionRules = nil },
			func(t *testing.T, spec CreateSpec) {
				if spec.AssertionRules == nil || len(spec.AssertionRules) != 0 {
					t.Fatalf("assertion_rules = %v, want empty non-nil slice", spec.AssertionRules)
				}
			}},
		{"rule operator and severity are trimmed", func(in *CreateInput) {
			in.AssertionRules = []AssertionRule{{Metric: "status_code", Operator: " >= ", Severity: " critical "}}
		}, func(t *testing.T, spec CreateSpec) {
			rule := spec.AssertionRules[0]
			if rule.Operator != ">=" || rule.Severity != "critical" {
				t.Fatalf("rule = %+v, want trimmed operator and severity", rule)
			}
		}},
		{"rule metric is trimmed", func(in *CreateInput) {
			in.AssertionRules = []AssertionRule{{Metric: "  status_code  ", Operator: ">", Severity: "warning"}}
		}, func(t *testing.T, spec CreateSpec) {
			assertEquals(t, "metric", spec.AssertionRules[0].Metric, "status_code")
		}},
		{"threshold passes through unvalidated including negatives", func(in *CreateInput) {
			in.AssertionRules = []AssertionRule{{Metric: "delta", Operator: "<", Threshold: -3.5, Severity: "warning"}}
		}, func(t *testing.T, spec CreateSpec) {
			if spec.AssertionRules[0].Threshold != -3.5 {
				t.Fatalf("threshold = %v, want -3.5", spec.AssertionRules[0].Threshold)
			}
		}},
		{"zero timeout falls back to default", func(in *CreateInput) { in.TimeoutSec = 0 },
			func(t *testing.T, spec CreateSpec) {
				if spec.TimeoutSec != DefaultTimeoutSec {
					t.Fatalf("timeout = %d, want default %d", spec.TimeoutSec, DefaultTimeoutSec)
				}
			}},
		{"explicit timeout is kept", func(in *CreateInput) { in.TimeoutSec = 120 },
			func(t *testing.T, spec CreateSpec) { assertEquals(t, "timeout", itoa(spec.TimeoutSec), "120") }},
		{"zero retry limit falls back to default", func(in *CreateInput) { in.RetryLimit = 0 },
			func(t *testing.T, spec CreateSpec) {
				if spec.RetryLimit != DefaultRetryLimit {
					t.Fatalf("retry limit = %d, want default %d", spec.RetryLimit, DefaultRetryLimit)
				}
			}},
		{"explicit retry limit is kept", func(in *CreateInput) { in.RetryLimit = 7 },
			func(t *testing.T, spec CreateSpec) { assertEquals(t, "retry limit", itoa(spec.RetryLimit), "7") }},
		{"zero priority falls back to default", func(in *CreateInput) { in.Priority = 0 },
			func(t *testing.T, spec CreateSpec) {
				if spec.Priority != DefaultPriority {
					t.Fatalf("priority = %d, want default %d", spec.Priority, DefaultPriority)
				}
			}},
		{"explicit priority is kept", func(in *CreateInput) { in.Priority = 9 },
			func(t *testing.T, spec CreateSpec) { assertEquals(t, "priority", itoa(spec.Priority), "9") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := boundsBaseInput()
			tt.override(&in)
			spec, err := NormalizeCreate(time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC), in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.verify(t, spec)
		})
	}
}

// Every comparison operator the evaluator understands must normalize cleanly;
// dropping one would silently reject checks the evaluator could score.
func TestNormalizeCreateAcceptsEveryOperator(t *testing.T) {
	for _, op := range []string{"<", "<=", "==", "!=", ">=", ">"} {
		t.Run(op, func(t *testing.T) {
			in := boundsBaseInput()
			in.AssertionRules = []AssertionRule{{Metric: "status_code", Operator: op, Threshold: 1, Severity: "warning"}}
			if _, err := NormalizeCreate(time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC), in); err != nil {
				t.Fatalf("operator %q rejected: %v", op, err)
			}
		})
	}
}

// TestNormalizeCreateScheduleMath pins the computed NextRunAt: cron must
// resolve strictly after now in the check's timezone and be stored in UTC,
// interval must be now plus exactly N seconds. The scheduler orders work off
// this column, so a wrong value skips or doubles runs.
func TestNormalizeCreateScheduleMath(t *testing.T) {
	now := time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)

	t.Run("cron next run is exact", func(t *testing.T) {
		expr := "30 9 * * *"
		spec, err := NormalizeCreate(now, boundsBaseWithCron(expr))
		if err != nil {
			t.Fatal(err)
		}
		want := time.Date(2026, 1, 4, 9, 30, 0, 0, time.UTC)
		if spec.NextRunAt == nil || !spec.NextRunAt.Equal(want) {
			t.Fatalf("next run = %v, want %v", spec.NextRunAt, want)
		}
	})

	t.Run("cron next run is strictly after a boundary now", func(t *testing.T) {
		// now lands exactly on a */5 slot: the next run must still be the
		// following slot, never now itself.
		spec, err := NormalizeCreate(now, boundsBaseWithCron("*/5 * * * *"))
		if err != nil {
			t.Fatal(err)
		}
		want := now.Add(5 * time.Minute)
		if spec.NextRunAt == nil || !spec.NextRunAt.Equal(want) || !spec.NextRunAt.After(now) {
			t.Fatalf("next run = %v, want %v (strictly after now)", spec.NextRunAt, want)
		}
	})

	t.Run("cron resolves in the check timezone and stores UTC", func(t *testing.T) {
		// 09:00 in Asia/Shanghai (UTC+8) is 01:00 UTC on the same day.
		in := boundsBaseWithCron("0 9 * * *")
		in.Timezone = "Asia/Shanghai"
		spec, err := NormalizeCreate(now, in)
		if err != nil {
			t.Fatal(err)
		}
		want := time.Date(2026, 1, 4, 1, 0, 0, 0, time.UTC)
		if spec.NextRunAt == nil || !spec.NextRunAt.Equal(want) {
			t.Fatalf("next run = %v, want %v", spec.NextRunAt, want)
		}
		if spec.NextRunAt.Location() != time.UTC {
			t.Fatalf("next run location = %v, want UTC", spec.NextRunAt.Location())
		}
	})

	t.Run("stored cron expression is trimmed", func(t *testing.T) {
		spec, err := NormalizeCreate(now, boundsBaseWithCron("  */10 * * * *  "))
		if err != nil {
			t.Fatal(err)
		}
		if spec.CronExpr == nil || *spec.CronExpr != "*/10 * * * *" {
			t.Fatalf("cron expr = %v, want trimmed", spec.CronExpr)
		}
		if spec.IntervalSec != nil {
			t.Fatalf("interval = %v, want nil on a cron check", spec.IntervalSec)
		}
	})

	t.Run("interval next run is now plus N seconds in UTC", func(t *testing.T) {
		in := boundsBaseInput()
		in.ScheduleType = ScheduleTypeInterval
		in.CronExpr = nil
		sec := 90
		in.IntervalSec = &sec
		spec, err := NormalizeCreate(now, in)
		if err != nil {
			t.Fatal(err)
		}
		want := now.Add(90 * time.Second)
		if spec.NextRunAt == nil || !spec.NextRunAt.Equal(want) {
			t.Fatalf("next run = %v, want %v", spec.NextRunAt, want)
		}
		if spec.CronExpr != nil {
			t.Fatalf("cron expr = %q, want nil on an interval check", *spec.CronExpr)
		}
		if spec.IntervalSec == nil || *spec.IntervalSec != 90 {
			t.Fatalf("interval = %v, want 90", spec.IntervalSec)
		}
	})
}

// Structural failures (bad timezone, bad cron) carry the underlying parser
// error so the API can explain why the value was rejected instead of only
// which field failed.
func TestNormalizeCreateCausePreserved(t *testing.T) {
	now := time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)

	t.Run("invalid timezone keeps the load error", func(t *testing.T) {
		in := boundsBaseInput()
		in.Timezone = "Mars/Olympus"
		_, err := NormalizeCreate(now, in)
		if err == nil {
			t.Fatal("expected error")
		}
		var vErr *validation.Error
		if !validation.As(err, &vErr) {
			t.Fatalf("expected validation.Error, got %T", err)
		}
		assertEquals(t, "message", vErr.Message, "invalid timezone")
		if vErr.Cause == nil {
			t.Fatal("expected the location load error to be preserved as Cause")
		}
	})

	t.Run("invalid cron keeps the parse error", func(t *testing.T) {
		in := boundsBaseWithCron("61 * * * *")
		_, err := NormalizeCreate(now, in)
		if err == nil {
			t.Fatal("expected error")
		}
		var vErr *validation.Error
		if !validation.As(err, &vErr) {
			t.Fatalf("expected validation.Error, got %T", err)
		}
		assertEquals(t, "message", vErr.Message, "invalid cron expression")
		if vErr.Cause == nil {
			t.Fatal("expected the cron parse error to be preserved as Cause")
		}
	})
}

// Check and label configs are stored as JSONB: a value JSON cannot represent
// must be rejected here, before the store marshals it mid-transaction.
func TestNormalizeCreateRejectsUnserializableJSONB(t *testing.T) {
	now := time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)

	t.Run("check_config", func(t *testing.T) {
		in := boundsBaseInput()
		in.CheckConfig = map[string]any{"ch": make(chan int)}
		_, err := NormalizeCreate(now, in)
		assertValidationField(t, err, "check_config")
	})
	t.Run("labels", func(t *testing.T) {
		in := boundsBaseInput()
		in.Labels = map[string]any{"fn": func() {}}
		_, err := NormalizeCreate(now, in)
		assertValidationField(t, err, "labels")
	})
}

func boundsBaseWithCron(expr string) CreateInput {
	in := boundsBaseInput()
	in.CronExpr = &expr
	return in
}

func assertEquals(t *testing.T, field, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %q, want %q", field, got, want)
	}
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
