package main

import (
	"testing"
	"time"
)

func TestStaticProfilesRejectNonProgressingSchedules(t *testing.T) {
	for _, profile := range []string{"standard", "smoke", "long"} {
		for _, rate := range []int{-1, 0, int(time.Minute) + 1} {
			cfg, err := LoadConfig("../../test/load/config/" + profile + ".yaml")
			if err != nil {
				t.Fatal(err)
			}
			cfg.Dynamic.Enabled = false
			for i := range cfg.Phases {
				cfg.Phases[i].RatePerMinute = 10
				cfg.Phases[i].MaxActive = 10
			}
			cfg.Phases[0].RatePerMinute = rate
			if err := validateProfile(profile, cfg); err == nil {
				t.Errorf("%s accepted rate %d", profile, rate)
			}
		}
	}
}

func TestPhaseDurationMustBePositive(t *testing.T) {
	cfg := Config{Duration: time.Minute, Phases: []Phase{
		{Name: "reverse", Duration: -time.Minute},
		{Name: "catch-up", Offset: -time.Minute, Duration: 2 * time.Minute},
	}}
	if err := validatePhaseContinuity(cfg); err == nil {
		t.Fatal("accepted a negative phase duration")
	}
}
