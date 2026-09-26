package service

import (
	"testing"
	"time"

	"github.com/wyw14/solo-035/internal/model"
	"github.com/wyw14/solo-035/internal/quality"
)

func newBootstrappedRuntime(t *testing.T, at time.Time) (*Runtime, model.LiveEvent) {
	t.Helper()
	runtime, err := NewRuntime(t.TempDir(), func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(runtime); err != nil {
		t.Fatal(err)
	}
	events := runtime.Events.List()
	if len(events) != 1 {
		t.Fatalf("events = %d", len(events))
	}
	return runtime, events[0]
}

func TestRulePublicationOnlyAffectsLaterRevision(t *testing.T) {
	base := time.Unix(100, 0).UTC()
	runtime, live := newBootstrappedRuntime(t, base)

	// Bootstrap clearance is on record under standard v1.
	report := runtime.Quality.Reevaluate(live.ID)
	if len(report.Entries) != 1 {
		t.Fatalf("decisions = %d", len(report.Entries))
	}
	if report.Entries[0].RuleID != "rule-standard-v1" || !report.Entries[0].Permitted {
		t.Fatalf("bootstrap decision = %+v", report.Entries[0])
	}

	// Tighten the standard profile: three independent sources required.
	if _, err := runtime.QualityPolicy.Publish(
		quality.ProfileStandard,
		quality.Thresholds{MinimumLoudness: -60, MaximumLoudness: 6, MaximumAgeMillis: 5000},
		[]string{"waveform", "integrated-loudness"}, 3,
	); err != nil {
		t.Fatal(err)
	}

	// A new program revision binds the new rule and must fail with two probes.
	newRevision := model.RevisionID("11111111-1111-4111-8111-111111111111")
	live.Revision = newRevision
	if err := runtime.Events.Update(live); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Quality.Permit(live.ID, newRevision, true, true); err == nil {
		t.Fatal("new revision must be denied under the stricter v2 rule")
	}

	// The historical v1 decision remains a pass when recomputed.
	report = runtime.Quality.Reevaluate(live.ID)
	if len(report.Entries) != 2 {
		t.Fatalf("decisions = %d", len(report.Entries))
	}
	if !report.Entries[0].Permitted || !report.Entries[0].Unchanged {
		t.Fatal("historical v1 clearance must not be rewritten by v2")
	}
}

func TestExceptionApprovalAllowsLaterPermit(t *testing.T) {
	base := time.Unix(100, 0).UTC()
	runtime, live := newBootstrappedRuntime(t, base)

	if _, err := runtime.QualityPolicy.Publish(
		quality.ProfileStandard,
		quality.Thresholds{MinimumLoudness: -60, MaximumLoudness: 6, MaximumAgeMillis: 5000},
		[]string{"waveform", "integrated-loudness"}, 3,
	); err != nil {
		t.Fatal(err)
	}
	newRevision := model.RevisionID("22222222-2222-4222-8222-222222222222")
	live.Revision = newRevision
	if err := runtime.Events.Update(live); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.QualityPolicy.GrantException(quality.Exception{
		EventID:    live.ID,
		Revision:   newRevision,
		Waive:      []string{quality.CodeIndependentSources},
		Reason:     "remote single-path venue",
		ApprovedBy: "duty-manager",
	}); err != nil {
		t.Fatal(err)
	}
	ready, err := runtime.Quality.Permit(live.ID, newRevision, true, true)
	if err != nil {
		t.Fatalf("permit under approved exception: %v", err)
	}
	if !ready.Ready {
		t.Fatal("approved exception must clear the interlock")
	}
}

func TestCheckpointPreservesRuleLedger(t *testing.T) {
	base := time.Unix(100, 0).UTC()
	runtime, live := newBootstrappedRuntime(t, base)
	if _, err := runtime.QualityPolicy.Publish(
		quality.ProfileNews,
		quality.Thresholds{MinimumLoudness: -28, MaximumLoudness: -2, MaximumAgeMillis: 3000},
		[]string{"waveform", "integrated-loudness"}, 2,
	); err != nil {
		t.Fatal(err)
	}
	checkpoint := runtime.ExportCheckpoint()

	restored, err := NewRuntime(t.TempDir(), func() time.Time { return base.Add(time.Hour) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.RestoreCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	if restored.QualityPolicy.Current(quality.ProfileNews).Version != 2 {
		t.Fatal("published news v2 rule missing after checkpoint restore")
	}
	report := restored.Quality.Reevaluate(live.ID)
	if len(report.Entries) != 1 || !report.Entries[0].Unchanged || !report.Entries[0].Permitted {
		t.Fatalf("historical clearance lost across checkpoint: %+v", report.Entries)
	}
}

func TestNewsEventBindsNewsRule(t *testing.T) {
	base := time.Unix(100, 0).UTC()
	runtime, err := NewRuntime(t.TempDir(), func() time.Time { return base })
	if err != nil {
		t.Fatal(err)
	}
	coordinator := NewCoordinator(runtime)
	live, err := coordinator.CreateEventForProfile("Late Bulletin", model.ProfileNews)
	if err != nil {
		t.Fatal(err)
	}
	if live.Profile != model.ProfileNews {
		t.Fatalf("profile = %s", live.Profile)
	}
	runtime.QualityPolicy.Resolve(live.ID, live.Revision, live.Profile, base)
	rule, ok := runtime.QualityPolicy.BoundRule(live.ID, live.Revision)
	if !ok || rule.ID != "rule-news-v1" {
		t.Fatalf("news event bound to %+v ok=%v", rule, ok)
	}
}
