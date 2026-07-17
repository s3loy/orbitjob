package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

func LoadConfig(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer func() { _ = file.Close() }()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var cfg Config
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode load config: %w", err)
	}
	if cfg.Duration, err = time.ParseDuration(cfg.DurationText); err != nil {
		return Config{}, fmt.Errorf("parse duration: %w", err)
	}
	for i := range cfg.Phases {
		if cfg.Phases[i].Offset, err = time.ParseDuration(cfg.Phases[i].OffsetText); err != nil {
			return Config{}, fmt.Errorf("parse phase %s offset: %w", cfg.Phases[i].Name, err)
		}
		if cfg.Phases[i].Duration, err = time.ParseDuration(cfg.Phases[i].DurationText); err != nil {
			return Config{}, fmt.Errorf("parse phase %s duration: %w", cfg.Phases[i].Name, err)
		}
	}
	if cfg.Burst.Offset, err = time.ParseDuration(cfg.Burst.OffsetText); err != nil {
		return Config{}, fmt.Errorf("parse burst offset: %w", err)
	}
	if cfg.Burst.SubmitWithin, err = time.ParseDuration(cfg.Burst.SubmitWithinText); err != nil {
		return Config{}, fmt.Errorf("parse burst submit_within: %w", err)
	}
	for i := range cfg.Faults.Plan {
		if cfg.Faults.Plan[i].Offset, err = time.ParseDuration(cfg.Faults.Plan[i].OffsetText); err != nil {
			return Config{}, fmt.Errorf("parse fault %s offset: %w", cfg.Faults.Plan[i].Name, err)
		}
	}
	cfg = withDefaults(cfg)
	return cfg, nil
}

func withDefaults(cfg Config) Config {
	if cfg.Dynamic.Resource.TaskAvgDurationSec == 0 {
		cfg.Dynamic.Resource.TaskAvgDurationSec = 30
	}
	if cfg.Dynamic.Resource.SystemReserveCPU == 0 {
		cfg.Dynamic.Resource.SystemReserveCPU = 1.0
	}
	if cfg.Dynamic.Resource.SystemReserveMemGi == 0 {
		cfg.Dynamic.Resource.SystemReserveMemGi = 2.0
	}
	if cfg.Dynamic.Resource.HeadroomFactor == 0 {
		cfg.Dynamic.Resource.HeadroomFactor = 0.8
	}
	if cfg.Dynamic.Resource.PeakOvershoot == 0 {
		cfg.Dynamic.Resource.PeakOvershoot = 1.2
	}
	if cfg.Dynamic.Feedback.PrometheusURL == "" {
		cfg.Dynamic.Feedback.PrometheusURL = "http://localhost:9090"
	}
	if cfg.Dynamic.Feedback.SampleIntervalSec == 0 {
		cfg.Dynamic.Feedback.SampleIntervalSec = 15
	}
	if cfg.Dynamic.Feedback.LatencyThresholdSec == 0 {
		cfg.Dynamic.Feedback.LatencyThresholdSec = 0.5
	}
	if cfg.Dynamic.Feedback.HighPressureThreshold == 0 {
		cfg.Dynamic.Feedback.HighPressureThreshold = 0.7
	}
	if cfg.Dynamic.Feedback.LowPressureThreshold == 0 {
		cfg.Dynamic.Feedback.LowPressureThreshold = 0.3
	}
	if cfg.Dynamic.Feedback.MinPace == 0 {
		cfg.Dynamic.Feedback.MinPace = 0.5
	}
	if cfg.Dynamic.Feedback.MaxPace == 0 {
		cfg.Dynamic.Feedback.MaxPace = 1.5
	}
	return cfg
}

func ValidateStandard(c Config) error {
	switch {
	case !c.Qualification:
		return errors.New("standard profile must set qualification=true")
	case c.Seed != "v020-standard-1":
		return fmt.Errorf("seed must be v020-standard-1")
	case c.Duration != 4*time.Hour:
		return fmt.Errorf("duration must be exactly 4h0m0s")
	case c.Definitions.Total != 1200:
		return fmt.Errorf("definition total must be 1200")
	case c.MinimumInstances != 10000:
		return fmt.Errorf("minimum_instances must be 10000")
	case sumCounts(c.Definitions.Categories) != 1200:
		return fmt.Errorf("category counts must sum to 1200")
	case sumCounts(c.Definitions.ProductTriggerTypes) != 1200:
		return fmt.Errorf("product trigger counts must sum to 1200")
	case sumCounts(c.Definitions.TriggerOrigins) != 1200:
		return fmt.Errorf("trigger origin counts must sum to 1200")
	case len(c.Tenants) != 4:
		return fmt.Errorf("standard profile requires exactly four tenants")
	case c.Environment.DockerCPU != 10 || c.Environment.DockerMemoryGi != 8 || c.Environment.KindNodes != 1:
		return fmt.Errorf("standard environment must be Docker 10 CPU / 8 GiB and one kind node")
	}
	if len(c.Faults.Plan) > 0 {
		return errors.New("standard profile must not override the qualification fault plan")
	}
	if err := validatePhaseContinuity(c); err != nil {
		return err
	}
	if c.Dynamic.Enabled {
		if err := validateDynamicPhases(c); err != nil {
			return err
		}
		return nil
	}
	for _, phase := range c.Phases {
		if phase.RatePerMinute == 0 {
			return fmt.Errorf("phase %s must have rate_per_minute > 0 in static mode", phase.Name)
		}
		if phase.MaxActive == 0 {
			return fmt.Errorf("phase %s must have max_active > 0 in static mode", phase.Name)
		}
	}
	return nil
}

func validatePhaseContinuity(c Config) error {
	var end time.Duration
	for _, phase := range c.Phases {
		if phase.Offset != end {
			return fmt.Errorf("phase %s starts at %s, expected %s", phase.Name, phase.Offset, end)
		}
		end = phase.Offset + phase.Duration
	}
	if end != c.Duration {
		return fmt.Errorf("phases end at %s, expected %s", end, c.Duration)
	}
	return nil
}

func validateDynamicPhases(c Config) error {
	for _, phase := range c.Phases {
		if phase.RatePerMinute != 0 {
			return fmt.Errorf("phase %s must not set rate_per_minute when dynamic.enabled=true", phase.Name)
		}
		if phase.MaxActive != 0 {
			return fmt.Errorf("phase %s must not set max_active when dynamic.enabled=true", phase.Name)
		}
	}
	return nil
}

// validateFaults checks a custom fault plan: known component names, strictly
// increasing offsets, and every offset inside the run duration. An empty plan
// is valid and falls back to StandardFaultPlan at run time.
func validateFaults(c Config) error {
	var prev time.Duration
	for i, fault := range c.Faults.Plan {
		if !knownFault(fault.Name) {
			return fmt.Errorf("unknown fault %q", fault.Name)
		}
		if fault.Offset <= 0 || fault.Offset >= c.Duration {
			return fmt.Errorf("fault %s offset %s must be within run duration %s", fault.Name, fault.Offset, c.Duration)
		}
		if i > 0 && fault.Offset <= prev {
			return fmt.Errorf("fault offsets must be strictly increasing: %s at %s follows %s", fault.Name, fault.Offset, prev)
		}
		prev = fault.Offset
	}
	return nil
}

// ValidateSmoke checks a 30-minute non-qualification profile. It exercises the
// load pipeline and observability stack without the 4h / 10000-instance gates,
// so environment is relaxed and minimum_instances is not enforced.
func ValidateSmoke(c Config) error {
	switch {
	case c.Qualification:
		return errors.New("smoke profile must set qualification=false")
	case c.Duration != 30*time.Minute:
		return fmt.Errorf("smoke duration must be exactly 30m0s, got %s", c.Duration)
	case len(c.Tenants) != 4:
		return fmt.Errorf("smoke profile requires exactly four tenants, got %d", len(c.Tenants))
	case c.Definitions.Total <= 0:
		return errors.New("smoke profile requires definitions.total > 0")
	case sumCounts(c.Definitions.Categories) != c.Definitions.Total:
		return fmt.Errorf("category counts must sum to definitions.total %d", c.Definitions.Total)
	case c.Environment.KindNodes < 1:
		return errors.New("smoke profile requires at least one kind node")
	}
	if err := validatePhaseContinuity(c); err != nil {
		return err
	}
	if err := validateFaults(c); err != nil {
		return err
	}
	if c.Dynamic.Enabled {
		return validateDynamicPhases(c)
	}
	for _, phase := range c.Phases {
		if phase.RatePerMinute == 0 {
			return fmt.Errorf("phase %s must have rate_per_minute > 0 in static mode", phase.Name)
		}
		if phase.MaxActive == 0 {
			return fmt.Errorf("phase %s must have max_active > 0 in static mode", phase.Name)
		}
	}
	return nil
}

// ValidateLong checks an 8-hour non-qualification profile for extended soak
// testing. Same shape constraints as smoke (continuous phases, 4 tenants) but
// 8h duration so a clean pool can be exercised over a long window.
func ValidateLong(c Config) error {
	switch {
	case c.Qualification:
		return errors.New("long profile must set qualification=false")
	case c.Duration != 8*time.Hour:
		return fmt.Errorf("long duration must be exactly 8h0m0s, got %s", c.Duration)
	case len(c.Tenants) != 4:
		return fmt.Errorf("long profile requires exactly four tenants, got %d", len(c.Tenants))
	case c.Definitions.Total <= 0:
		return errors.New("long profile requires definitions.total > 0")
	case sumCounts(c.Definitions.Categories) != c.Definitions.Total:
		return fmt.Errorf("category counts must sum to definitions.total %d", c.Definitions.Total)
	case c.Environment.KindNodes < 1:
		return errors.New("long profile requires at least one kind node")
	}
	if err := validatePhaseContinuity(c); err != nil {
		return err
	}
	if err := validateFaults(c); err != nil {
		return err
	}
	if c.Dynamic.Enabled {
		return validateDynamicPhases(c)
	}
	for _, phase := range c.Phases {
		if phase.RatePerMinute == 0 {
			return fmt.Errorf("phase %s must have rate_per_minute > 0 in static mode", phase.Name)
		}
		if phase.MaxActive == 0 {
			return fmt.Errorf("phase %s must have max_active > 0 in static mode", phase.Name)
		}
	}
	return nil
}

func sumCounts(values map[string]int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}
