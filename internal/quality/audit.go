package quality

import (
	"fmt"
	"time"
)

// AuditDifference describes one field where the recalculated evaluation
// disagrees with the stored one.
type AuditDifference struct {
	Item         string `json:"item"`
	Stored       string `json:"stored"`
	Recalculated string `json:"recalculated"`
}

// AuditResult is the read-only outcome of recalculating a historical
// permit with the rules that applied at the time.
type AuditResult struct {
	PermitID     string            `json:"permit_id"`
	EventID      string            `json:"event_id"`
	Rule         RuleVersion       `json:"rule"`
	Stored       Evaluation        `json:"stored"`
	Recalculated Evaluation        `json:"recalculated"`
	Matches      bool              `json:"matches"`
	Differences  []AuditDifference `json:"differences,omitempty"`
	AuditedAt    time.Time         `json:"audited_at"`
}

// Recalculate replays a permit record against its own snapshots. The
// decision instant is the record's RecordedAt, never the audit time, so
// evidence freshness is judged exactly as it was then. The function is
// pure: it never mutates the ledger.
func Recalculate(record PermitRecord, auditedAt time.Time) AuditResult {
	active := make([]Exception, 0, len(record.Exceptions))
	for _, exception := range record.Exceptions {
		if exception.Active(record.RecordedAt) {
			active = append(active, exception)
		}
	}
	again := Evaluate(record.RuleSet, record.Evidence, active, record.RecordedAt, record.Evaluation.VideoLocked, record.Evaluation.CaptionReady)

	result := AuditResult{
		PermitID:     record.ID,
		EventID:      string(record.EventID),
		Rule:         record.Rule,
		Stored:       record.Evaluation,
		Recalculated: again,
		Matches:      true,
		AuditedAt:    auditedAt.UTC(),
	}
	compare := func(item string, stored bool, recalculated bool) {
		if stored != recalculated {
			result.Matches = false
			result.Differences = append(result.Differences, AuditDifference{
				Item:         item,
				Stored:       fmt.Sprintf("%t", stored),
				Recalculated: fmt.Sprintf("%t", recalculated),
			})
		}
	}
	compare("ready", record.Evaluation.Ready, again.Ready)
	compare("audio_ready", record.Evaluation.AudioReady, again.AudioReady)
	storedProbes := make(map[string]bool, len(record.Evaluation.Probes))
	for _, verdict := range record.Evaluation.Probes {
		storedProbes[verdict.Kind] = verdict.Satisfied
	}
	for _, verdict := range again.Probes {
		compare("probe:"+verdict.Kind, storedProbes[verdict.Kind], verdict.Satisfied)
	}
	type evidenceKey struct {
		probe string
		kind  string
	}
	storedEvidence := make(map[evidenceKey]bool, len(record.Evaluation.Evidence))
	for _, verdict := range record.Evaluation.Evidence {
		storedEvidence[evidenceKey{verdict.ProbeID, verdict.Kind}] = verdict.Accepted
	}
	for _, verdict := range again.Evidence {
		key := evidenceKey{verdict.ProbeID, verdict.Kind}
		compare("evidence:"+key.probe+":"+key.kind, storedEvidence[key], verdict.Accepted)
	}
	return result
}
