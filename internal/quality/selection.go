package quality

import (
	"fmt"
	"sync"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

// RuleSelection pins one program revision of an event to the rule version
// that was current when the revision was created. Pins are append-only:
// a revision never changes its rule version afterwards.
type RuleSelection struct {
	EventID  model.EventID    `json:"event_id"`
	Revision model.RevisionID `json:"revision"`
	Rule     RuleVersion      `json:"rule"`
	PinnedAt time.Time        `json:"pinned_at"`
}

type SelectionLog struct {
	mu    sync.RWMutex
	items []RuleSelection
	index map[model.EventID]map[model.RevisionID]RuleVersion
}

func NewSelectionLog() *SelectionLog {
	return &SelectionLog{index: make(map[model.EventID]map[model.RevisionID]RuleVersion)}
}

func (log *SelectionLog) Pin(eventID model.EventID, revision model.RevisionID, rule RuleVersion, now time.Time) error {
	if !eventID.Valid() || !revision.Valid() {
		return fmt.Errorf("rule selection has an invalid identity")
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	if _, pinned := log.index[eventID][revision]; pinned {
		return fmt.Errorf("event %s revision %s already has a pinned rule", eventID, revision)
	}
	selection := RuleSelection{EventID: eventID, Revision: revision, Rule: rule, PinnedAt: now.UTC()}
	log.items = append(log.items, selection)
	if log.index[eventID] == nil {
		log.index[eventID] = make(map[model.RevisionID]RuleVersion)
	}
	log.index[eventID][revision] = rule
	return nil
}

func (log *SelectionLog) Current(eventID model.EventID, revision model.RevisionID) (RuleSelection, bool) {
	log.mu.RLock()
	defer log.mu.RUnlock()
	if _, ok := log.index[eventID][revision]; !ok {
		return RuleSelection{}, false
	}
	for _, selection := range log.items {
		if selection.EventID == eventID && selection.Revision == revision {
			return selection, true
		}
	}
	return RuleSelection{}, false
}

func (log *SelectionLog) Latest(eventID model.EventID) (RuleSelection, bool) {
	log.mu.RLock()
	defer log.mu.RUnlock()
	for index := len(log.items) - 1; index >= 0; index-- {
		if log.items[index].EventID == eventID {
			return log.items[index], true
		}
	}
	return RuleSelection{}, false
}

func (log *SelectionLog) List(eventID model.EventID) []RuleSelection {
	log.mu.RLock()
	defer log.mu.RUnlock()
	items := make([]RuleSelection, 0)
	for _, selection := range log.items {
		if eventID == "" || selection.EventID == eventID {
			items = append(items, selection)
		}
	}
	return items
}

func (log *SelectionLog) Export() []RuleSelection {
	return log.List("")
}

func (log *SelectionLog) Restore(items []RuleSelection) error {
	restored := NewSelectionLog()
	for _, selection := range items {
		if err := restored.Pin(selection.EventID, selection.Revision, selection.Rule, selection.PinnedAt); err != nil {
			return err
		}
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	log.items = restored.items
	log.index = restored.index
	return nil
}
