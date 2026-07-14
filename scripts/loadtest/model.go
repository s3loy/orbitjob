package main

import "time"

type Verdict string

const (
	VerdictPass         Verdict = "PASS"
	VerdictFail         Verdict = "FAIL"
	VerdictInconclusive Verdict = "INCONCLUSIVE"
)

type Config struct {
	SchemaVersion    string            `yaml:"schema_version"`
	Profile          string            `yaml:"profile"`
	Qualification    bool              `yaml:"qualification"`
	Seed             string            `yaml:"seed"`
	Duration         time.Duration     `yaml:"-"`
	DurationText     string            `yaml:"duration"`
	MinimumInstances int               `yaml:"minimum_instances"`
	Environment      EnvironmentConfig `yaml:"environment"`
	Definitions      DefinitionConfig  `yaml:"definitions"`
	Tenants          []string          `yaml:"tenants"`
	Phases           []Phase           `yaml:"phases"`
	Burst            BurstConfig       `yaml:"burst"`
	Sampling         SamplingConfig    `yaml:"sampling"`
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
	CaseID             string         `json:"case_id"`
	Tenant             string         `json:"tenant"`
	Category           string         `json:"category"`
	ProductTriggerType string         `json:"product_trigger_type"`
	TriggerOrigin      string         `json:"trigger_origin"`
	Request            map[string]any `json:"request"`
	Expected           Expected       `json:"expected"`
}

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

type Result struct {
	SchemaVersion string        `json:"schema_version"`
	RunID         string        `json:"run_id"`
	Qualification bool          `json:"qualification"`
	Verdict       Verdict       `json:"verdict"`
	Reason        string        `json:"reason"`
	Checks        []CheckResult `json:"checks"`
	CompletedAt   time.Time     `json:"completed_at"`
}
