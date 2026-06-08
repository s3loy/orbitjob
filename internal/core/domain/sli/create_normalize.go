package sli

import (
	"fmt"
	"strings"

	"orbitjob/internal/domain/validation"
)

// NormalizeCreate validates and normalizes a CreateInput into a CreateSpec.
func NormalizeCreate(in CreateInput) (CreateSpec, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return CreateSpec{}, validation.New("name", "name is required")
	}
	if len(name) > MaxNameLength {
		return CreateSpec{}, validation.New("name", fmt.Sprintf("name must be at most %d characters", MaxNameLength))
	}

	sliType := in.SLIType
	if sliType == "" {
		sliType = TypeAvailability
	}
	if !ValidSLITypes[sliType] {
		return CreateSpec{}, validation.New("sli_type", fmt.Sprintf("unsupported sli_type: %s", sliType))
	}

	sourceType := in.SourceType
	if sourceType == "" {
		sourceType = SourceTypeCheckRun
	}
	if !ValidSourceTypes[sourceType] {
		return CreateSpec{}, validation.New("source_type", fmt.Sprintf("unsupported source_type: %s", sourceType))
	}

	aggregation := in.Aggregation
	if aggregation == "" {
		aggregation = AggregationRatio
	}
	if !ValidAggregations[aggregation] {
		return CreateSpec{}, validation.New("aggregation", fmt.Sprintf("unsupported aggregation: %s", aggregation))
	}

	// Validate source_config contains required fields for check_run source.
	if sourceType == SourceTypeCheckRun {
		if in.SourceConfig == nil {
			return CreateSpec{}, validation.New("source_config", "source_config is required for check_run source")
		}
		checkID, ok := in.SourceConfig["check_id"]
		if !ok {
			return CreateSpec{}, validation.New("source_config.check_id", "check_id is required in source_config")
		}
		var checkIDFloat float64
		switch v := checkID.(type) {
		case float64:
			checkIDFloat = v
		case int64:
			checkIDFloat = float64(v)
		case int:
			checkIDFloat = float64(v)
		default:
			return CreateSpec{}, validation.New("source_config.check_id", "check_id must be a positive integer")
		}
		if checkIDFloat < 1 || checkIDFloat != float64(int64(checkIDFloat)) {
			return CreateSpec{}, validation.New("source_config.check_id", "check_id must be a positive integer")
		}
	}

	// good_event_criteria is required for availability/quality types.
	if sliType == TypeAvailability || sliType == TypeQuality {
		if len(in.GoodEventCriteria) == 0 {
			return CreateSpec{}, validation.New("good_event_criteria", "good_event_criteria is required for this sli_type")
		}
	}

	return CreateSpec{
		Name:              name,
		Description:       in.Description,
		SLIType:           sliType,
		SourceType:        sourceType,
		SourceConfig:      in.SourceConfig,
		Aggregation:       aggregation,
		GoodEventCriteria: in.GoodEventCriteria,
	}, nil
}
