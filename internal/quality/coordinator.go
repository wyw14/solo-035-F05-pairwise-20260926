package quality

import (
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

type Coordinator struct {
	evidence *EvidenceSet
	clock    func() time.Time
}

func NewCoordinator(evidence *EvidenceSet, clock func() time.Time) *Coordinator {
	return &Coordinator{evidence: evidence, clock: clock}
}

func (coordinator *Coordinator) Record(eventID model.EventID, item model.AudioEvidence) error {
	if item.MeasuredAt.IsZero() {
		item.MeasuredAt = coordinator.clock().UTC()
	}
	return coordinator.evidence.Add(eventID, item)
}

func (coordinator *Coordinator) Reset(eventID model.EventID) {
	coordinator.evidence.Clear(eventID)
}
