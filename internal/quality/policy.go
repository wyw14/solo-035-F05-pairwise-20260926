package quality

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

// Exception is an approval that waives one or more violation codes for an
// event (and, when set, one specific event revision). It is valid between its
// approval and expiry. An empty Waive list covers every code.
type Exception struct {
	ID         string           `json:"id"`
	EventID    model.EventID    `json:"event_id"`
	Revision   model.RevisionID `json:"revision,omitempty"`
	Waive      []string         `json:"waive,omitempty"`
	Reason     string           `json:"reason"`
	ApprovedBy string           `json:"approved_by"`
	ApprovedAt time.Time        `json:"approved_at"`
	ExpiresAt  *time.Time       `json:"expires_at,omitempty"`
}

func (exception Exception) Applies(eventID model.EventID, revision model.RevisionID, at time.Time) bool {
	if exception.EventID != eventID {
		return false
	}
	if exception.Revision != "" && exception.Revision != revision {
		return false
	}
	if at.Before(exception.ApprovedAt) {
		return false
	}
	if exception.ExpiresAt != nil && at.After(*exception.ExpiresAt) {
		return false
	}
	return true
}

func (exception Exception) Waives(code string) bool {
	if len(exception.Waive) == 0 {
		return true
	}
	for _, waived := range exception.Waive {
		if waived == code {
			return true
		}
	}
	return false
}

func (exception Exception) Validate() error {
	if exception.ID == "" {
		return fmt.Errorf("quality exception has no identity")
	}
	if !exception.EventID.Valid() {
		return fmt.Errorf("quality exception %s has no event", exception.ID)
	}
	if exception.Revision != "" && !exception.Revision.Valid() {
		return fmt.Errorf("quality exception %s has an invalid revision", exception.ID)
	}
	known := map[string]bool{CodeLoudness: true, CodeFreshness: true, CodeRequiredKind: true, CodeIndependentSources: true}
	for _, code := range exception.Waive {
		if !known[code] {
			return fmt.Errorf("quality exception %s waives unknown code %q", exception.ID, code)
		}
	}
	if exception.ExpiresAt != nil && exception.ExpiresAt.Before(exception.ApprovedAt) {
		return fmt.Errorf("quality exception %s expires before it is approved", exception.ID)
	}
	return nil
}

// RuleBinding records which immutable rule set an event revision was judged
// under. Once a revision is bound, later rule publications never change it.
type RuleBinding struct {
	EventID  model.EventID    `json:"event_id"`
	Revision model.RevisionID `json:"revision"`
	RuleID   string           `json:"rule_id"`
	BoundAt  time.Time        `json:"bound_at"`
}

// Decision is the immutable ledger entry for one on-air quality judgement.
// It snapshots the rule set, evidence and active exceptions at decision time
// so the historical clearance can be recomputed with the rules in force then.
type Decision struct {
	EventID    model.EventID         `json:"event_id"`
	Revision   model.RevisionID      `json:"revision"`
	Rule       RuleSet               `json:"rule"`
	Evidence   []model.AudioEvidence `json:"evidence"`
	Exceptions []Exception           `json:"exceptions"`
	Result     Result                `json:"result"`
	Permitted  bool                  `json:"permitted"`
	Reason     string                `json:"reason"`
	DecidedAt  time.Time             `json:"decided_at"`
}

// ReplayEntry compares a stored decision against a fresh evaluation using the
// rule set and evidence snapshotted on that decision.
type ReplayEntry struct {
	EventID      model.EventID    `json:"event_id"`
	Revision     model.RevisionID `json:"revision"`
	RuleID       string           `json:"rule_id"`
	RuleVersion  int              `json:"rule_version"`
	DecidedAt    time.Time        `json:"decided_at"`
	Permitted    bool             `json:"permitted"`
	StoredPassed bool             `json:"stored_passed"`
	Recomputed   Result           `json:"recomputed"`
	Unchanged    bool             `json:"unchanged"`
}

type ReplayReport struct {
	ReplayedAt time.Time     `json:"replayed_at"`
	Entries    []ReplayEntry `json:"entries"`
}

// Policy is the versioned quality rule ledger: published rule sets, the
// revision-scoped bindings, exception approvals and historical decisions.
type Policy struct {
	mu           sync.RWMutex
	clock        func() time.Time
	rules        map[string]RuleSet
	versions     map[model.Profile]int
	published    map[model.Profile][]string
	bindings     map[model.EventID]map[model.RevisionID]RuleBinding
	exceptions   map[string]Exception
	exceptionSeq int
	decisions    map[model.EventID][]Decision
}

func NewPolicy(clock func() time.Time) *Policy {
	if clock == nil {
		clock = time.Now
	}
	policy := &Policy{
		clock:      clock,
		rules:      make(map[string]RuleSet),
		versions:   make(map[Profile]int),
		published:  make(map[Profile][]string),
		bindings:   make(map[model.EventID]map[model.RevisionID]RuleBinding),
		exceptions: make(map[string]Exception),
		decisions:  make(map[model.EventID][]Decision),
	}
	policy.seedDefaults()
	return policy
}

func (policy *Policy) seedDefaults() {
	defaults := []struct {
		profile Profile
		min     float64
		max     float64
		age     int64
		kinds   []string
		sources int
	}{
		{ProfileStandard, -60, 6, 5000, []string{"waveform", "integrated-loudness"}, 2},
		{ProfileNews, -30, -1, 4000, []string{"waveform", "integrated-loudness"}, 2},
		{ProfileSports, -40, 3, 3000, []string{"waveform", "integrated-loudness"}, 2},
		{ProfileMusic, -24, 2, 4000, []string{"waveform", "integrated-loudness"}, 2},
	}
	for _, item := range defaults {
		rule := RuleSet{
			ID:                        ruleID(item.profile, 1),
			Profile:                   item.profile,
			Version:                   1,
			Published:                 true,
			Thresholds:                Thresholds{MinimumLoudness: item.min, MaximumLoudness: item.max, MaximumAgeMillis: item.age},
			RequiredKinds:             item.kinds,
			MinimumIndependentSources: item.sources,
		}
		policy.rules[rule.ID] = rule
		policy.versions[item.profile] = 1
		policy.published[item.profile] = []string{rule.ID}
	}
}

func ruleID(profile Profile, version int) string {
	return fmt.Sprintf("rule-%s-v%d", profile, version)
}

// Publish creates a new immutable rule version for a profile. It only affects
// event revisions that are first judged afterwards: already bound revisions
// keep judging under their snapshotted rule.
func (policy *Policy) Publish(profile Profile, thresholds Thresholds, kinds []string, minimumSources int) (RuleSet, error) {
	if profile == "" {
		profile = ProfileStandard
	}
	rule := RuleSet{
		Profile:                   profile,
		Published:                 true,
		Thresholds:                thresholds,
		RequiredKinds:             SortedKinds(kinds),
		MinimumIndependentSources: minimumSources,
	}
	policy.mu.Lock()
	defer policy.mu.Unlock()
	rule.Version = policy.versions[profile] + 1
	rule.ID = ruleID(profile, rule.Version)
	if err := rule.Validate(); err != nil {
		return RuleSet{}, err
	}
	policy.rules[rule.ID] = rule
	policy.versions[profile] = rule.Version
	policy.published[profile] = append(policy.published[profile], rule.ID)
	return rule, nil
}

// Resolve returns the rule an event revision must be judged under, binding
// the revision to the profile's current rule on first use.
func (policy *Policy) Resolve(eventID model.EventID, revision model.RevisionID, profile Profile, now time.Time) RuleSet {
	policy.mu.Lock()
	defer policy.mu.Unlock()
	if byRevision, ok := policy.bindings[eventID]; ok {
		if binding, ok := byRevision[revision]; ok {
			return policy.rules[binding.RuleID]
		}
	}
	rule := policy.currentLocked(profile)
	if policy.bindings[eventID] == nil {
		policy.bindings[eventID] = make(map[model.RevisionID]RuleBinding)
	}
	policy.bindings[eventID][revision] = RuleBinding{EventID: eventID, Revision: revision, RuleID: rule.ID, BoundAt: now.UTC()}
	return rule
}

// BoundRule returns the rule a revision is already pinned to, without
// creating a binding. The second result is false when the revision is unbound.
func (policy *Policy) BoundRule(eventID model.EventID, revision model.RevisionID) (RuleSet, bool) {
	policy.mu.RLock()
	defer policy.mu.RUnlock()
	byRevision, ok := policy.bindings[eventID]
	if !ok {
		return RuleSet{}, false
	}
	binding, ok := byRevision[revision]
	if !ok {
		return RuleSet{}, false
	}
	return policy.rules[binding.RuleID], true
}

// Current returns the newest published rule for a profile without binding it.
func (policy *Policy) Current(profile Profile) RuleSet {
	policy.mu.RLock()
	defer policy.mu.RUnlock()
	return policy.currentLocked(profile)
}

func (policy *Policy) currentLocked(profile Profile) RuleSet {
	if profile == "" {
		profile = ProfileStandard
	}
	ids := policy.published[profile]
	if len(ids) == 0 {
		ids = policy.published[ProfileStandard]
	}
	return policy.rules[ids[len(ids)-1]]
}

func (policy *Policy) GrantException(exception Exception) (Exception, error) {
	if exception.ApprovedAt.IsZero() {
		exception.ApprovedAt = policy.clock().UTC()
	}
	policy.mu.Lock()
	defer policy.mu.Unlock()
	if exception.ID == "" {
		policy.exceptionSeq++
		exception.ID = fmt.Sprintf("exception-%d", policy.exceptionSeq)
	}
	if err := exception.Validate(); err != nil {
		return Exception{}, err
	}
	if _, exists := policy.exceptions[exception.ID]; exists {
		return Exception{}, fmt.Errorf("quality exception %s already exists", exception.ID)
	}
	policy.exceptions[exception.ID] = exception
	return exception, nil
}

// ExceptionsFor returns exception approvals in force for the revision at now.
func (policy *Policy) ExceptionsFor(eventID model.EventID, revision model.RevisionID, now time.Time) []Exception {
	policy.mu.RLock()
	defer policy.mu.RUnlock()
	items := make([]Exception, 0)
	for _, exception := range policy.exceptions {
		if exception.Applies(eventID, revision, now) {
			items = append(items, exception)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

// RecordDecision appends a clearance decision to the immutable ledger.
func (policy *Policy) RecordDecision(decision Decision) {
	policy.mu.Lock()
	defer policy.mu.Unlock()
	policy.decisions[decision.EventID] = append(policy.decisions[decision.EventID], decision)
}

// Replay recomputes historical clearances with the exact rule set, evidence
// and exceptions snapshotted at each decision. Passing an empty event ID
// replays every recorded decision.
func (policy *Policy) Replay(eventID model.EventID) ReplayReport {
	policy.mu.RLock()
	defer policy.mu.RUnlock()
	report := ReplayReport{ReplayedAt: policy.clock().UTC()}
	eventIDs := []model.EventID{eventID}
	if eventID == "" {
		eventIDs = eventIDs[:0]
		for id := range policy.decisions {
			eventIDs = append(eventIDs, id)
		}
		sort.Slice(eventIDs, func(i, j int) bool { return eventIDs[i] < eventIDs[j] })
	}
	for _, id := range eventIDs {
		for _, decision := range policy.decisions[id] {
			recomputed := Evaluate(decision.Rule, decision.EventID, decision.Revision, decision.Evidence, decision.DecidedAt, decision.Exceptions)
			report.Entries = append(report.Entries, ReplayEntry{
				EventID:      decision.EventID,
				Revision:     decision.Revision,
				RuleID:       decision.Rule.ID,
				RuleVersion:  decision.Rule.Version,
				DecidedAt:    decision.DecidedAt,
				Permitted:    decision.Permitted,
				StoredPassed: decision.Result.Passed,
				Recomputed:   recomputed,
				Unchanged:    recomputed.Passed == decision.Result.Passed && violationSummaries(recomputed) == violationSummaries(decision.Result),
			})
		}
	}
	return report
}

func violationSummaries(result Result) string {
	summary := ""
	for _, violation := range result.Violations {
		summary += violation.Code + ":" + violation.Detail + "|"
	}
	return summary
}

// Catalog lists every published rule set across profiles, ordered by profile
// and version.
func (policy *Policy) Catalog() []RuleSet {
	policy.mu.RLock()
	defer policy.mu.RUnlock()
	rules := make([]RuleSet, 0, len(policy.rules))
	for _, rule := range policy.rules {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Profile != rules[j].Profile {
			return rules[i].Profile < rules[j].Profile
		}
		return rules[i].Version < rules[j].Version
	})
	return rules
}

// Exceptions lists every recorded exception approval.
func (policy *Policy) Exceptions() []Exception {
	policy.mu.RLock()
	defer policy.mu.RUnlock()
	items := make([]Exception, 0, len(policy.exceptions))
	for _, exception := range policy.exceptions {
		items = append(items, exception)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ApprovedAt.Before(items[j].ApprovedAt) })
	return items
}

// Bindings lists every revision rule binding.
func (policy *Policy) Bindings() []RuleBinding {
	policy.mu.RLock()
	defer policy.mu.RUnlock()
	items := make([]RuleBinding, 0)
	for _, byRevision := range policy.bindings {
		for _, binding := range byRevision {
			items = append(items, binding)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].EventID != items[j].EventID {
			return items[i].EventID < items[j].EventID
		}
		return items[i].Revision < items[j].Revision
	})
	return items
}

// Decisions returns the clearance ledger for an event, or every event when
// eventID is empty.
func (policy *Policy) Decisions(eventID model.EventID) []Decision {
	policy.mu.RLock()
	defer policy.mu.RUnlock()
	if eventID != "" {
		return append([]Decision(nil), policy.decisions[eventID]...)
	}
	items := make([]Decision, 0)
	for _, decisions := range policy.decisions {
		items = append(items, decisions...)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].DecidedAt.Before(items[j].DecidedAt) })
	return items
}
