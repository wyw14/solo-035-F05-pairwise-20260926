package quality

import (
	"testing"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

type engineFixture struct {
	evidence   *EvidenceSet
	book       *Rulebook
	selections *SelectionLog
	exceptions *ExceptionRegistry
	ledger     *PermitLedger
	engine     *PermitEngine
}

func newEngineFixture(now time.Time) *engineFixture {
	clock := func() time.Time { return now }
	evidence := NewEvidenceSet()
	book := NewRulebook()
	if _, err := book.Publish("news", legacyThresholds(), legacyProbes(), now); err != nil {
		panic(err)
	}
	selections := NewSelectionLog()
	exceptions := NewExceptionRegistry()
	ledger := NewPermitLedger()
	return &engineFixture{
		evidence:   evidence,
		book:       book,
		selections: selections,
		exceptions: exceptions,
		ledger:     ledger,
		engine:     NewPermitEngine(evidence, book, selections, exceptions, ledger, clock),
	}
}

func (fixture *engineFixture) pin(t *testing.T, eventID model.EventID, revision model.RevisionID, version int) {
	t.Helper()
	if err := fixture.selections.Pin(eventID, revision, RuleVersion{EventType: "news", Version: version}, testClock()); err != nil {
		t.Fatal(err)
	}
}

func TestPermitRecordsGrantedAndDenied(t *testing.T) {
	now := testClock()
	fixture := newEngineFixture(now)
	eventID := model.NewEventID()
	revision := model.NewRevisionID()
	fixture.pin(t, eventID, revision, 1)
	if err := fixture.evidence.Add(eventID, waveEvidence("probe-a", -18, now)); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.Permit(eventID, revision, true, true); err == nil {
		t.Fatal("expected a denial with one source")
	}
	if err := fixture.evidence.Add(eventID, waveEvidence("probe-b", -17, now)); err != nil {
		t.Fatal(err)
	}
	evaluation, err := fixture.engine.Permit(eventID, revision, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !evaluation.Ready {
		t.Fatalf("evaluation = %+v", evaluation)
	}
	records := fixture.ledger.List(eventID)
	if len(records) != 2 {
		t.Fatalf("records = %d", len(records))
	}
	if records[0].Granted || !records[1].Granted {
		t.Fatalf("granted flags = %v, %v", records[0].Granted, records[1].Granted)
	}
	if records[1].Rule.Version != 1 || records[1].RuleSet.Version != 1 {
		t.Fatalf("rule coordinates = %+v", records[1].Rule)
	}
	if len(records[1].Evidence) != 2 {
		t.Fatalf("snapshotted evidence = %d", len(records[1].Evidence))
	}
}

func TestPermitRequiresPinnedRevision(t *testing.T) {
	fixture := newEngineFixture(testClock())
	if _, err := fixture.engine.Permit(model.NewEventID(), model.NewRevisionID(), true, true); err == nil {
		t.Fatal("expected an error for an unpinned revision")
	}
}

func TestPermitSnapshotSurvivesEvidenceReset(t *testing.T) {
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
	fixture.evidence.Clear(eventID)
	records := fixture.ledger.List(eventID)
	if len(records) != 1 {
		t.Fatalf("records = %d", len(records))
	}
	result := Recalculate(records[0], now.Add(time.Hour))
	if !result.Matches {
		t.Fatalf("differences = %+v", result.Differences)
	}
}
