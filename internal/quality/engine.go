package quality

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/solo-035/internal/model"
)

// PermitEngine assembles the permit decision from the pinned rule version,
// the live evidence, and the active exceptions, and journals every
// decision into the ledger.
type PermitEngine struct {
	evidence   *EvidenceSet
	book       *Rulebook
	selections *SelectionLog
	exceptions *ExceptionRegistry
	ledger     *PermitLedger
	clock      func() time.Time
}

func NewPermitEngine(evidence *EvidenceSet, book *Rulebook, selections *SelectionLog, exceptions *ExceptionRegistry, ledger *PermitLedger, clock func() time.Time) *PermitEngine {
	return &PermitEngine{evidence: evidence, book: book, selections: selections, exceptions: exceptions, ledger: ledger, clock: clock}
}

func (engine *PermitEngine) Permit(eventID model.EventID, revision model.RevisionID, videoLocked bool, captionReady bool) (Evaluation, error) {
	now := engine.clock().UTC()
	selection, ok := engine.selections.Current(eventID, revision)
	if !ok {
		return Evaluation{}, fmt.Errorf("event %s revision %s has no pinned quality rule", eventID, revision)
	}
	set, ok := engine.book.Version(selection.Rule)
	if !ok {
		return Evaluation{}, fmt.Errorf("quality rule %s v%d is not in the rulebook", selection.Rule.EventType, selection.Rule.Version)
	}
	evidence := engine.evidence.List(eventID)
	exceptions := engine.exceptions.Active(eventID, selection.Rule, now)
	evaluation := Evaluate(set, evidence, exceptions, now, videoLocked, captionReady)
	record := PermitRecord{
		ID:         uuid.NewString(),
		EventID:    eventID,
		Revision:   revision,
		Rule:       selection.Rule,
		RuleSet:    set,
		Evidence:   evidence,
		Exceptions: exceptions,
		Evaluation: evaluation,
		Granted:    evaluation.Ready,
		RecordedAt: now,
	}
	if err := engine.ledger.Append(record); err != nil {
		return evaluation, err
	}
	if !evaluation.Ready {
		return evaluation, fmt.Errorf("on-air permit denied: %s", evaluation.Reason)
	}
	return evaluation, nil
}
