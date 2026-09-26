package quality

import (
	"testing"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

func fixedClock(base time.Time) (func() time.Time, func(time.Duration)) {
	now := base
	return func() time.Time { return now }, func(advance time.Duration) { now = now.Add(advance) }
}

func evidence(probe string, kinds []string, value float64, at time.Time) []model.AudioEvidence {
	items := make([]model.AudioEvidence, 0, len(kinds))
	for _, kind := range kinds {
		item := model.AudioEvidence{ProbeID: probe, Kind: kind, Value: value, MeasuredAt: at}
		if kind == "integrated-loudness" {
			item.DerivedFrom = probe
			item.Value = value - 3
		}
		items = append(items, item)
	}
	return items
}

func standardEvidence(t time.Time, probes ...string) []model.AudioEvidence {
	kinds := []string{"waveform", "integrated-loudness"}
	items := make([]model.AudioEvidence, 0, len(probes)*len(kinds))
	for _, probe := range probes {
		items = append(items, evidence(probe, kinds, -18, t)...)
	}
	return items
}

func TestRevisionBindingKeepsRuleAfterNewPublication(t *testing.T) {
	base := time.Unix(1000, 0).UTC()
	clock, advance := fixedClock(base)
	policy := NewPolicy(clock)
	set := NewEvidenceSet()
	coordinator := NewCoordinator(set, policy, func(model.EventID) Profile { return ProfileStandard }, clock)

	eventID := model.NewEventID()
	revision1 := model.NewRevisionID()
	revision2 := model.NewRevisionID()

	for _, item := range standardEvidence(base, "probe-a", "probe-b") {
		if err := set.Add(eventID, item); err != nil {
			t.Fatal(err)
		}
	}

	// First clearance binds revision1 to standard v1 and passes.
	if _, err := coordinator.Permit(eventID, revision1, true, true); err != nil {
		t.Fatalf("permit revision1: %v", err)
	}
	bound, ok := policy.BoundRule(eventID, revision1)
	if !ok || bound.Version != 1 {
		t.Fatalf("revision1 bound = %+v ok=%v", bound, ok)
	}

	// A stricter v2 demanding three sources only affects later bindings.
	rule2, err := policy.Publish(ProfileStandard, Thresholds{-60, 6, 5000},
		[]string{"waveform", "integrated-loudness"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if rule2.Version != 2 {
		t.Fatalf("published version = %d", rule2.Version)
	}
	if _, err := coordinator.Permit(eventID, revision2, true, true); err == nil {
		t.Fatal("permit revision2 under v2 must fail with only two sources")
	}

	// The historical clearance still recomputes under the v1 snapshot.
	report := coordinator.Reevaluate(eventID)
	if len(report.Entries) != 2 {
		t.Fatalf("replay entries = %d", len(report.Entries))
	}
	first := report.Entries[0]
	if first.RuleVersion != 1 || !first.Permitted || !first.Unchanged {
		t.Fatalf("historical decision altered: %+v", first)
	}
	second := report.Entries[1]
	if second.RuleVersion != 2 || second.Permitted || !second.Unchanged {
		t.Fatalf("revision2 replay mismatch: %+v", second)
	}

	// Time travel does not make the first decision re-evaluate as stale:
	// freshness is judged at the original decision instant.
	advance(10 * time.Minute)
	report = coordinator.Reevaluate(eventID)
	if !report.Entries[0].Unchanged {
		t.Fatal("historical clearance changed after clock advanced")
	}
}

func TestProfilesSelectDifferentRules(t *testing.T) {
	base := time.Unix(1000, 0).UTC()
	policy := NewPolicy(func() time.Time { return base })

	news := policy.Current(ProfileNews)
	music := policy.Current(ProfileMusic)
	if news.Thresholds.MinimumLoudness == music.Thresholds.MinimumLoudness {
		t.Fatal("news and music share loudness thresholds")
	}

	eventID := model.NewEventID()
	revision := model.NewRevisionID()
	// Readings acceptable for the standard -60..6 range but below the news
	// loudness floor of -30.
	quiet := standardEvidence(base, "probe-a", "probe-b")
	for i := range quiet {
		quiet[i].Value -= 25
	}
	rule := policy.Resolve(eventID, revision, ProfileNews, base)
	if rule.ID != "rule-news-v1" {
		t.Fatalf("resolved rule = %s", rule.ID)
	}
	result := Evaluate(rule, eventID, revision, quiet, base, nil)
	if result.Passed {
		t.Fatal("news profile must reject standard-range quiet evidence")
	}
	foundLoudness := false
	for _, violation := range result.Violations {
		if violation.Code == CodeLoudness {
			foundLoudness = true
		}
	}
	if !foundLoudness {
		t.Fatalf("expected loudness violation, got %+v", result.Violations)
	}
}

func TestExceptionWaivesViolation(t *testing.T) {
	base := time.Unix(1000, 0).UTC()
	clock := func() time.Time { return base }
	policy := NewPolicy(clock)
	set := NewEvidenceSet()
	coordinator := NewCoordinator(set, policy, func(model.EventID) Profile { return ProfileStandard }, clock)

	eventID := model.NewEventID()
	revision := model.NewRevisionID()
	// Only one complete source: fails independent-sources.
	for _, item := range standardEvidence(base, "probe-a") {
		if err := set.Add(eventID, item); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := coordinator.Permit(eventID, revision, true, true); err == nil {
		t.Fatal("permit without exception must fail")
	}
	exception, err := policy.GrantException(Exception{
		EventID:    eventID,
		Revision:   revision,
		Waive:      []string{CodeIndependentSources},
		Reason:     "single feed venue",
		ApprovedBy: "ops-lead",
	})
	if err != nil {
		t.Fatal(err)
	}
	ready, err := coordinator.Permit(eventID, revision, true, true)
	if err != nil {
		t.Fatalf("permit with exception: %v", err)
	}
	if !ready.AudioReady {
		t.Fatal("exception should waive the source count")
	}
	entry := policy.Replay(eventID).Entries[1]
	if !entry.Unchanged || len(entry.Recomputed.AppliedExceptions) != 1 {
		t.Fatalf("exception not preserved on replay: %+v", entry)
	}
	if entry.Recomputed.AppliedExceptions[0].ID != exception.ID {
		t.Fatal("applied exception identity lost")
	}
}

func TestAdmitRejectsUnknownKindAndStaleEvidence(t *testing.T) {
	base := time.Unix(1000, 0).UTC()
	rule := RuleSet{
		ID:                        "rule-standard-v1",
		Profile:                   ProfileStandard,
		Version:                   1,
		Thresholds:                Thresholds{-60, 6, 1000},
		RequiredKinds:             []string{"waveform"},
		MinimumIndependentSources: 1,
	}
	eventID := model.NewEventID()
	revision := model.NewRevisionID()

	if err := Admit(rule, eventID, revision, model.AudioEvidence{ProbeID: "p", Kind: "spectrogram", Value: -10, MeasuredAt: base}, base, nil); err == nil {
		t.Fatal("unknown measurement kind must be rejected")
	}
	if err := Admit(rule, eventID, revision, model.AudioEvidence{ProbeID: "p", Kind: "waveform", Value: -10, MeasuredAt: base.Add(-2 * time.Second)}, base, nil); err == nil {
		t.Fatal("stale evidence must be rejected")
	}
	if err := Admit(rule, eventID, revision, model.AudioEvidence{ProbeID: "p", Kind: "waveform", Value: -10, MeasuredAt: base}, base, nil); err != nil {
		t.Fatalf("fresh in-range evidence rejected: %v", err)
	}
}

func TestExceptionRespectsRevisionAndExpiry(t *testing.T) {
	base := time.Unix(1000, 0).UTC()
	eventID := model.NewEventID()
	revision := model.NewRevisionID()
	expiry := base.Add(time.Hour)
	exception := Exception{
		ID:         "exc-1",
		EventID:    eventID,
		Revision:   revision,
		Waive:      []string{CodeLoudness},
		ApprovedAt: base,
		ExpiresAt:  &expiry,
	}
	if !exception.Applies(eventID, revision, base.Add(30*time.Minute)) {
		t.Fatal("exception must apply within its window and revision")
	}
	if exception.Applies(eventID, model.NewRevisionID(), base) {
		t.Fatal("exception must not cross revisions")
	}
	if exception.Applies(eventID, revision, base.Add(2*time.Hour)) {
		t.Fatal("expired exception must not apply")
	}
	if exception.Waives(CodeFreshness) {
		t.Fatal("scoped exception must not waive other codes")
	}
}

func TestPolicyRestoreRoundTrip(t *testing.T) {
	base := time.Unix(1000, 0).UTC()
	clock := func() time.Time { return base }
	policy := NewPolicy(clock)
	set := NewEvidenceSet()
	coordinator := NewCoordinator(set, policy, func(model.EventID) Profile { return ProfileSports }, clock)

	eventID := model.NewEventID()
	revision := model.NewRevisionID()
	for _, item := range standardEvidence(base, "probe-a", "probe-b") {
		if err := set.Add(eventID, item); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := coordinator.Permit(eventID, revision, true, true); err != nil {
		t.Fatal(err)
	}
	if _, err := policy.Publish(ProfileSports, Thresholds{-50, 0, 2000}, []string{"waveform"}, 1); err != nil {
		t.Fatal(err)
	}

	record := policy.Export(nil)
	restored := NewPolicy(clock)
	if err := restored.Restore(record); err != nil {
		t.Fatal(err)
	}
	bound, ok := restored.BoundRule(eventID, revision)
	if !ok || bound.ID != "rule-sports-v1" {
		t.Fatalf("binding lost after restore: %+v %v", bound, ok)
	}
	if restored.Current(ProfileSports).Version != 2 {
		t.Fatal("published v2 rule missing after restore")
	}
	report := restored.Replay(eventID)
	if len(report.Entries) != 1 || !report.Entries[0].Unchanged {
		t.Fatalf("decision ledger not restored: %+v", report.Entries)
	}
}
