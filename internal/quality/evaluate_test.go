package quality

import (
	"testing"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

func newsRuleSet(t *testing.T) RuleSet {
	t.Helper()
	book := NewRulebook()
	set, err := book.Publish("news", legacyThresholds(), legacyProbes(), testClock())
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func waveEvidence(probeID string, value float64, at time.Time) model.AudioEvidence {
	return model.AudioEvidence{ProbeID: probeID, Kind: "waveform", Value: value, MeasuredAt: at.UTC()}
}

func TestEvaluateAcceptsTwoIndependentSources(t *testing.T) {
	set := newsRuleSet(t)
	now := testClock()
	evidence := []model.AudioEvidence{waveEvidence("probe-a", -18, now), waveEvidence("probe-b", -17, now)}
	evaluation := Evaluate(set, evidence, nil, now, true, true)
	if !evaluation.Ready || !evaluation.AudioReady {
		t.Fatalf("evaluation = %+v", evaluation)
	}
	if evaluation.Probes[0].ActualSources != 2 {
		t.Fatalf("sources = %d", evaluation.Probes[0].ActualSources)
	}
	if evaluation.Reason != "all playout interlocks passed" {
		t.Fatalf("reason = %q", evaluation.Reason)
	}
}

func TestEvaluateRejectsSingleSource(t *testing.T) {
	set := newsRuleSet(t)
	now := testClock()
	evidence := []model.AudioEvidence{waveEvidence("probe-a", -18, now), waveEvidence("probe-a", -17, now)}
	evaluation := Evaluate(set, evidence, nil, now, true, true)
	if evaluation.Ready || evaluation.AudioReady {
		t.Fatalf("evaluation = %+v", evaluation)
	}
	if evaluation.Probes[0].ActualSources != 1 {
		t.Fatalf("sources = %d", evaluation.Probes[0].ActualSources)
	}
	if evaluation.Reason != "independent audio evidence is missing" {
		t.Fatalf("reason = %q", evaluation.Reason)
	}
}

func TestEvaluateCountsKindsIndependently(t *testing.T) {
	set := newsRuleSet(t)
	now := testClock()
	evidence := []model.AudioEvidence{
		waveEvidence("probe-a", -18, now),
		{ProbeID: "probe-a", Kind: "integrated-loudness", Value: -21, MeasuredAt: now, DerivedFrom: "probe-a"},
	}
	evaluation := Evaluate(set, evidence, nil, now, true, true)
	if evaluation.Probes[0].ActualSources != 1 {
		t.Fatalf("waveform sources = %d", evaluation.Probes[0].ActualSources)
	}
}

func TestEvaluateRejectsOutOfRangeAndStaleEvidence(t *testing.T) {
	set := newsRuleSet(t)
	now := testClock()
	evidence := []model.AudioEvidence{
		waveEvidence("probe-a", -18, now),
		waveEvidence("probe-b", 10, now),
		waveEvidence("probe-c", -20, now.Add(-10*time.Second)),
	}
	evaluation := Evaluate(set, evidence, nil, now, true, true)
	if evaluation.Ready {
		t.Fatal("expected not ready")
	}
	if evaluation.Evidence[1].InRange || evaluation.Evidence[1].Accepted {
		t.Fatalf("out-of-range verdict = %+v", evaluation.Evidence[1])
	}
	if evaluation.Evidence[2].Fresh || evaluation.Evidence[2].Accepted {
		t.Fatalf("stale verdict = %+v", evaluation.Evidence[2])
	}
	if evaluation.Probes[0].ActualSources != 1 {
		t.Fatalf("sources = %d", evaluation.Probes[0].ActualSources)
	}
}

func TestEvaluateAppliesExceptions(t *testing.T) {
	set := newsRuleSet(t)
	now := testClock()
	registry := NewExceptionRegistry()
	rule := set.Key()
	eventID := model.NewEventID()
	if _, err := registry.Grant(eventID, rule, ExceptionLoudnessRange, "", "ops-lead", "known tone", now, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Grant(eventID, rule, ExceptionEvidenceAge, "", "ops-lead", "delayed probe", now, time.Minute); err != nil {
		t.Fatal(err)
	}
	exceptions := registry.Active(eventID, rule, now)
	evidence := []model.AudioEvidence{
		waveEvidence("probe-a", -18, now),
		waveEvidence("probe-b", 10, now),
		waveEvidence("probe-c", -20, now.Add(-10*time.Second)),
	}
	evaluation := Evaluate(set, evidence, exceptions, now, true, true)
	if !evaluation.Ready {
		t.Fatalf("evaluation = %+v", evaluation)
	}
	if len(evaluation.Exceptions) != 2 {
		t.Fatalf("used exceptions = %v", evaluation.Exceptions)
	}
	if len(evaluation.Evidence[1].WaivedBy) != 1 || len(evaluation.Evidence[2].WaivedBy) != 1 {
		t.Fatalf("waivers = %+v", evaluation.Evidence)
	}
}

func TestEvaluateProbeSourceExceptionSatisfiesKind(t *testing.T) {
	set := newsRuleSet(t)
	now := testClock()
	registry := NewExceptionRegistry()
	eventID := model.NewEventID()
	if _, err := registry.Grant(eventID, set.Key(), ExceptionProbeSources, "waveform", "director", "one probe lost", now, 0); err != nil {
		t.Fatal(err)
	}
	exceptions := registry.Active(eventID, set.Key(), now)
	evidence := []model.AudioEvidence{waveEvidence("probe-a", -18, now)}
	evaluation := Evaluate(set, evidence, exceptions, now, true, true)
	if !evaluation.Ready {
		t.Fatalf("evaluation = %+v", evaluation)
	}
	if !evaluation.Probes[0].Satisfied || len(evaluation.Probes[0].WaivedBy) != 1 {
		t.Fatalf("probe verdict = %+v", evaluation.Probes[0])
	}
}

func TestEvaluateIgnoresExpiredExceptions(t *testing.T) {
	set := newsRuleSet(t)
	now := testClock()
	registry := NewExceptionRegistry()
	eventID := model.NewEventID()
	if _, err := registry.Grant(eventID, set.Key(), ExceptionLoudnessRange, "", "ops-lead", "brief", now, time.Minute); err != nil {
		t.Fatal(err)
	}
	later := now.Add(2 * time.Minute)
	exceptions := registry.Active(eventID, set.Key(), later)
	if len(exceptions) != 0 {
		t.Fatalf("active exceptions = %d", len(exceptions))
	}
	evidence := []model.AudioEvidence{waveEvidence("probe-a", -18, later), waveEvidence("probe-b", 10, later)}
	evaluation := Evaluate(set, evidence, exceptions, later, true, true)
	if evaluation.Ready {
		t.Fatal("expired exception must not waive")
	}
}

func TestEvaluateGatesOnVideoAndCaption(t *testing.T) {
	set := newsRuleSet(t)
	now := testClock()
	evidence := []model.AudioEvidence{waveEvidence("probe-a", -18, now), waveEvidence("probe-b", -17, now)}
	evaluation := Evaluate(set, evidence, nil, now, false, true)
	if evaluation.Ready || evaluation.Reason != "video is not locked" {
		t.Fatalf("evaluation = %+v", evaluation)
	}
	evaluation = Evaluate(set, evidence, nil, now, true, false)
	if evaluation.Ready || evaluation.Reason != "caption chain is not ready" {
		t.Fatalf("evaluation = %+v", evaluation)
	}
}
