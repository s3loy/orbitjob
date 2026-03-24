package job

const (
	DefaultTenantID = "default"
	DefaultTimezone = "UTC"

	TriggerTypeCron   = "cron"
	TriggerTypeManual = "manual"

	ConcurrencyAllow   = "allow"
	ConcurrencyForbid  = "forbid"
	ConcurrencyReplace = "replace"

	MisfireSkip    = "skip"
	MisfireFireNow = "fire_now"
	MisfireCatchUp = "catch_up"
)

func isOneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}
