package quality

import (
	"fmt"
	"sort"

	"github.com/wyw14/solo-035/internal/model"
)

// Profile is the broadcast family an event is produced as. It aliases the
// canonical model type so quality callers do not need to import model just
// to select a rule profile.
type Profile = model.Profile

const (
	ProfileStandard = model.ProfileStandard
	ProfileNews     = model.ProfileNews
	ProfileSports   = model.ProfileSports
	ProfileMusic    = model.ProfileMusic
)

// Thresholds are the numeric loudness limits and freshness bound applied to a
// single audio measurement.
type Thresholds struct {
	MinimumLoudness  float64 `json:"minimum_loudness"`
	MaximumLoudness  float64 `json:"maximum_loudness"`
	MaximumAgeMillis int64   `json:"maximum_age_millis"`
}

// RuleSet is one immutable quality requirement. A rule set records the
// thresholds, the kinds of measurement points it recognises, how many
// independent sources must report, and which kinds must be present per source.
type RuleSet struct {
	ID         string        `json:"id"`
	Profile    model.Profile `json:"profile"`
	Version    int           `json:"version"`
	Published  bool          `json:"published"`
	Thresholds Thresholds    `json:"thresholds"`
	// RequiredKinds is the ordered set of measurement kinds every independent
	// source must provide, e.g. waveform + integrated-loudness.
	RequiredKinds []string `json:"required_kinds"`
	// MinimumIndependentSources is the number of distinct probe origins that
	// must contribute evidence.
	MinimumIndependentSources int `json:"minimum_independent_sources"`
}

func (rule RuleSet) Validate() error {
	if rule.ID == "" {
		return fmt.Errorf("quality rule set has no identity")
	}
	if rule.Profile == "" {
		return fmt.Errorf("quality rule set %s has no broadcast profile", rule.ID)
	}
	if rule.Version < 1 {
		return fmt.Errorf("quality rule set %s has an invalid version", rule.ID)
	}
	if rule.Thresholds.MinimumLoudness >= rule.Thresholds.MaximumLoudness {
		return fmt.Errorf("quality rule set %s has an invalid loudness range", rule.ID)
	}
	if rule.Thresholds.MaximumAgeMillis < 1 {
		return fmt.Errorf("quality rule set %s requires a positive evidence age", rule.ID)
	}
	if len(rule.RequiredKinds) == 0 {
		return fmt.Errorf("quality rule set %s requires at least one measurement kind", rule.ID)
	}
	seen := make(map[string]bool, len(rule.RequiredKinds))
	for _, kind := range rule.RequiredKinds {
		if kind == "" {
			return fmt.Errorf("quality rule set %s has an empty measurement kind", rule.ID)
		}
		if seen[kind] {
			return fmt.Errorf("quality rule set %s lists measurement kind %s more than once", rule.ID, kind)
		}
		seen[kind] = true
	}
	if rule.MinimumIndependentSources < 1 {
		return fmt.Errorf("quality rule set %s requires at least one independent source", rule.ID)
	}
	return nil
}

// SortedKinds returns the required measurement kinds in a stable order so
// comparisons and reports are deterministic regardless of input order.
func SortedKinds(kinds []string) []string {
	sorted := append([]string(nil), kinds...)
	sort.Strings(sorted)
	return sorted
}
