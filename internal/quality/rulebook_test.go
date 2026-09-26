package quality

import (
	"testing"
	"time"
)

var testClock = func() time.Time { return time.Unix(100, 0).UTC() }

func legacyThresholds() Thresholds {
	return Thresholds{MinimumLoudness: -60, MaximumLoudness: 6, MaximumAgeMillis: 5000}
}

func legacyProbes() []ProbeRequirement {
	return []ProbeRequirement{{Kind: "waveform", MinimumSources: 2}}
}

func TestRulebookPublishesMonotonicVersions(t *testing.T) {
	book := NewRulebook()
	first, err := book.Publish("news", legacyThresholds(), legacyProbes(), testClock())
	if err != nil {
		t.Fatal(err)
	}
	second, err := book.Publish("news", legacyThresholds(), legacyProbes(), testClock())
	if err != nil {
		t.Fatal(err)
	}
	other, err := book.Publish("sports", legacyThresholds(), legacyProbes(), testClock())
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || second.Version != 2 {
		t.Fatalf("news versions = %d, %d", first.Version, second.Version)
	}
	if other.Version != 1 {
		t.Fatalf("sports version = %d", other.Version)
	}
	current, ok := book.Current("news")
	if !ok || current.Version != 2 || current.Status != RuleSetPublished {
		t.Fatalf("current = %+v, %v", current, ok)
	}
	archived, ok := book.Version(RuleVersion{EventType: "news", Version: 1})
	if !ok {
		t.Fatal("version 1 was not retained")
	}
	if archived.Status != RuleSetDeprecated {
		t.Fatalf("archived status = %s", archived.Status)
	}
	if archived.Thresholds != first.Thresholds {
		t.Fatal("archived version was rewritten")
	}
}

func TestRulebookRejectsInvalidRules(t *testing.T) {
	book := NewRulebook()
	cases := []struct {
		name       string
		eventType  string
		thresholds Thresholds
		probes     []ProbeRequirement
	}{
		{"empty type", "", legacyThresholds(), legacyProbes()},
		{"inverted loudness", "news", Thresholds{MinimumLoudness: 6, MaximumLoudness: -60, MaximumAgeMillis: 5000}, legacyProbes()},
		{"non-positive age", "news", Thresholds{MinimumLoudness: -60, MaximumLoudness: 6, MaximumAgeMillis: 0}, legacyProbes()},
		{"no probes", "news", legacyThresholds(), nil},
		{"empty kind", "news", legacyThresholds(), []ProbeRequirement{{Kind: "", MinimumSources: 1}}},
		{"zero sources", "news", legacyThresholds(), []ProbeRequirement{{Kind: "waveform", MinimumSources: 0}}},
		{"duplicate kind", "news", legacyThresholds(), []ProbeRequirement{{Kind: "waveform", MinimumSources: 1}, {Kind: "waveform", MinimumSources: 1}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := book.Publish(tc.eventType, tc.thresholds, tc.probes, testClock()); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestRulebookRestoreRoundTrip(t *testing.T) {
	book := NewRulebook()
	if _, err := book.Publish("news", legacyThresholds(), legacyProbes(), testClock()); err != nil {
		t.Fatal(err)
	}
	if _, err := book.Publish("news", Thresholds{MinimumLoudness: -48, MaximumLoudness: 3, MaximumAgeMillis: 3000}, legacyProbes(), testClock()); err != nil {
		t.Fatal(err)
	}
	exported := book.Export()
	restored := NewRulebook()
	if err := restored.Restore(exported); err != nil {
		t.Fatal(err)
	}
	current, ok := restored.Current("news")
	if !ok || current.Version != 2 || current.Thresholds.MaximumLoudness != 3 {
		t.Fatalf("restored current = %+v, %v", current, ok)
	}
	if len(restored.List("news")) != 2 {
		t.Fatalf("restored versions = %d", len(restored.List("news")))
	}
}

func TestRulebookRestoreRejectsDiscontinuousHistory(t *testing.T) {
	book := NewRulebook()
	broken := []RuleSet{
		{EventType: "news", Version: 1, Thresholds: legacyThresholds(), Probes: legacyProbes(), Status: RuleSetDeprecated, PublishedAt: testClock()},
		{EventType: "news", Version: 3, Thresholds: legacyThresholds(), Probes: legacyProbes(), Status: RuleSetPublished, PublishedAt: testClock()},
	}
	if err := book.Restore(broken); err == nil {
		t.Fatal("expected an error")
	}
}
