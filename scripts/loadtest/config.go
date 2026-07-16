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
	return cfg, nil
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

func sumCounts(values map[string]int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}
