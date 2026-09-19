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
		sourceType = SourceTypeJobRun
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

	// The source_config must name the definition the SLI observes. The ledger
	// is the event source, so a source_uid is what ties the SLI to its runs.
	if in.SourceConfig == nil {
		return CreateSpec{}, validation.New("source_config", "source_config is required")
	}
	sourceUID, ok := in.SourceConfig["source_uid"]
	if !ok {
		return CreateSpec{}, validation.New("source_config.source_uid", "source_uid is required in source_config")
	}
	sourceUIDStr, ok := sourceUID.(string)
	if !ok || strings.TrimSpace(sourceUIDStr) == "" {
		return CreateSpec{}, validation.New("source_config.source_uid", "source_uid must be a non-empty string")
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
		ResourceGroupID:   strings.TrimSpace(in.ResourceGroupID),
		SLIType:           sliType,
		SourceType:        sourceType,
		SourceConfig:      in.SourceConfig,
		Aggregation:       aggregation,
		GoodEventCriteria: in.GoodEventCriteria,
	}, nil
}
