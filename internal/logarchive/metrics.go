package logarchive

import "github.com/prometheus/client_golang/prometheus"

var collectedRecords = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "orbitjob_archive_records_total", Help: "Execution evidence records accepted or rejected."}, []string{"result"})
var observationErrors = prometheus.NewCounter(prometheus.CounterOpts{Name: "orbitjob_archive_observation_errors_total", Help: "Bounded execution observation failures."})

func init() { prometheus.MustRegister(collectedRecords, observationErrors) }
