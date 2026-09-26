package quality

import (
	"testing"

	"github.com/wyw14/solo-035/internal/model"
)

func TestSelectionPinAndQuery(t *testing.T) {
	log := NewSelectionLog()
	eventID := model.NewEventID()
	revisionA := model.NewRevisionID()
	revisionB := model.NewRevisionID()
	now := testClock()
	if err := log.Pin(eventID, revisionA, RuleVersion{EventType: "news", Version: 1}, now); err != nil {
		t.Fatal(err)
	}
	if err := log.Pin(eventID, revisionB, RuleVersion{EventType: "news", Version: 2}, now); err != nil {
		t.Fatal(err)
	}
	if err := log.Pin(eventID, revisionA, RuleVersion{EventType: "news", Version: 3}, now); err == nil {
		t.Fatal("re-pinning the same revision must fail")
	}
	current, ok := log.Current(eventID, revisionA)
	if !ok || current.Rule.Version != 1 {
		t.Fatalf("old revision pin = %+v, %v", current, ok)
	}
	latest, ok := log.Latest(eventID)
	if !ok || latest.Rule.Version != 2 || latest.Revision != revisionB {
		t.Fatalf("latest pin = %+v, %v", latest, ok)
	}
}

func TestSelectionRestoreRejectsDuplicateKeys(t *testing.T) {
	log := NewSelectionLog()
	eventID := model.NewEventID()
	revision := model.NewRevisionID()
	selection := RuleSelection{EventID: eventID, Revision: revision, Rule: RuleVersion{EventType: "news", Version: 1}, PinnedAt: testClock()}
	if err := log.Restore([]RuleSelection{selection, selection}); err == nil {
		t.Fatal("duplicate pins should fail restore")
	}
	if err := log.Restore([]RuleSelection{selection}); err != nil {
		t.Fatal(err)
	}
	if _, ok := log.Current(eventID, revision); !ok {
		t.Fatal("restored pin is missing")
	}
}
