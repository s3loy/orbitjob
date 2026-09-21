package execution

import "testing"

// benchJob keeps benchmark results alive so the compiler cannot drop the calls.
var benchJob interface{}

// BuildJob renders one Kubernetes Job per attempt, on the operator's ensure
// path: every reconcile of a run that owes an attempt re-renders the desired
// object to compare against what the cluster holds. The render is cheap —
// struct literals plus two defensive slice copies — and the benchmark exists
// to keep it that way as the pod spec grows (volumes, affinity, node
// selectors all land here eventually). The two name cases matter for
// different reasons: ordinary run names stay inside the DNS-1123 limit and
// cost nothing, while long ones take the JobName hashing branch, which is
// the only nontrivial work in the render.
func BenchmarkBuildJob(b *testing.B) {
	template := Template{
		Image:                 "ghcr.io/s3loy/orbitjob-task:v0.9.0",
		Command:               []string{"/bin/sh", "-ec"},
		Args:                  []string{"curl", "-fsS", "--max-time", "25", "https://api.example.com/v1/health", "|", "tee", "/dev/null"},
		BackoffLimit:          2,
		ActiveDeadlineSeconds: 1800,
	}

	b.Run("name-within-dns-limit", func(b *testing.B) {
		identity := Identity{
			RunName:    "run-0198f6a27c3d4b2a9f1e5d6c8b7a3e21",
			RunUID:     "f47ac10b-58cc-4372-a567-0e02b2c3d479",
			RevisionID: "4242",
			Attempt:    1,
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchJob = BuildJob(identity, "orbitjob-tenant-1", template)
		}
	})

	b.Run("long-name-hashed", func(b *testing.B) {
		identity := Identity{
			RunName:    "run-0198f6a27c3d4b2a9f1e5d6c8b7a3e21-tenant-ingest-scheduled-job-occurrence-20260918",
			RunUID:     "f47ac10b-58cc-4372-a567-0e02b2c3d479",
			RevisionID: "4242",
			Attempt:    3,
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			benchJob = BuildJob(identity, "orbitjob-tenant-1", template)
		}
	})
}
