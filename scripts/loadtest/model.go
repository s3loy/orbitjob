package main

import "time"

type Verdict string

const (
	VerdictPass         Verdict = "PASS"
	VerdictFail         Verdict = "FAIL"
	VerdictInconclusive Verdict = "INCONCLUSIVE"
)

// TenantIDLen is the exact length of a tenant identifier. Every tenants.id is
// a CHAR(26) ULID and the API rejects anything else, so a profile that lists
// slugs would fail at trigger time rather than at decode time unless
// validation refuses it here.
const TenantIDLen = 26

type Config struct {
	Profile          string            `yaml:"profile"`
	Qualification    bool              `yaml:"qualification"`
	Seed             string            `yaml:"seed"`
	Duration         time.Duration     `yaml:"-"`
	DurationText     string            `yaml:"duration"`
	MinimumInstances int               `yaml:"minimum_instances"`
	Environment      EnvironmentConfig `yaml:"environment"`
	Definitions      DefinitionConfig  `yaml:"definitions"`
	// Tenants are the tenant identifiers this profile runs against, each a
	// 26-character ULID. There is exactly one execution path (the Kubernetes
	// control plane), so a profile declares who runs, not how.
	Tenants  []string       `yaml:"tenants"`
	Phases   []Phase        `yaml:"phases"`
	Burst    BurstConfig    `yaml:"burst"`
	Sampling SamplingConfig `yaml:"sampling"`
	Dynamic  DynamicConfig  `yaml:"dynamic"`
	Faults   FaultsConfig   `yaml:"faults"`
}

// FaultsConfig optionally overrides the fault injection plan. Profiles that
// omit it fall back to StandardFaultPlan. The standard qualification profile
// must not override it: the fixed plan is part of the qualification spec.
type FaultsConfig struct {
	Plan []FaultPhase `yaml:"plan"`
}

type FaultPhase struct {
	Name       string        `yaml:"name"`
	OffsetText string        `yaml:"offset"`
	Offset     time.Duration `yaml:"-"`
}

type DynamicConfig struct {
	Enabled  bool                `yaml:"enabled"`
	Resource ResourceModelConfig `yaml:"resource"`
	Feedback FeedbackConfig      `yaml:"feedback"`
}

func (c Config) DynamicTuningEnabled() bool { return c.Dynamic.Enabled }

type ResourceModelConfig struct {
	TaskAvgDurationSec int     `yaml:"task_avg_duration_sec"`
	SystemReserveCPU   float64 `yaml:"system_reserve_cpu"`
	SystemReserveMemGi float64 `yaml:"system_reserve_mem_gib"`
	HeadroomFactor     float64 `yaml:"headroom_factor"`
	PeakOvershoot      float64 `yaml:"peak_overshoot"`
}

type FeedbackConfig struct {
	PrometheusURL         string  `yaml:"prometheus_url"`
	SampleIntervalSec     int     `yaml:"sample_interval_sec"`
	LatencyThresholdSec   float64 `yaml:"latency_threshold_sec"`
	HighPressureThreshold float64 `yaml:"high_pressure_threshold"`
	LowPressureThreshold  float64 `yaml:"low_pressure_threshold"`
	MinPace               float64 `yaml:"min_pace"`
	MaxPace               float64 `yaml:"max_pace"`
}

type EnvironmentConfig struct {
	DockerCPU      int `yaml:"docker_cpu"`
	DockerMemoryGi int `yaml:"docker_memory_gib"`
	KindNodes      int `yaml:"kind_nodes"`
}

type DefinitionConfig struct {
	Total               int            `yaml:"total"`
	Categories          map[string]int `yaml:"categories"`
	ProductTriggerTypes map[string]int `yaml:"product_trigger_types"`
	TriggerOrigins      map[string]int `yaml:"trigger_origins"`
	CronIntervalMinutes int            `yaml:"cron_interval_minutes"`
}

type Phase struct {
	Name          string        `yaml:"name"`
	Offset        time.Duration `yaml:"-"`
	OffsetText    string        `yaml:"offset"`
	Duration      time.Duration `yaml:"-"`
	DurationText  string        `yaml:"duration"`
	RatePerMinute int           `yaml:"rate_per_minute"`
	MaxActive     int           `yaml:"max_active"`
}

type BurstConfig struct {
	Phase            string        `yaml:"phase"`
	Offset           time.Duration `yaml:"-"`
	OffsetText       string        `yaml:"offset"`
	Count            int           `yaml:"count"`
	SubmitWithin     time.Duration `yaml:"-"`
	SubmitWithinText string        `yaml:"submit_within"`
}

type SamplingConfig struct {
	ReadinessText         string `yaml:"readiness"`
	KubernetesObjectsText string `yaml:"kubernetes_objects"`
	ResourcesText         string `yaml:"resources"`
	PrometheusText        string `yaml:"prometheus"`
	PostgresText          string `yaml:"postgres"`
	BacklogText           string `yaml:"backlog"`
	DatabaseSizeText      string `yaml:"database_size"`
}

type Definition struct {
	CaseID             string `json:"case_id"`
	Tenant             string `json:"tenant"`
	Category           string `json:"category"`
	ProductTriggerType string `json:"product_trigger_type"`
	TriggerOrigin      string `json:"trigger_origin"`
	// ScheduledJob is the Custom Resource spec this definition is declared
	// with. The Admin API has no create route: definitions are ScheduledJob
	// custom resources the operator materializes into revisions.
	ScheduledJob ScheduledJobSpec `json:"scheduled_job"`
	Expected     Expected         `json:"expected"`
}

// ScheduledJobSpec is the subset of the ScheduledJob CRD the load corpus
// needs. Env and service account fields are absent on purpose: the operator
// renders Kubernetes Jobs with image, command and args only, so scenario
// bodies receive their configuration as baked-in argument values.
type ScheduledJobSpec struct {
	// Schedule is a standard cron expression. Definitions that are only ever
	// triggered manually carry neverFiresCron: the CRD requires a non-empty
	// schedule, while an expression that matches no instant keeps the
	// scheduler out of the way.
	Schedule string `json:"schedule"`
	// History limits. The default retention (3 per outcome) would prune
	// evidence mid-run, so generated definitions raise both; the load tool
	// verifies rows the run created, not rows retention kept.
	HistorySuccessful int      `json:"history_successful"`
	HistoryFailed     int      `json:"history_failed"`
	TimeoutSeconds    int      `json:"timeout_seconds"`
	Image             string   `json:"image"`
	Command           []string `json:"command,omitempty"`
	Args              []string `json:"args,omitempty"`
	BackoffLimit      int32    `json:"backoff_limit"`
}

// neverFiresCron is minute 0, hour 0, February 30th. It parses as a standard
// five-field cron expression and matches no instant in any year, so a manual
// definition satisfies the CRD's non-empty schedule without the scheduler
// ever creating occurrences for it.
const neverFiresCron = "0 0 30 2 *"

type Expected struct {
	TerminalState string `json:"terminal_state"`
	Attempts      int    `json:"attempts"`
	Core          bool   `json:"core"`
}

type RunRecord struct {
	RunID         string      `json:"run_id"`
	Profile       string      `json:"profile"`
	Qualification bool        `json:"qualification"`
	Seed          string      `json:"seed"`
	Commit        string      `json:"commit"`
	Dirty         bool        `json:"dirty"`
	StartedAt     time.Time   `json:"started_at"`
	Environment   Environment `json:"environment"`
}

type Environment struct {
	DockerCPU         int   `json:"docker_cpu"`
	DockerMemoryBytes int64 `json:"docker_memory_bytes"`
}

type CheckResult struct {
	ID       string   `json:"id"`
	Status   Verdict  `json:"status"`
	Observed any      `json:"observed"`
	Expected any      `json:"expected"`
	Evidence []string `json:"evidence"`
	Message  string   `json:"message"`
}

// RunStats is written by the run command and read by verify/report. Completed
// is false when the engine stopped early (timeout) — a truncated run cannot
// produce a PASS verdict.
type RunStats struct {
	RunID string `json:"run_id"`
	// Profile and Seed record what this run was, so a later verify cannot
	// relabel it. Without them the run record took its profile from whatever
	// --config the verify caller passed, and a smoke run verified without one
	// was written down as a qualification run.
	Profile         string             `json:"profile"`
	Seed            string             `json:"seed"`
	Qualification   bool               `json:"qualification"`
	StartedAt       time.Time          `json:"started_at"`
	FinishedAt      time.Time          `json:"finished_at"`
	ScheduledEvents int                `json:"scheduled_events"`
	Triggered       int64              `json:"triggered"`
	Accepted        int64              `json:"accepted"`
	Rejected        int64              `json:"rejected"`
	Skipped         int64              `json:"skipped"`
	Breakdown       RejectionBreakdown `json:"breakdown"`
	Completed       bool               `json:"completed"`
	Faults          []FaultRecord      `json:"faults,omitempty"`
}

type Result struct {
	RunID         string        `json:"run_id"`
	Qualification bool          `json:"qualification"`
	Verdict       Verdict       `json:"verdict"`
	Reason        string        `json:"reason"`
	Checks        []CheckResult `json:"checks"`
	CompletedAt   time.Time     `json:"completed_at"`
}
