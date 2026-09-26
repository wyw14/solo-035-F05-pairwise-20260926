package quality

import (
	"testing"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

func TestRecalculateMatchesHonestRecord(t *testing.T) {
	now := testClock()
	fixture := newEngineFixture(now)
	eventID := model.NewEventID()
	revision := model.NewRevisionID()
	fixture.pin(t, eventID, revision, 1)
	if err := fixture.evidence.Add(eventID, waveEvidence("probe-a", -18, now)); err != nil {
		t.Fatal(err)
	}
	if err := fixture.evidence.Add(eventID, waveEvidence("probe-b", -17, now)); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.Permit(eventID, revision, true, true); err != nil {
		t.Fatal(err)
	}
	record := fixture.ledger.List(eventID)[0]
	result := Recalculate(record, now.Add(24*time.Hour))
	if !result.Matches {
		t.Fatalf("differences = %+v", result.Differences)
	}
	if result.Rule.Version != 1 {
		t.Fatalf("rule = %+v", result.Rule)
	}
	if len(fixture.ledger.List(eventID)) != 1 {
		t.Fatal("recalculation must not append records")
	}
}

func TestRecalculateDetectsTamperedVerdict(t *testing.T) {
	now := testClock()
	fixture := newEngineFixture(now)
	eventID := model.NewEventID()
	revision := model.NewRevisionID()
	fixture.pin(t, eventID, revision, 1)
	if err := fixture.evidence.Add(eventID, waveEvidence("probe-a", -18, now)); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.Permit(eventID, revision, true, true); err == nil {
		t.Fatal("expected denial")
	}
	record := fixture.ledger.List(eventID)[0]
	record.Evaluation.Ready = true
	result := Recalculate(record, now)
	if result.Matches {
		t.Fatal("tampered record must not match")
	}
	found := false
	for _, difference := range result.Differences {
		if difference.Item == "ready" && difference.Stored == "true" && difference.Recalculated == "false" {
			found = true
		}
	}
	if !found {
		t.Fatalf("differences = %+v", result.Differences)
	}
}

func TestRecalculateUsesRecordedTimeForFreshness(t *testing.T) {
	now := testClock()
	fixture := newEngineFixture(now)
	eventID := model.NewEventID()
	revision := model.NewRevisionID()
	fixture.pin(t, eventID, revision, 1)
	if err := fixture.evidence.Add(eventID, waveEvidence("probe-a", -18, now)); err != nil {
		t.Fatal(err)
	}
	if err := fixture.evidence.Add(eventID, waveEvidence("probe-b", -17, now)); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.Permit(eventID, revision, true, true); err != nil {
		t.Fatal(err)
	}
	record := fixture.ledger.List(eventID)[0]
	// Auditing long after the evidence went stale must still reproduce the
	// original verdict, because freshness is judged at RecordedAt.
	result := Recalculate(record, now.Add(365*24*time.Hour))
	if !result.Matches {
		t.Fatalf("differences = %+v", result.Differences)
	}
	if !result.Recalculated.Ready {
		t.Fatal("recalculated verdict should remain granted")
	}
}
