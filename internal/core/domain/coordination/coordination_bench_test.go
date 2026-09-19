package coordination

import (
	"testing"
	"time"
)

// benchSink keeps the benchmark result alive so the compiler cannot drop the call.
var benchSink string

// OccurrenceKey is the ledger's dedup cornerstone: every schedule fire, manual
// trigger, and check occurrence derives its occurrence key here before the
// database unique index sees it. The function is cheap (a Sprintf plus one
// sha256), and that is the point of the benchmark: it pins the cost of a path
// taken once per occurrence across every tenant, so a future change that makes
// derivation allocate heavily (buffer pools, a different hash, per-call
// timezone math) shows up here. The input dimension that varies in production
// is the source UID shape: check-sourced definitions use "check-<row id>",
// ScheduledJob revisions carry the Kubernetes CR UID (a 36-character UUID),
// and the suffix that feeds the hash is therefore not one fixed length.
func BenchmarkOccurrenceKey(b *testing.B) {
	scheduledAt := time.Date(2026, 9, 18, 9, 30, 0, 123456789, time.UTC)
	cases := []struct {
		name      string
		sourceUID string
	}{
		{"check-source-uid", "check-1234"},
		{"kubernetes-cr-uid", "f47ac10b-58cc-4372-a567-0e02b2c3d479"},
		{"long-namespaced-name", "orbitjob-prod/tenant-ingest-scheduled-job-0198f6a2"},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				benchSink = OccurrenceKey(tc.sourceUID, 42, scheduledAt)
			}
		})
	}
}
