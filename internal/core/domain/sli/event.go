package sli

import "time"

const (
	LegacyProcessing      = "legacy_processing_v1"
	EventTime             = "event_time_v2"
	MixedSemantics        = "mixed"
	TimeContainerFinished = "container_finished"
	TimeObserved          = "observed"
)

// EventConfiguration freezes one terminal Run's selection before derivatives.
// The plan remains stable across retries and later SLI creation or deletion.
type EventConfiguration struct {
	RunID               int64      `json:"run_id"`
	Semantics           string     `json:"semantics"`
	EventTime           time.Time  `json:"event_time"`
	TimeQuality         string     `json:"time_quality"`
	ContainerStartedAt  *time.Time `json:"container_started_at,omitempty"`
	ContainerFinishedAt *time.Time `json:"container_finished_at,omitempty"`
	Configurations      []Snapshot `json:"configurations"`
}

// Evaluation retains missing facts without adding fabricated bad samples.
type Evaluation struct {
	RunID                int64
	SLIID                int64
	ConfigurationVersion int
	Semantics            string
	EventTime            time.Time
	TimeQuality          string
	WindowStart          time.Time
	Good                 *bool
	MissingReason        string
}
