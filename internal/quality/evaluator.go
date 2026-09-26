package quality

import (
	"fmt"
	"sort"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

// Violation codes describe the individual rule a piece of evidence or an
// event fails. Exception approvals waive these codes.
const (
	CodeLoudness           = "loudness"
	CodeFreshness          = "freshness"
	CodeRequiredKind       = "required-kind"
	CodeIndependentSources = "independent-sources"
)

// Violation is one failed rule observation, recorded against the rule set in
// force for the decision.
type Violation struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// Result is the evaluation of an evidence set against one immutable rule set.
type Result struct {
	Rule               RuleSet     `json:"rule"`
	Passed             bool        `json:"passed"`
	IndependentSources int         `json:"independent_sources"`
	ConsideredEvidence int         `json:"considered_evidence"`
	Violations         []Violation `json:"violations"`
	AppliedExceptions  []Exception `json:"applied_exceptions"`
}

// Admit checks a single measurement against the rule set and any active
// exception approvals. It is the early gate used while interlocks are being
// established; Permit runs the same checks again across the whole set.
func Admit(rule RuleSet, eventID model.EventID, revision model.RevisionID, item model.AudioEvidence, at time.Time, exceptions []Exception) error {
	if item.ProbeID == "" {
		return fmt.Errorf("audio evidence has no probe identity")
	}
	if !containsKind(rule.RequiredKinds, item.Kind) {
		return fmt.Errorf("measurement kind %q is not accepted by rule %s", item.Kind, rule.ID)
	}
	waived := waivedCodes(eventID, revision, at, exceptions)
	if (item.Value < rule.Thresholds.MinimumLoudness || item.Value > rule.Thresholds.MaximumLoudness) && !waived[CodeLoudness] {
		return fmt.Errorf("audio evidence is outside configured range")
	}
	if isStale(rule, item.MeasuredAt, at) && !waived[CodeFreshness] {
		return fmt.Errorf("audio evidence is older than %d ms", rule.Thresholds.MaximumAgeMillis)
	}
	return nil
}

// Evaluate re-runs every requirement of the rule set against the supplied
// evidence snapshot. at is the decision instant: historical decisions pass
// their original decision time so the same freshness verdict is reproduced.
func Evaluate(rule RuleSet, eventID model.EventID, revision model.RevisionID, items []model.AudioEvidence, at time.Time, exceptions []Exception) Result {
	result := Result{Rule: rule}
	waived := waivedCodes(eventID, revision, at, exceptions)
	required := make(map[string]bool, len(rule.RequiredKinds))
	for _, kind := range rule.RequiredKinds {
		required[kind] = true
	}

	// origin -> kind -> one usable measurement. EvidenceSet.List is already
	// ordered by MeasuredAt, so the first fresh in-range reading wins.
	usable := make(map[string]map[string]model.AudioEvidence)
	for _, item := range items {
		if !required[item.Kind] {
			continue
		}
		result.ConsideredEvidence++
		origin := item.ProbeID
		if item.DerivedFrom != "" {
			origin = item.DerivedFrom
		}
		outOfRange := item.Value < rule.Thresholds.MinimumLoudness || item.Value > rule.Thresholds.MaximumLoudness
		stale := isStale(rule, item.MeasuredAt, at)
		if outOfRange {
			addViolation(&result, waived, CodeLoudness,
				fmt.Sprintf("probe %s %s reading %v is outside %v..%v", origin, item.Kind, item.Value, rule.Thresholds.MinimumLoudness, rule.Thresholds.MaximumLoudness), exceptions, eventID, revision, at)
		}
		if stale {
			addViolation(&result, waived, CodeFreshness,
				fmt.Sprintf("probe %s %s reading is older than %d ms", origin, item.Kind, rule.Thresholds.MaximumAgeMillis), exceptions, eventID, revision, at)
		}
		if outOfRange || stale {
			continue
		}
		if usable[origin] == nil {
			usable[origin] = make(map[string]model.AudioEvidence)
		}
		if _, exists := usable[origin][item.Kind]; !exists {
			usable[origin][item.Kind] = item
		}
	}

	origins := make([]string, 0, len(usable))
	for origin := range usable {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	for _, origin := range origins {
		for _, kind := range rule.RequiredKinds {
			if _, ok := usable[origin][kind]; !ok {
				addViolation(&result, waived, CodeRequiredKind,
					fmt.Sprintf("probe %s has no usable %s measurement", origin, kind), exceptions, eventID, revision, at)
			}
		}
	}

	for _, origin := range origins {
		complete := true
		for _, kind := range rule.RequiredKinds {
			if _, ok := usable[origin][kind]; !ok {
				complete = false
			}
		}
		if complete {
			result.IndependentSources++
		}
	}
	if result.IndependentSources < rule.MinimumIndependentSources {
		addViolation(&result, waived, CodeIndependentSources,
			fmt.Sprintf("rule %s requires %d independent sources, found %d", rule.ID, rule.MinimumIndependentSources, result.IndependentSources),
			exceptions, eventID, revision, at)
	}

	sort.Slice(result.Violations, func(i, j int) bool {
		if result.Violations[i].Code == result.Violations[j].Code {
			return result.Violations[i].Detail < result.Violations[j].Detail
		}
		return result.Violations[i].Code < result.Violations[j].Code
	})
	result.Passed = len(result.Violations) == 0
	return result
}

func isStale(rule RuleSet, measuredAt, at time.Time) bool {
	age := at.Sub(measuredAt)
	return age > time.Duration(rule.Thresholds.MaximumAgeMillis)*time.Millisecond
}

func containsKind(kinds []string, wanted string) bool {
	for _, kind := range kinds {
		if kind == wanted {
			return true
		}
	}
	return false
}

// waivedCodes returns the violation codes covered by exception approvals that
// are in force for this event revision at time at.
func waivedCodes(eventID model.EventID, revision model.RevisionID, at time.Time, exceptions []Exception) map[string]bool {
	codes := make(map[string]bool)
	for _, exception := range exceptions {
		if !exception.Applies(eventID, revision, at) {
			continue
		}
		if len(exception.Waive) == 0 {
			for _, code := range []string{CodeLoudness, CodeFreshness, CodeRequiredKind, CodeIndependentSources} {
				codes[code] = true
			}
			continue
		}
		for _, code := range exception.Waive {
			codes[code] = true
		}
	}
	return codes
}

func addViolation(result *Result, waived map[string]bool, code, detail string, exceptions []Exception, eventID model.EventID, revision model.RevisionID, at time.Time) {
	if waived[code] {
		for _, exception := range exceptions {
			if exception.Applies(eventID, revision, at) && exception.Waives(code) {
				result.AppliedExceptions = appendExceptionOnce(result.AppliedExceptions, exception)
			}
		}
		return
	}
	result.Violations = append(result.Violations, Violation{Code: code, Detail: detail})
}

func appendExceptionOnce(items []Exception, candidate Exception) []Exception {
	for _, item := range items {
		if item.ID == candidate.ID {
			return items
		}
	}
	return append(items, candidate)
}
