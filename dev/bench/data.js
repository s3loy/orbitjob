window.BENCHMARK_DATA = {
  "lastUpdate": 1790000198482,
  "repoUrl": "https://github.com/s3loy/orbitjob",
  "entries": {
    "Benchmark": [
      {
        "commit": {
          "author": {
            "email": "1915276056@qq.com",
            "name": "s3loy",
            "username": "s3loy"
          },
          "committer": {
            "email": "1915276056@qq.com",
            "name": "s3loy",
            "username": "s3loy"
          },
          "distinct": true,
          "id": "4a11d9112640b0712f4b2fddd837fda78b83ba78",
          "message": "docs: install v0.2.1 in quick start",
          "timestamp": "2026-09-21T21:48:26+08:00",
          "tree_id": "a9922de4b6ba8e4710f864c885da0a39cd5e4c02",
          "url": "https://github.com/s3loy/orbitjob/commit/4a11d9112640b0712f4b2fddd837fda78b83ba78"
        },
        "date": 1789998776430,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkOccurrenceKey/check-source-uid (orbitjob/internal/core/domain/coordination)",
            "value": 1517,
            "unit": "ns/op\t     288 B/op\t       7 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/check-source-uid (orbitjob/internal/core/domain/coordination) - ns/op",
            "value": 1517,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/check-source-uid (orbitjob/internal/core/domain/coordination) - B/op",
            "value": 288,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/check-source-uid (orbitjob/internal/core/domain/coordination) - allocs/op",
            "value": 7,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/kubernetes-cr-uid (orbitjob/internal/core/domain/coordination)",
            "value": 1648,
            "unit": "ns/op\t     352 B/op\t       7 allocs/op",
            "extra": "903921 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/kubernetes-cr-uid (orbitjob/internal/core/domain/coordination) - ns/op",
            "value": 1648,
            "unit": "ns/op",
            "extra": "903921 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/kubernetes-cr-uid (orbitjob/internal/core/domain/coordination) - B/op",
            "value": 352,
            "unit": "B/op",
            "extra": "903921 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/kubernetes-cr-uid (orbitjob/internal/core/domain/coordination) - allocs/op",
            "value": 7,
            "unit": "allocs/op",
            "extra": "903921 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/long-namespaced-name (orbitjob/internal/core/domain/coordination)",
            "value": 1600,
            "unit": "ns/op\t     384 B/op\t       7 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/long-namespaced-name (orbitjob/internal/core/domain/coordination) - ns/op",
            "value": 1600,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/long-namespaced-name (orbitjob/internal/core/domain/coordination) - B/op",
            "value": 384,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/long-namespaced-name (orbitjob/internal/core/domain/coordination) - allocs/op",
            "value": 7,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/valid/single-edge (orbitjob/internal/core/domain/jobrun)",
            "value": 36.85,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "34388678 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/valid/single-edge (orbitjob/internal/core/domain/jobrun) - ns/op",
            "value": 36.85,
            "unit": "ns/op",
            "extra": "34388678 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/valid/single-edge (orbitjob/internal/core/domain/jobrun) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "34388678 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/valid/single-edge (orbitjob/internal/core/domain/jobrun) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "34388678 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/invalid/terminal-rewrite (orbitjob/internal/core/domain/jobrun)",
            "value": 33.32,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "37546508 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/invalid/terminal-rewrite (orbitjob/internal/core/domain/jobrun) - ns/op",
            "value": 33.32,
            "unit": "ns/op",
            "extra": "37546508 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/invalid/terminal-rewrite (orbitjob/internal/core/domain/jobrun) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "37546508 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/invalid/terminal-rewrite (orbitjob/internal/core/domain/jobrun) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "37546508 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/valid/full-lifecycle (orbitjob/internal/core/domain/jobrun)",
            "value": 242.4,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "4565331 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/valid/full-lifecycle (orbitjob/internal/core/domain/jobrun) - ns/op",
            "value": 242.4,
            "unit": "ns/op",
            "extra": "4565331 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/valid/full-lifecycle (orbitjob/internal/core/domain/jobrun) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "4565331 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/valid/full-lifecycle (orbitjob/internal/core/domain/jobrun) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "4565331 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/can-transition/full-matrix (orbitjob/internal/core/domain/jobrun)",
            "value": 203.8,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "5731410 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/can-transition/full-matrix (orbitjob/internal/core/domain/jobrun) - ns/op",
            "value": 203.8,
            "unit": "ns/op",
            "extra": "5731410 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/can-transition/full-matrix (orbitjob/internal/core/domain/jobrun) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "5731410 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/can-transition/full-matrix (orbitjob/internal/core/domain/jobrun) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "5731410 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/terminal/all-phases (orbitjob/internal/core/domain/jobrun)",
            "value": 14.28,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "83830675 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/terminal/all-phases (orbitjob/internal/core/domain/jobrun) - ns/op",
            "value": 14.28,
            "unit": "ns/op",
            "extra": "83830675 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/terminal/all-phases (orbitjob/internal/core/domain/jobrun) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "83830675 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/terminal/all-phases (orbitjob/internal/core/domain/jobrun) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "83830675 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/valid/cron-schedule (orbitjob/internal/core/domain/check)",
            "value": 14299,
            "unit": "ns/op\t    6466 B/op\t      63 allocs/op",
            "extra": "84534 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/valid/cron-schedule (orbitjob/internal/core/domain/check) - ns/op",
            "value": 14299,
            "unit": "ns/op",
            "extra": "84534 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/valid/cron-schedule (orbitjob/internal/core/domain/check) - B/op",
            "value": 6466,
            "unit": "B/op",
            "extra": "84534 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/valid/cron-schedule (orbitjob/internal/core/domain/check) - allocs/op",
            "value": 63,
            "unit": "allocs/op",
            "extra": "84534 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/valid/interval-schedule (orbitjob/internal/core/domain/check)",
            "value": 2450,
            "unit": "ns/op\t     472 B/op\t      17 allocs/op",
            "extra": "468744 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/valid/interval-schedule (orbitjob/internal/core/domain/check) - ns/op",
            "value": 2450,
            "unit": "ns/op",
            "extra": "468744 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/valid/interval-schedule (orbitjob/internal/core/domain/check) - B/op",
            "value": 472,
            "unit": "B/op",
            "extra": "468744 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/valid/interval-schedule (orbitjob/internal/core/domain/check) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "468744 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/invalid/malformed-cron (orbitjob/internal/core/domain/check)",
            "value": 2884,
            "unit": "ns/op\t    1080 B/op\t      36 allocs/op",
            "extra": "406017 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/invalid/malformed-cron (orbitjob/internal/core/domain/check) - ns/op",
            "value": 2884,
            "unit": "ns/op",
            "extra": "406017 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/invalid/malformed-cron (orbitjob/internal/core/domain/check) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "406017 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/invalid/malformed-cron (orbitjob/internal/core/domain/check) - allocs/op",
            "value": 36,
            "unit": "allocs/op",
            "extra": "406017 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/valid/full-config (orbitjob/internal/core/domain/check)",
            "value": 347.7,
            "unit": "ns/op\t     152 B/op\t       2 allocs/op",
            "extra": "3423298 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/valid/full-config (orbitjob/internal/core/domain/check) - ns/op",
            "value": 347.7,
            "unit": "ns/op",
            "extra": "3423298 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/valid/full-config (orbitjob/internal/core/domain/check) - B/op",
            "value": 152,
            "unit": "B/op",
            "extra": "3423298 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/valid/full-config (orbitjob/internal/core/domain/check) - allocs/op",
            "value": 2,
            "unit": "allocs/op",
            "extra": "3423298 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/valid/url-only-defaults (orbitjob/internal/core/domain/check)",
            "value": 257.8,
            "unit": "ns/op\t     144 B/op\t       1 allocs/op",
            "extra": "4626487 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/valid/url-only-defaults (orbitjob/internal/core/domain/check) - ns/op",
            "value": 257.8,
            "unit": "ns/op",
            "extra": "4626487 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/valid/url-only-defaults (orbitjob/internal/core/domain/check) - B/op",
            "value": 144,
            "unit": "B/op",
            "extra": "4626487 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/valid/url-only-defaults (orbitjob/internal/core/domain/check) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "4626487 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/invalid/bad-scheme (orbitjob/internal/core/domain/check)",
            "value": 251.1,
            "unit": "ns/op\t     192 B/op\t       2 allocs/op",
            "extra": "4980258 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/invalid/bad-scheme (orbitjob/internal/core/domain/check) - ns/op",
            "value": 251.1,
            "unit": "ns/op",
            "extra": "4980258 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/invalid/bad-scheme (orbitjob/internal/core/domain/check) - B/op",
            "value": 192,
            "unit": "B/op",
            "extra": "4980258 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/invalid/bad-scheme (orbitjob/internal/core/domain/check) - allocs/op",
            "value": 2,
            "unit": "allocs/op",
            "extra": "4980258 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/invalid/relative-url (orbitjob/internal/core/domain/check)",
            "value": 152.4,
            "unit": "ns/op\t     192 B/op\t       2 allocs/op",
            "extra": "7907388 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/invalid/relative-url (orbitjob/internal/core/domain/check) - ns/op",
            "value": 152.4,
            "unit": "ns/op",
            "extra": "7907388 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/invalid/relative-url (orbitjob/internal/core/domain/check) - B/op",
            "value": 192,
            "unit": "B/op",
            "extra": "7907388 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/invalid/relative-url (orbitjob/internal/core/domain/check) - allocs/op",
            "value": 2,
            "unit": "allocs/op",
            "extra": "7907388 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildJob/name-within-dns-limit (orbitjob/internal/core/app/execution)",
            "value": 1240,
            "unit": "ns/op\t    2384 B/op\t       9 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildJob/name-within-dns-limit (orbitjob/internal/core/app/execution) - ns/op",
            "value": 1240,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildJob/name-within-dns-limit (orbitjob/internal/core/app/execution) - B/op",
            "value": 2384,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildJob/name-within-dns-limit (orbitjob/internal/core/app/execution) - allocs/op",
            "value": 9,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildJob/long-name-hashed (orbitjob/internal/core/app/execution)",
            "value": 1794,
            "unit": "ns/op\t    2720 B/op\t      13 allocs/op",
            "extra": "625911 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildJob/long-name-hashed (orbitjob/internal/core/app/execution) - ns/op",
            "value": 1794,
            "unit": "ns/op",
            "extra": "625911 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildJob/long-name-hashed (orbitjob/internal/core/app/execution) - B/op",
            "value": 2720,
            "unit": "B/op",
            "extra": "625911 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildJob/long-name-hashed (orbitjob/internal/core/app/execution) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "625911 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/availability-default (orbitjob/internal/core/app/checkobserve)",
            "value": 5.927,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "202005184 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/availability-default (orbitjob/internal/core/app/checkobserve) - ns/op",
            "value": 5.927,
            "unit": "ns/op",
            "extra": "202005184 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/availability-default (orbitjob/internal/core/app/checkobserve) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "202005184 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/availability-default (orbitjob/internal/core/app/checkobserve) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "202005184 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/latency-threshold (orbitjob/internal/core/app/checkobserve)",
            "value": 75.69,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "16024808 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/latency-threshold (orbitjob/internal/core/app/checkobserve) - ns/op",
            "value": 75.69,
            "unit": "ns/op",
            "extra": "16024808 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/latency-threshold (orbitjob/internal/core/app/checkobserve) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "16024808 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/latency-threshold (orbitjob/internal/core/app/checkobserve) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "16024808 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/status-criterion (orbitjob/internal/core/app/checkobserve)",
            "value": 52.15,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "22949629 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/status-criterion (orbitjob/internal/core/app/checkobserve) - ns/op",
            "value": 52.15,
            "unit": "ns/op",
            "extra": "22949629 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/status-criterion (orbitjob/internal/core/app/checkobserve) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "22949629 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/status-criterion (orbitjob/internal/core/app/checkobserve) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "22949629 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/unknown-producer-key (orbitjob/internal/core/app/checkobserve)",
            "value": 268.9,
            "unit": "ns/op\t      96 B/op\t       3 allocs/op",
            "extra": "4591179 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/unknown-producer-key (orbitjob/internal/core/app/checkobserve) - ns/op",
            "value": 268.9,
            "unit": "ns/op",
            "extra": "4591179 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/unknown-producer-key (orbitjob/internal/core/app/checkobserve) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "4591179 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/unknown-producer-key (orbitjob/internal/core/app/checkobserve) - allocs/op",
            "value": 3,
            "unit": "allocs/op",
            "extra": "4591179 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/occurrence-run-id (orbitjob/internal/core/app/checkobserve)",
            "value": 70.75,
            "unit": "ns/op\t      48 B/op\t       1 allocs/op",
            "extra": "14715130 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/occurrence-run-id (orbitjob/internal/core/app/checkobserve) - ns/op",
            "value": 70.75,
            "unit": "ns/op",
            "extra": "14715130 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/occurrence-run-id (orbitjob/internal/core/app/checkobserve) - B/op",
            "value": 48,
            "unit": "B/op",
            "extra": "14715130 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/occurrence-run-id (orbitjob/internal/core/app/checkobserve) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "14715130 times\n4 procs"
          },
          {
            "name": "BenchmarkCampaign (orbitjob/internal/platform/election)",
            "value": 827.6,
            "unit": "ns/op\t     311 B/op\t       7 allocs/op",
            "extra": "1396039 times\n4 procs"
          },
          {
            "name": "BenchmarkCampaign (orbitjob/internal/platform/election) - ns/op",
            "value": 827.6,
            "unit": "ns/op",
            "extra": "1396039 times\n4 procs"
          },
          {
            "name": "BenchmarkCampaign (orbitjob/internal/platform/election) - B/op",
            "value": 311,
            "unit": "B/op",
            "extra": "1396039 times\n4 procs"
          },
          {
            "name": "BenchmarkCampaign (orbitjob/internal/platform/election) - allocs/op",
            "value": 7,
            "unit": "allocs/op",
            "extra": "1396039 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLock (orbitjob/internal/platform/election)",
            "value": 2955,
            "unit": "ns/op\t     255 B/op\t       7 allocs/op",
            "extra": "391504 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLock (orbitjob/internal/platform/election) - ns/op",
            "value": 2955,
            "unit": "ns/op",
            "extra": "391504 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLock (orbitjob/internal/platform/election) - B/op",
            "value": 255,
            "unit": "B/op",
            "extra": "391504 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLock (orbitjob/internal/platform/election) - allocs/op",
            "value": 7,
            "unit": "allocs/op",
            "extra": "391504 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=2 (orbitjob/internal/platform/election)",
            "value": 99.5,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "28728204 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=2 (orbitjob/internal/platform/election) - ns/op",
            "value": 99.5,
            "unit": "ns/op",
            "extra": "28728204 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=2 (orbitjob/internal/platform/election) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "28728204 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=2 (orbitjob/internal/platform/election) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "28728204 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=4 (orbitjob/internal/platform/election)",
            "value": 118.7,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "9930572 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=4 (orbitjob/internal/platform/election) - ns/op",
            "value": 118.7,
            "unit": "ns/op",
            "extra": "9930572 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=4 (orbitjob/internal/platform/election) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "9930572 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=4 (orbitjob/internal/platform/election) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "9930572 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=8 (orbitjob/internal/platform/election)",
            "value": 156.9,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "16351339 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=8 (orbitjob/internal/platform/election) - ns/op",
            "value": 156.9,
            "unit": "ns/op",
            "extra": "16351339 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=8 (orbitjob/internal/platform/election) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "16351339 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=8 (orbitjob/internal/platform/election) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "16351339 times\n4 procs"
          }
        ]
      },
      {
        "commit": {
          "author": {
            "email": "1915276056@qq.com",
            "name": "s3loy",
            "username": "s3loy"
          },
          "committer": {
            "email": "1915276056@qq.com",
            "name": "s3loy",
            "username": "s3loy"
          },
          "distinct": true,
          "id": "d12cca01a2e16eba808043dc98358f35ace4709e",
          "message": "fix(release): accept missing docker hub repositories",
          "timestamp": "2026-09-21T22:13:05+08:00",
          "tree_id": "912d1a52dccc09807f313590cb5f50439b5943ee",
          "url": "https://github.com/s3loy/orbitjob/commit/d12cca01a2e16eba808043dc98358f35ace4709e"
        },
        "date": 1790000196781,
        "tool": "go",
        "benches": [
          {
            "name": "BenchmarkOccurrenceKey/check-source-uid (orbitjob/internal/core/domain/coordination)",
            "value": 1456,
            "unit": "ns/op\t     288 B/op\t       7 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/check-source-uid (orbitjob/internal/core/domain/coordination) - ns/op",
            "value": 1456,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/check-source-uid (orbitjob/internal/core/domain/coordination) - B/op",
            "value": 288,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/check-source-uid (orbitjob/internal/core/domain/coordination) - allocs/op",
            "value": 7,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/kubernetes-cr-uid (orbitjob/internal/core/domain/coordination)",
            "value": 1644,
            "unit": "ns/op\t     352 B/op\t       7 allocs/op",
            "extra": "744615 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/kubernetes-cr-uid (orbitjob/internal/core/domain/coordination) - ns/op",
            "value": 1644,
            "unit": "ns/op",
            "extra": "744615 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/kubernetes-cr-uid (orbitjob/internal/core/domain/coordination) - B/op",
            "value": 352,
            "unit": "B/op",
            "extra": "744615 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/kubernetes-cr-uid (orbitjob/internal/core/domain/coordination) - allocs/op",
            "value": 7,
            "unit": "allocs/op",
            "extra": "744615 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/long-namespaced-name (orbitjob/internal/core/domain/coordination)",
            "value": 1584,
            "unit": "ns/op\t     384 B/op\t       7 allocs/op",
            "extra": "643281 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/long-namespaced-name (orbitjob/internal/core/domain/coordination) - ns/op",
            "value": 1584,
            "unit": "ns/op",
            "extra": "643281 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/long-namespaced-name (orbitjob/internal/core/domain/coordination) - B/op",
            "value": 384,
            "unit": "B/op",
            "extra": "643281 times\n4 procs"
          },
          {
            "name": "BenchmarkOccurrenceKey/long-namespaced-name (orbitjob/internal/core/domain/coordination) - allocs/op",
            "value": 7,
            "unit": "allocs/op",
            "extra": "643281 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/valid/single-edge (orbitjob/internal/core/domain/jobrun)",
            "value": 40.85,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "28160574 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/valid/single-edge (orbitjob/internal/core/domain/jobrun) - ns/op",
            "value": 40.85,
            "unit": "ns/op",
            "extra": "28160574 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/valid/single-edge (orbitjob/internal/core/domain/jobrun) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "28160574 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/valid/single-edge (orbitjob/internal/core/domain/jobrun) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "28160574 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/invalid/terminal-rewrite (orbitjob/internal/core/domain/jobrun)",
            "value": 32.57,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "42344571 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/invalid/terminal-rewrite (orbitjob/internal/core/domain/jobrun) - ns/op",
            "value": 32.57,
            "unit": "ns/op",
            "extra": "42344571 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/invalid/terminal-rewrite (orbitjob/internal/core/domain/jobrun) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "42344571 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/invalid/terminal-rewrite (orbitjob/internal/core/domain/jobrun) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "42344571 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/valid/full-lifecycle (orbitjob/internal/core/domain/jobrun)",
            "value": 244.7,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "4898534 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/valid/full-lifecycle (orbitjob/internal/core/domain/jobrun) - ns/op",
            "value": 244.7,
            "unit": "ns/op",
            "extra": "4898534 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/valid/full-lifecycle (orbitjob/internal/core/domain/jobrun) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "4898534 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/valid/full-lifecycle (orbitjob/internal/core/domain/jobrun) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "4898534 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/can-transition/full-matrix (orbitjob/internal/core/domain/jobrun)",
            "value": 204.4,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "5848873 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/can-transition/full-matrix (orbitjob/internal/core/domain/jobrun) - ns/op",
            "value": 204.4,
            "unit": "ns/op",
            "extra": "5848873 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/can-transition/full-matrix (orbitjob/internal/core/domain/jobrun) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "5848873 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/can-transition/full-matrix (orbitjob/internal/core/domain/jobrun) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "5848873 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/terminal/all-phases (orbitjob/internal/core/domain/jobrun)",
            "value": 14.33,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "84756769 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/terminal/all-phases (orbitjob/internal/core/domain/jobrun) - ns/op",
            "value": 14.33,
            "unit": "ns/op",
            "extra": "84756769 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/terminal/all-phases (orbitjob/internal/core/domain/jobrun) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "84756769 times\n4 procs"
          },
          {
            "name": "BenchmarkTransition/terminal/all-phases (orbitjob/internal/core/domain/jobrun) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "84756769 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/valid/cron-schedule (orbitjob/internal/core/domain/check)",
            "value": 13682,
            "unit": "ns/op\t    6466 B/op\t      63 allocs/op",
            "extra": "86203 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/valid/cron-schedule (orbitjob/internal/core/domain/check) - ns/op",
            "value": 13682,
            "unit": "ns/op",
            "extra": "86203 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/valid/cron-schedule (orbitjob/internal/core/domain/check) - B/op",
            "value": 6466,
            "unit": "B/op",
            "extra": "86203 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/valid/cron-schedule (orbitjob/internal/core/domain/check) - allocs/op",
            "value": 63,
            "unit": "allocs/op",
            "extra": "86203 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/valid/interval-schedule (orbitjob/internal/core/domain/check)",
            "value": 2460,
            "unit": "ns/op\t     472 B/op\t      17 allocs/op",
            "extra": "460794 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/valid/interval-schedule (orbitjob/internal/core/domain/check) - ns/op",
            "value": 2460,
            "unit": "ns/op",
            "extra": "460794 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/valid/interval-schedule (orbitjob/internal/core/domain/check) - B/op",
            "value": 472,
            "unit": "B/op",
            "extra": "460794 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/valid/interval-schedule (orbitjob/internal/core/domain/check) - allocs/op",
            "value": 17,
            "unit": "allocs/op",
            "extra": "460794 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/invalid/malformed-cron (orbitjob/internal/core/domain/check)",
            "value": 2803,
            "unit": "ns/op\t    1080 B/op\t      36 allocs/op",
            "extra": "405151 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/invalid/malformed-cron (orbitjob/internal/core/domain/check) - ns/op",
            "value": 2803,
            "unit": "ns/op",
            "extra": "405151 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/invalid/malformed-cron (orbitjob/internal/core/domain/check) - B/op",
            "value": 1080,
            "unit": "B/op",
            "extra": "405151 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeCreate/invalid/malformed-cron (orbitjob/internal/core/domain/check) - allocs/op",
            "value": 36,
            "unit": "allocs/op",
            "extra": "405151 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/valid/full-config (orbitjob/internal/core/domain/check)",
            "value": 349.2,
            "unit": "ns/op\t     152 B/op\t       2 allocs/op",
            "extra": "3417372 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/valid/full-config (orbitjob/internal/core/domain/check) - ns/op",
            "value": 349.2,
            "unit": "ns/op",
            "extra": "3417372 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/valid/full-config (orbitjob/internal/core/domain/check) - B/op",
            "value": 152,
            "unit": "B/op",
            "extra": "3417372 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/valid/full-config (orbitjob/internal/core/domain/check) - allocs/op",
            "value": 2,
            "unit": "allocs/op",
            "extra": "3417372 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/valid/url-only-defaults (orbitjob/internal/core/domain/check)",
            "value": 258.2,
            "unit": "ns/op\t     144 B/op\t       1 allocs/op",
            "extra": "4566296 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/valid/url-only-defaults (orbitjob/internal/core/domain/check) - ns/op",
            "value": 258.2,
            "unit": "ns/op",
            "extra": "4566296 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/valid/url-only-defaults (orbitjob/internal/core/domain/check) - B/op",
            "value": 144,
            "unit": "B/op",
            "extra": "4566296 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/valid/url-only-defaults (orbitjob/internal/core/domain/check) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "4566296 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/invalid/bad-scheme (orbitjob/internal/core/domain/check)",
            "value": 241.6,
            "unit": "ns/op\t     192 B/op\t       2 allocs/op",
            "extra": "4965228 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/invalid/bad-scheme (orbitjob/internal/core/domain/check) - ns/op",
            "value": 241.6,
            "unit": "ns/op",
            "extra": "4965228 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/invalid/bad-scheme (orbitjob/internal/core/domain/check) - B/op",
            "value": 192,
            "unit": "B/op",
            "extra": "4965228 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/invalid/bad-scheme (orbitjob/internal/core/domain/check) - allocs/op",
            "value": 2,
            "unit": "allocs/op",
            "extra": "4965228 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/invalid/relative-url (orbitjob/internal/core/domain/check)",
            "value": 149.2,
            "unit": "ns/op\t     192 B/op\t       2 allocs/op",
            "extra": "8013678 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/invalid/relative-url (orbitjob/internal/core/domain/check) - ns/op",
            "value": 149.2,
            "unit": "ns/op",
            "extra": "8013678 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/invalid/relative-url (orbitjob/internal/core/domain/check) - B/op",
            "value": 192,
            "unit": "B/op",
            "extra": "8013678 times\n4 procs"
          },
          {
            "name": "BenchmarkNormalizeProbeConfig/invalid/relative-url (orbitjob/internal/core/domain/check) - allocs/op",
            "value": 2,
            "unit": "allocs/op",
            "extra": "8013678 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildJob/name-within-dns-limit (orbitjob/internal/core/app/execution)",
            "value": 1184,
            "unit": "ns/op\t    2384 B/op\t       9 allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildJob/name-within-dns-limit (orbitjob/internal/core/app/execution) - ns/op",
            "value": 1184,
            "unit": "ns/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildJob/name-within-dns-limit (orbitjob/internal/core/app/execution) - B/op",
            "value": 2384,
            "unit": "B/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildJob/name-within-dns-limit (orbitjob/internal/core/app/execution) - allocs/op",
            "value": 9,
            "unit": "allocs/op",
            "extra": "1000000 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildJob/long-name-hashed (orbitjob/internal/core/app/execution)",
            "value": 1701,
            "unit": "ns/op\t    2720 B/op\t      13 allocs/op",
            "extra": "629004 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildJob/long-name-hashed (orbitjob/internal/core/app/execution) - ns/op",
            "value": 1701,
            "unit": "ns/op",
            "extra": "629004 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildJob/long-name-hashed (orbitjob/internal/core/app/execution) - B/op",
            "value": 2720,
            "unit": "B/op",
            "extra": "629004 times\n4 procs"
          },
          {
            "name": "BenchmarkBuildJob/long-name-hashed (orbitjob/internal/core/app/execution) - allocs/op",
            "value": 13,
            "unit": "allocs/op",
            "extra": "629004 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/availability-default (orbitjob/internal/core/app/checkobserve)",
            "value": 5.985,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "199316590 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/availability-default (orbitjob/internal/core/app/checkobserve) - ns/op",
            "value": 5.985,
            "unit": "ns/op",
            "extra": "199316590 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/availability-default (orbitjob/internal/core/app/checkobserve) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "199316590 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/availability-default (orbitjob/internal/core/app/checkobserve) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "199316590 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/latency-threshold (orbitjob/internal/core/app/checkobserve)",
            "value": 74.98,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "16015324 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/latency-threshold (orbitjob/internal/core/app/checkobserve) - ns/op",
            "value": 74.98,
            "unit": "ns/op",
            "extra": "16015324 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/latency-threshold (orbitjob/internal/core/app/checkobserve) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "16015324 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/latency-threshold (orbitjob/internal/core/app/checkobserve) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "16015324 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/status-criterion (orbitjob/internal/core/app/checkobserve)",
            "value": 52.91,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "23016102 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/status-criterion (orbitjob/internal/core/app/checkobserve) - ns/op",
            "value": 52.91,
            "unit": "ns/op",
            "extra": "23016102 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/status-criterion (orbitjob/internal/core/app/checkobserve) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "23016102 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/status-criterion (orbitjob/internal/core/app/checkobserve) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "23016102 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/unknown-producer-key (orbitjob/internal/core/app/checkobserve)",
            "value": 253,
            "unit": "ns/op\t      96 B/op\t       3 allocs/op",
            "extra": "4741190 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/unknown-producer-key (orbitjob/internal/core/app/checkobserve) - ns/op",
            "value": 253,
            "unit": "ns/op",
            "extra": "4741190 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/unknown-producer-key (orbitjob/internal/core/app/checkobserve) - B/op",
            "value": 96,
            "unit": "B/op",
            "extra": "4741190 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/is-good/unknown-producer-key (orbitjob/internal/core/app/checkobserve) - allocs/op",
            "value": 3,
            "unit": "allocs/op",
            "extra": "4741190 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/occurrence-run-id (orbitjob/internal/core/app/checkobserve)",
            "value": 69.14,
            "unit": "ns/op\t      48 B/op\t       1 allocs/op",
            "extra": "16897338 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/occurrence-run-id (orbitjob/internal/core/app/checkobserve) - ns/op",
            "value": 69.14,
            "unit": "ns/op",
            "extra": "16897338 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/occurrence-run-id (orbitjob/internal/core/app/checkobserve) - B/op",
            "value": 48,
            "unit": "B/op",
            "extra": "16897338 times\n4 procs"
          },
          {
            "name": "BenchmarkTerminalOutcomeDerivation/occurrence-run-id (orbitjob/internal/core/app/checkobserve) - allocs/op",
            "value": 1,
            "unit": "allocs/op",
            "extra": "16897338 times\n4 procs"
          },
          {
            "name": "BenchmarkCampaign (orbitjob/internal/platform/election)",
            "value": 729.6,
            "unit": "ns/op\t     303 B/op\t       7 allocs/op",
            "extra": "1534656 times\n4 procs"
          },
          {
            "name": "BenchmarkCampaign (orbitjob/internal/platform/election) - ns/op",
            "value": 729.6,
            "unit": "ns/op",
            "extra": "1534656 times\n4 procs"
          },
          {
            "name": "BenchmarkCampaign (orbitjob/internal/platform/election) - B/op",
            "value": 303,
            "unit": "B/op",
            "extra": "1534656 times\n4 procs"
          },
          {
            "name": "BenchmarkCampaign (orbitjob/internal/platform/election) - allocs/op",
            "value": 7,
            "unit": "allocs/op",
            "extra": "1534656 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLock (orbitjob/internal/platform/election)",
            "value": 2847,
            "unit": "ns/op\t     255 B/op\t       7 allocs/op",
            "extra": "503218 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLock (orbitjob/internal/platform/election) - ns/op",
            "value": 2847,
            "unit": "ns/op",
            "extra": "503218 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLock (orbitjob/internal/platform/election) - B/op",
            "value": 255,
            "unit": "B/op",
            "extra": "503218 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLock (orbitjob/internal/platform/election) - allocs/op",
            "value": 7,
            "unit": "allocs/op",
            "extra": "503218 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=2 (orbitjob/internal/platform/election)",
            "value": 68.26,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "24762102 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=2 (orbitjob/internal/platform/election) - ns/op",
            "value": 68.26,
            "unit": "ns/op",
            "extra": "24762102 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=2 (orbitjob/internal/platform/election) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "24762102 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=2 (orbitjob/internal/platform/election) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "24762102 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=4 (orbitjob/internal/platform/election)",
            "value": 111.9,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "13987046 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=4 (orbitjob/internal/platform/election) - ns/op",
            "value": 111.9,
            "unit": "ns/op",
            "extra": "13987046 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=4 (orbitjob/internal/platform/election) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "13987046 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=4 (orbitjob/internal/platform/election) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "13987046 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=8 (orbitjob/internal/platform/election)",
            "value": 154.7,
            "unit": "ns/op\t       0 B/op\t       0 allocs/op",
            "extra": "13072690 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=8 (orbitjob/internal/platform/election) - ns/op",
            "value": 154.7,
            "unit": "ns/op",
            "extra": "13072690 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=8 (orbitjob/internal/platform/election) - B/op",
            "value": 0,
            "unit": "B/op",
            "extra": "13072690 times\n4 procs"
          },
          {
            "name": "BenchmarkTryLockContention/concurrency=8 (orbitjob/internal/platform/election) - allocs/op",
            "value": 0,
            "unit": "allocs/op",
            "extra": "13072690 times\n4 procs"
          }
        ]
      }
    ]
  }
}