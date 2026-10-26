package workflowschedule

import (
	"testing"
	"time"
)

func TestPreviewUsesZoneAndRejectsUnsupportedSyntax(t *testing.T) {
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	got, err := Preview("0 9 * * *", "Asia/Shanghai", now, 2)
	if err != nil || len(got) != 2 || !got[0].Equal(now.Add(time.Hour)) {
		t.Fatalf("preview=%v err=%v", got, err)
	}
	for _, expression := range []string{"@daily", "0 0 9 * * *", "CRON_TZ=UTC 0 9 * * *", "61 * * * *"} {
		if _, err := Parse(expression, "UTC"); err == nil {
			t.Errorf("accepted %q", expression)
		}
	}
	if _, err := Parse("0 9 * * *", "Local"); err == nil {
		t.Error("accepted host-local timezone")
	}
	if _, err := Parse("0 9 * * *", "Missing/Zone"); err == nil {
		t.Error("accepted invalid timezone")
	}
}

func TestDSTUsesUTCInstants(t *testing.T) {
	start := time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC)
	got, err := Preview("30 1 * * *", "America/New_York", start, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].Equal(start.Add(90*time.Minute)) || !got[1].Equal(start.Add(150*time.Minute)) {
		t.Fatalf("fall-back instants=%v", got)
	}
	spring, err := Preview("30 2 * * *", "America/New_York", time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC), 1)
	if err != nil || spring[0].Day() != 9 {
		t.Fatalf("nonexistent local time was not skipped: %v %v", spring, err)
	}
}
