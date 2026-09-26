package quality

import (
	"fmt"
	"time"
)

// RuleVersion is the natural identity of an immutable rule set:
// an event type together with a monotonically increasing version number.
type RuleVersion struct {
	EventType string `json:"event_type"`
	Version   int    `json:"version"`
}

// Thresholds bounds the loudness and freshness of acceptable audio evidence.
type Thresholds struct {
	MinimumLoudness  float64 `json:"minimum_loudness"`
	MaximumLoudness  float64 `json:"maximum_loudness"`
	MaximumAgeMillis int64   `json:"maximum_age_millis"`
}

// ProbeRequirement states how many independent measurement sources must
// provide evidence of a particular probe kind.
type ProbeRequirement struct {
	Kind           string `json:"kind"`
	MinimumSources int    `json:"minimum_sources"`
}

type RuleSetStatus string

const (
	RuleSetPublished  RuleSetStatus = "published"
	RuleSetDeprecated RuleSetStatus = "deprecated"
)

// RuleSet is an immutable, versioned set of on-air quality requirements
// for one kind of live event.
type RuleSet struct {
	EventType   string             `json:"event_type"`
	Version     int                `json:"version"`
	Thresholds  Thresholds         `json:"thresholds"`
	Probes      []ProbeRequirement `json:"probes"`
	Status      RuleSetStatus      `json:"status"`
	PublishedAt time.Time          `json:"published_at"`
}

func (set RuleSet) Key() RuleVersion {
	return RuleVersion{EventType: set.EventType, Version: set.Version}
}

func (set RuleSet) Validate() error {
	if set.EventType == "" {
		return fmt.Errorf("rule set has no event type")
	}
	if set.Version < 1 {
		return fmt.Errorf("rule set version must be positive")
	}
	if set.Thresholds.MinimumLoudness >= set.Thresholds.MaximumLoudness {
		return fmt.Errorf("quality thresholds are invalid")
	}
	if set.Thresholds.MaximumAgeMillis < 1 {
		return fmt.Errorf("quality evidence age must be positive")
	}
	if len(set.Probes) == 0 {
		return fmt.Errorf("rule set requires at least one probe kind")
	}
	kinds := make(map[string]bool, len(set.Probes))
	for _, probe := range set.Probes {
		if probe.Kind == "" {
			return fmt.Errorf("probe requirement has no kind")
		}
		if probe.MinimumSources < 1 {
			return fmt.Errorf("probe requirement %s must demand at least one source", probe.Kind)
		}
		if kinds[probe.Kind] {
			return fmt.Errorf("probe requirement %s is duplicated", probe.Kind)
		}
		kinds[probe.Kind] = true
	}
	if set.Status != RuleSetPublished && set.Status != RuleSetDeprecated {
		return fmt.Errorf("rule set has an invalid status")
	}
	return nil
}

func cloneProbes(probes []ProbeRequirement) []ProbeRequirement {
	return append([]ProbeRequirement(nil), probes...)
}

func (set RuleSet) clone() RuleSet {
	set.Probes = cloneProbes(set.Probes)
	return set
}
