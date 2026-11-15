package sli

import "time"

// Snapshot is the persisted SLI state.
type Snapshot struct {
	ID                   int64          `json:"id"`
	TenantID             string         `json:"tenant_id"`
	Name                 string         `json:"name"`
	Description          *string        `json:"description,omitempty"`
	SLIType              string         `json:"sli_type"`
	SourceType           string         `json:"source_type"`
	SourceConfig         map[string]any `json:"source_config"`
	Aggregation          string         `json:"aggregation"`
	GoodEventCriteria    map[string]any `json:"good_event_criteria"`
	Version              int            `json:"version"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
	ConfigurationVersion int            `json:"configuration_version"`
	StatisticsSemantics  string         `json:"statistics_semantics"`
	ActiveFrom           time.Time      `json:"active_from"`
	LastEventAt          *time.Time     `json:"last_event_at,omitempty"`
	LastSampleAt         *time.Time     `json:"last_sample_at,omitempty"`
	LastObservedAt       *time.Time     `json:"last_observed_at,omitempty"`
}
