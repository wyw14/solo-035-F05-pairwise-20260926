package quality

import (
	"fmt"
	"sync"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

// PermitRecord is the immutable account of one on-air permit decision. It
// snapshots the rule set, the evidence, and the active exceptions at the
// moment of decision so the outcome can always be recalculated with the
// rules that applied then, even after the live state moved on.
type PermitRecord struct {
	ID         string                `json:"id"`
	EventID    model.EventID         `json:"event_id"`
	Revision   model.RevisionID      `json:"revision"`
	Rule       RuleVersion           `json:"rule"`
	RuleSet    RuleSet               `json:"ruleset"`
	Evidence   []model.AudioEvidence `json:"evidence"`
	Exceptions []Exception           `json:"exceptions"`
	Evaluation Evaluation            `json:"evaluation"`
	Granted    bool                  `json:"granted"`
	RecordedAt time.Time             `json:"recorded_at"`
}

// PermitLedger is the append-only journal of permit decisions. Both
// granted and denied permits are recorded.
type PermitLedger struct {
	mu      sync.RWMutex
	records []PermitRecord
}

func NewPermitLedger() *PermitLedger {
	return &PermitLedger{}
}

func (ledger *PermitLedger) Append(record PermitRecord) error {
	if record.ID == "" {
		return fmt.Errorf("permit record has no identity")
	}
	if !record.EventID.Valid() || !record.Revision.Valid() {
		return fmt.Errorf("permit record %s has an invalid identity", record.ID)
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	for _, existing := range ledger.records {
		if existing.ID == record.ID {
			return fmt.Errorf("permit record %s already exists", record.ID)
		}
	}
	ledger.records = append(ledger.records, record)
	return nil
}

func (ledger *PermitLedger) Get(id string) (PermitRecord, bool) {
	ledger.mu.RLock()
	defer ledger.mu.RUnlock()
	for _, record := range ledger.records {
		if record.ID == id {
			return record, true
		}
	}
	return PermitRecord{}, false
}

func (ledger *PermitLedger) List(eventID model.EventID) []PermitRecord {
	ledger.mu.RLock()
	defer ledger.mu.RUnlock()
	items := make([]PermitRecord, 0)
	for _, record := range ledger.records {
		if eventID == "" || record.EventID == eventID {
			items = append(items, record)
		}
	}
	return items
}

func (ledger *PermitLedger) ListAll() []PermitRecord {
	return ledger.List("")
}

func (ledger *PermitLedger) Export() []PermitRecord {
	return ledger.ListAll()
}

func (ledger *PermitLedger) Restore(records []PermitRecord) error {
	restored := NewPermitLedger()
	for _, record := range records {
		if err := restored.Append(record); err != nil {
			return err
		}
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	ledger.records = restored.records
	return nil
}
