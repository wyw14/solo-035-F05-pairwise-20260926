package quality

import (
	"fmt"
	"sync"
	"time"
)

// Rulebook keeps every published rule set per event type. Rule sets are
// append-only: a revision publishes a new version and deprecates the
// previous one, but never rewrites history.
type Rulebook struct {
	mu     sync.RWMutex
	byType map[string][]RuleSet
}

func NewRulebook() *Rulebook {
	return &Rulebook{byType: make(map[string][]RuleSet)}
}

func (book *Rulebook) Publish(eventType string, thresholds Thresholds, probes []ProbeRequirement, now time.Time) (RuleSet, error) {
	book.mu.Lock()
	defer book.mu.Unlock()
	versions := book.byType[eventType]
	set := RuleSet{
		EventType:   eventType,
		Version:     len(versions) + 1,
		Thresholds:  thresholds,
		Probes:      cloneProbes(probes),
		Status:      RuleSetPublished,
		PublishedAt: now.UTC(),
	}
	if err := set.Validate(); err != nil {
		return RuleSet{}, err
	}
	if len(versions) > 0 {
		versions[len(versions)-1].Status = RuleSetDeprecated
	}
	book.byType[eventType] = append(versions, set)
	return set.clone(), nil
}

func (book *Rulebook) Current(eventType string) (RuleSet, bool) {
	book.mu.RLock()
	defer book.mu.RUnlock()
	versions := book.byType[eventType]
	if len(versions) == 0 {
		return RuleSet{}, false
	}
	return versions[len(versions)-1].clone(), true
}

func (book *Rulebook) Version(ref RuleVersion) (RuleSet, bool) {
	book.mu.RLock()
	defer book.mu.RUnlock()
	versions := book.byType[ref.EventType]
	if ref.Version < 1 || ref.Version > len(versions) {
		return RuleSet{}, false
	}
	return versions[ref.Version-1].clone(), true
}

func (book *Rulebook) List(eventType string) []RuleSet {
	book.mu.RLock()
	defer book.mu.RUnlock()
	return cloneSets(book.byType[eventType])
}

func (book *Rulebook) ListAll() []RuleSet {
	book.mu.RLock()
	defer book.mu.RUnlock()
	all := make([]RuleSet, 0, len(book.byType))
	for _, versions := range book.byType {
		all = append(all, cloneSets(versions)...)
	}
	return all
}

func (book *Rulebook) Export() []RuleSet {
	return book.ListAll()
}

func (book *Rulebook) Restore(sets []RuleSet) error {
	byType := make(map[string][]RuleSet)
	for _, set := range sets {
		byType[set.EventType] = append(byType[set.EventType], set.clone())
	}
	for eventType, versions := range byType {
		for index, set := range versions {
			if set.Version != index+1 {
				return fmt.Errorf("rule history for %s is not continuous", eventType)
			}
			if err := set.Validate(); err != nil {
				return fmt.Errorf("restore %s v%d: %w", eventType, set.Version, err)
			}
		}
		for index := 0; index < len(versions)-1; index++ {
			if versions[index].Status != RuleSetDeprecated {
				return fmt.Errorf("rule history for %s has a live non-current version", eventType)
			}
		}
		if versions[len(versions)-1].Status != RuleSetPublished {
			return fmt.Errorf("rule history for %s has no published current version", eventType)
		}
	}
	book.mu.Lock()
	defer book.mu.Unlock()
	book.byType = byType
	return nil
}

func cloneSets(sets []RuleSet) []RuleSet {
	items := make([]RuleSet, 0, len(sets))
	for _, set := range sets {
		items = append(items, set.clone())
	}
	return items
}
