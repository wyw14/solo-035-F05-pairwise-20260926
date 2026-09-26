package quality

import (
	"testing"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

func TestExceptionGrantValidation(t *testing.T) {
	registry := NewExceptionRegistry()
	rule := RuleVersion{EventType: "news", Version: 1}
	eventID := model.NewEventID()
	if _, err := registry.Grant(eventID, rule, "nonsense", "", "ops", "reason", testClock(), 0); err == nil {
		t.Fatal("unknown item should fail")
	}
	if _, err := registry.Grant(eventID, rule, ExceptionProbeSources, "", "ops", "reason", testClock(), 0); err == nil {
		t.Fatal("probe-sources without kind should fail")
	}
	if _, err := registry.Grant(eventID, rule, ExceptionLoudnessRange, "waveform", "ops", "reason", testClock(), 0); err == nil {
		t.Fatal("loudness-range with kind should fail")
	}
	if _, err := registry.Grant(eventID, rule, ExceptionLoudnessRange, "", "", "reason", testClock(), 0); err == nil {
		t.Fatal("missing approver should fail")
	}
	if _, err := registry.Grant(eventID, rule, ExceptionLoudnessRange, "", "ops", "", testClock(), 0); err == nil {
		t.Fatal("missing reason should fail")
	}
}

func TestExceptionActiveFiltersByEventRuleAndExpiry(t *testing.T) {
	registry := NewExceptionRegistry()
	now := testClock()
	ruleV1 := RuleVersion{EventType: "news", Version: 1}
	ruleV2 := RuleVersion{EventType: "news", Version: 2}
	eventID := model.NewEventID()
	otherID := model.NewEventID()
	if _, err := registry.Grant(eventID, ruleV1, ExceptionLoudnessRange, "", "ops", "pinned to v1", now, 0); err != nil {
		t.Fatal(err)
	}
	expiring, err := registry.Grant(eventID, ruleV2, ExceptionEvidenceAge, "", "ops", "short lived", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Grant(otherID, ruleV1, ExceptionLoudnessRange, "", "ops", "other event", now, 0); err != nil {
		t.Fatal(err)
	}
	if expiring.ExpiresAt == nil {
		t.Fatal("ttl was not applied")
	}
	active := registry.Active(eventID, ruleV1, now)
	if len(active) != 1 {
		t.Fatalf("active for v1 = %d", len(active))
	}
	active = registry.Active(eventID, ruleV2, now)
	if len(active) != 1 {
		t.Fatalf("active for v2 = %d", len(active))
	}
	if got := registry.Active(eventID, ruleV2, now.Add(2*time.Minute)); len(got) != 0 {
		t.Fatalf("expired exceptions = %d", len(got))
	}
	if got := registry.Active(eventID, ruleV2, now.Add(time.Minute)); len(got) != 0 {
		t.Fatal("expiry boundary must be exclusive")
	}
	if len(registry.List(eventID)) != 2 {
		t.Fatalf("event exceptions = %d", len(registry.List(eventID)))
	}
	if len(registry.Export()) != 3 {
		t.Fatalf("all exceptions = %d", len(registry.Export()))
	}
}

func TestExceptionRestoreRejectsDuplicates(t *testing.T) {
	registry := NewExceptionRegistry()
	eventID := model.NewEventID()
	exception := Exception{ID: "dup", EventID: eventID, Rule: RuleVersion{EventType: "news", Version: 1}, Item: ExceptionLoudnessRange, ApprovedBy: "ops", Reason: "r", GrantedAt: testClock()}
	if err := registry.Restore([]Exception{exception, exception}); err == nil {
		t.Fatal("duplicates should fail")
	}
	if err := registry.Restore([]Exception{exception}); err != nil {
		t.Fatal(err)
	}
	if len(registry.List(eventID)) != 1 {
		t.Fatalf("restored = %d", len(registry.List(eventID)))
	}
}
