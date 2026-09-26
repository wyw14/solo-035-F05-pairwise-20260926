package service

import (
	"context"
	"testing"
	"time"

	"github.com/wyw14/solo-035/internal/event"
	"github.com/wyw14/solo-035/internal/model"
	"github.com/wyw14/solo-035/internal/quality"
)

func fixedRuntime(t *testing.T, now time.Time) *Runtime {
	t.Helper()
	runtime, err := NewRuntime(t.TempDir(), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func TestBootstrapPermitIsPinnedToNewsV1(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	runtime := fixedRuntime(t, now)
	if err := Bootstrap(runtime); err != nil {
		t.Fatal(err)
	}
	event := runtime.Events.List()[0]
	if event.Type != "news" {
		t.Fatalf("event type = %q", event.Type)
	}
	records := runtime.Permits.List(event.ID)
	if len(records) != 1 || !records[0].Granted {
		t.Fatalf("permit records = %+v", records)
	}
	if records[0].Rule.Version != 1 || records[0].Rule.EventType != "news" {
		t.Fatalf("pinned rule = %+v", records[0].Rule)
	}
	if len(records[0].RuleSet.Probes) != 1 || records[0].RuleSet.Thresholds.MaximumAgeMillis != 5000 {
		t.Fatalf("snapshot ruleset = %+v", records[0].RuleSet)
	}
	if len(records[0].Evidence) != 4 {
		t.Fatalf("snapshotted evidence = %d, want wave+loudness from two probes", len(records[0].Evidence))
	}
}

func TestRuleRevisionDoesNotChangeHistoricalPermit(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	runtime := fixedRuntime(t, now)
	if err := Bootstrap(runtime); err != nil {
		t.Fatal(err)
	}
	event := runtime.Events.List()[0]
	historical := runtime.Permits.List(event.ID)[0]

	// Publish a stricter v2 that the recorded evidence (-18/-17) could not pass.
	_, err := runtime.ExecuteControl(context.Background(), ControlRequest{
		Action: "publish-quality-rules", EventType: "news",
		Minimum: -60, Maximum: -19, MaxAgeMillis: 5000,
		Probes: []quality.ProbeRequirement{{Kind: "waveform", MinimumSources: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	current, _ := runtime.Rulebook.Current("news")
	if current.Version != 2 {
		t.Fatalf("current version = %d", current.Version)
	}

	result, err := runtime.ExecuteControl(context.Background(), ControlRequest{
		Action: "recalculate-permit", PermitID: historical.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	audit, ok := result.Value.(quality.AuditResult)
	if !ok {
		t.Fatalf("audit result = %T", result.Value)
	}
	if !audit.Matches || audit.Rule.Version != 1 {
		t.Fatalf("audit = %+v", audit)
	}
	if len(runtime.Permits.List(event.ID)) != 1 {
		t.Fatal("recalculation must not append ledger records")
	}
}

func TestAdvanceProgramRepinsCurrentRules(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	runtime := fixedRuntime(t, now)
	coordinator := NewCoordinator(runtime)
	event, err := coordinator.CreateEvent("Live Sports", "sports")
	if err != nil {
		t.Fatal(err)
	}
	firstRevision := event.Revision
	if selection, ok := runtime.Selections.Latest(event.ID); !ok || selection.Rule.Version != 1 {
		t.Fatalf("initial selection = %+v, %v", selection, ok)
	}
	if _, err := runtime.Rulebook.Publish("sports",
		quality.Thresholds{MinimumLoudness: -60, MaximumLoudness: 0, MaximumAgeMillis: 3000},
		[]quality.ProbeRequirement{{Kind: "waveform", MinimumSources: 2}}, now); err != nil {
		t.Fatal(err)
	}
	updated, err := coordinator.AdvanceProgram(event.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision == firstRevision {
		t.Fatal("revision was not advanced")
	}
	latest, ok := runtime.Selections.Latest(event.ID)
	if !ok || latest.Rule.Version != 2 || latest.Revision != updated.Revision {
		t.Fatalf("latest selection = %+v, %v", latest, ok)
	}
	old, ok := runtime.Selections.Current(event.ID, firstRevision)
	if !ok || old.Rule.Version != 1 {
		t.Fatalf("old revision selection = %+v, %v", old, ok)
	}
}

func TestExceptionAllowsPermitAndExpiryRestoresDenial(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	clock := func() time.Time { return now }
	runtime, err := NewRuntime(t.TempDir(), clock)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := NewCoordinator(runtime)
	event, err := coordinator.CreateEvent("Breaking News", "news")
	if err != nil {
		t.Fatal(err)
	}
	// One source where two are required: the permit must be denied first.
	if err := runtime.Quality.Record(event.ID, model.AudioEvidence{ProbeID: "probe-a", Kind: "waveform", Value: -18, MeasuredAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.PermitFlow.Permit(event.ID, event.Revision, true, true); err == nil {
		t.Fatal("expected denial")
	}
	_, err = runtime.ExecuteControl(context.Background(), ControlRequest{
		Action: "grant-quality-exception", EventID: string(event.ID),
		Item: "probe-sources", Kind: "waveform", Approver: "duty-manager",
		Message: "second probe failed before air", ExpiresInMillis: 60000,
	})
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := runtime.PermitFlow.Permit(event.ID, event.Revision, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !evaluation.Ready || len(evaluation.Exceptions) != 1 {
		t.Fatalf("evaluation = %+v", evaluation)
	}

	// Advance the clock past the expiry: the exception is a fact with a TTL,
	// so a later permit is denied again. Fresh evidence keeps the denial
	// attributable to the expired exception, not to stale measurements.
	now = now.Add(2 * time.Minute)
	if err := runtime.Quality.Record(event.ID, model.AudioEvidence{ProbeID: "probe-a", Kind: "waveform", Value: -18, MeasuredAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.PermitFlow.Permit(event.ID, event.Revision, true, true); err == nil {
		t.Fatal("expired exception must not waive")
	}
}

func TestCheckpointRoundTripPreservesPermitAudit(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	runtime := fixedRuntime(t, now)
	if err := Bootstrap(runtime); err != nil {
		t.Fatal(err)
	}
	event := runtime.Events.List()[0]
	historical := runtime.Permits.List(event.ID)[0]
	checkpoint := runtime.ExportCheckpoint()
	if len(checkpoint.Rules) != 3 || len(checkpoint.Selections) != 1 || len(checkpoint.Permits) != 1 {
		t.Fatalf("checkpoint quality sections = %+v", checkpoint)
	}

	restored := fixedRuntime(t, now)
	if _, err := restored.RestoreCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	record, ok := restored.Permits.Get(historical.ID)
	if !ok {
		t.Fatal("permit was not restored")
	}
	audit := quality.Recalculate(record, now.Add(time.Hour))
	if !audit.Matches {
		t.Fatalf("differences = %+v", audit.Differences)
	}
	// The restored runtime must keep every event revision pinned. (Caption
	// continuity is a pre-existing checkpoint gap outside this feature.)
	for _, check := range restored.Diagnostics().Checks {
		if check.Name == "quality-rules" && !check.Passed {
			t.Fatalf("quality-rules diagnostic = %+v", check)
		}
	}
}

func TestLegacyCheckpointBackfillsRulesAndPins(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	oldEvent := model.LiveEvent{
		ID:               model.NewEventID(),
		Name:             "Legacy Broadcast",
		Phase:            model.PhasePreparing,
		Revision:         model.NewRevisionID(),
		CommandEpoch:     1,
		SourceGeneration: 1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	checkpoint := ControlCheckpoint{
		Events: []event.RecoveryRecord{{Event: oldEvent, History: []model.EventPhase{model.PhasePreparing}}},
	}
	runtime := fixedRuntime(t, now)
	if _, err := runtime.RestoreCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	restored, ok := runtime.Events.Get(oldEvent.ID)
	if !ok || restored.Type != "news" {
		t.Fatalf("restored event = %+v, %v", restored, ok)
	}
	selection, ok := runtime.Selections.Current(oldEvent.ID, oldEvent.Revision)
	if !ok || selection.Rule.EventType != "news" || selection.Rule.Version != 1 {
		t.Fatalf("backfilled selection = %+v, %v", selection, ok)
	}
	report := runtime.Diagnostics()
	for _, check := range report.Checks {
		if check.Name == "quality-rules" && !check.Passed {
			t.Fatalf("quality-rules diagnostic = %+v", check)
		}
	}
}
