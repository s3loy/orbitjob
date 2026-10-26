// Package workflowschedule owns the cron and timezone contract shared by
// Workflow admission, preview and automatic execution.
package workflowschedule

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // Scheduling remains independent of the container filesystem.

	"github.com/robfig/cron/v3"
)

const DefaultTimeZone = "UTC"
const MaximumPreviewCount = 20
const DefaultPreviewCount = 5
const MaximumExpressionLength = 256
const MaximumTimeZoneLength = 128

func Zone(zone string) string {
	if zone == "" {
		return DefaultTimeZone
	}
	return zone
}

// Parse accepts exactly five cron fields and an explicit IANA zone. Embedded
// timezone directives, descriptors and host-local time are not part of the API.
func Parse(expression, zone string) (cron.Schedule, error) {
	fields := strings.Fields(expression)
	if len(expression) > MaximumExpressionLength || len(fields) != 5 || strings.HasPrefix(expression, "@") {
		return nil, fmt.Errorf("schedule must contain five cron fields")
	}
	zone = Zone(zone)
	if len(zone) > MaximumTimeZoneLength || zone == "Local" || strings.TrimSpace(zone) != zone {
		return nil, fmt.Errorf("time_zone must be an IANA timezone")
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return nil, fmt.Errorf("time_zone must be an IANA timezone: %w", err)
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	parsed, err := parser.Parse("CRON_TZ=" + zone + " " + expression)
	if err != nil {
		return nil, fmt.Errorf("invalid schedule: %w", err)
	}
	return parsed, nil
}

func Validate(expression, zone string) error {
	if expression == "" {
		_, err := Parse("0 0 * * *", zone)
		return err
	}
	_, err := Parse(expression, zone)
	return err
}

func Preview(expression, zone string, after time.Time, count int) ([]time.Time, error) {
	if count < 1 || count > MaximumPreviewCount {
		return nil, fmt.Errorf("count must be between 1 and %d", MaximumPreviewCount)
	}
	parsed, err := Parse(expression, zone)
	if err != nil {
		return nil, err
	}
	out := make([]time.Time, 0, count)
	for range count {
		after = parsed.Next(after)
		if after.IsZero() {
			return nil, fmt.Errorf("schedule has no upcoming occurrence")
		}
		out = append(out, after.UTC())
	}
	return out, nil
}
