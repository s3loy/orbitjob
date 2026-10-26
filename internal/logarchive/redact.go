package logarchive

import "regexp"

// Platform-generated diagnostic events must not disclose credentials from
// argv, image URLs or Kubernetes messages. Workload stdout/stderr stays raw
// and is protected by its independent sensitive-log permission.
var platformSecrets = []*regexp.Regexp{
	regexp.MustCompile(`otj_[A-Za-z0-9_-]+`),
	regexp.MustCompile(`(?i)(?:postgres(?:ql)?|https?)://[^\s/@"]+:[^\s/@"]+@`),
	regexp.MustCompile(`(?i)bearer[ \t]+[A-Za-z0-9._~+/=-]+`),
}

func redactEvent(text string) string {
	for _, pattern := range platformSecrets {
		text = pattern.ReplaceAllString(text, "[REDACTED]")
	}
	return text
}
