package quality

import (
	"fmt"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

// ProfileFor resolves the broadcast profile of an event. An empty result
// falls back to the standard profile.
type ProfileFor func(eventID model.EventID) Profile

type Coordinator struct {
	evidence *EvidenceSet
	policy   *Policy
	profiles ProfileFor
	clock    func() time.Time
}

func NewCoordinator(evidence *EvidenceSet, policy *Policy, profiles ProfileFor, clock func() time.Time) *Coordinator {
	if profiles == nil {
		profiles = func(model.EventID) Profile { return ProfileStandard }
	}
	return &Coordinator{evidence: evidence, policy: policy, profiles: profiles, clock: clock}
}

// Profile returns the broadcast family declared for an event.
func (coordinator *Coordinator) Profile(eventID model.EventID) Profile {
	profile := coordinator.profiles(eventID)
	if profile == "" {
		return ProfileStandard
	}
	return profile
}

func (coordinator *Coordinator) Record(eventID model.EventID, revision model.RevisionID, item model.AudioEvidence) error {
	if item.MeasuredAt.IsZero() {
		item.MeasuredAt = coordinator.clock().UTC()
	}
	now := coordinator.clock().UTC()
	// Recording evidence does not pin a revision to a rule: the binding is
	// created by the first Permit decision. Until then the current rule of
	// the profile is used as the admission gate.
	rule, bound := coordinator.policy.BoundRule(eventID, revision)
	if !bound {
		rule = coordinator.policy.Current(coordinator.Profile(eventID))
	}
	exceptions := coordinator.policy.ExceptionsFor(eventID, revision, now)
	if err := Admit(rule, eventID, revision, item, now, exceptions); err != nil {
		return err
	}
	return coordinator.evidence.Add(eventID, item)
}

func (coordinator *Coordinator) IndependentAudioSources(eventID model.EventID, revision model.RevisionID) int {
	now := coordinator.clock().UTC()
	rule, bound := coordinator.policy.BoundRule(eventID, revision)
	if !bound {
		rule = coordinator.policy.Current(coordinator.Profile(eventID))
	}
	result := Evaluate(rule, eventID, revision, coordinator.evidence.List(eventID), now,
		coordinator.policy.ExceptionsFor(eventID, revision, now))
	return result.IndependentSources
}

// Permit evaluates an event revision against the rule set bound to that
// revision and appends a decision to the ledger. The decision snapshots the
// rule, evidence and exceptions in force so the clearance can be recomputed
// later even after newer rules are published.
func (coordinator *Coordinator) Permit(eventID model.EventID, revision model.RevisionID, videoLocked bool, captionReady bool) (model.Readiness, error) {
	now := coordinator.clock().UTC()
	rule := coordinator.policy.Resolve(eventID, revision, coordinator.Profile(eventID), now)
	items := coordinator.evidence.List(eventID)
	exceptions := coordinator.policy.ExceptionsFor(eventID, revision, now)
	result := Evaluate(rule, eventID, revision, items, now, exceptions)

	ready := model.Readiness{VideoLocked: videoLocked, CheckedAt: now}
	ready.AudioReady = result.Passed
	ready.CaptionReady = captionReady
	ready.Ready = ready.VideoLocked && ready.AudioReady && ready.CaptionReady
	switch {
	case !videoLocked:
		ready.Reason = "video is not locked"
	case !ready.AudioReady:
		ready.Reason = "audio quality rule " + rule.ID + " failed: " + firstViolation(result)
	case !captionReady:
		ready.Reason = "caption chain is not ready"
	default:
		ready.Reason = "all playout interlocks passed under rule " + rule.ID
	}

	decision := Decision{
		EventID:    eventID,
		Revision:   revision,
		Rule:       rule,
		Evidence:   append([]model.AudioEvidence(nil), items...),
		Exceptions: append([]Exception(nil), exceptions...),
		Result:     result,
		Permitted:  ready.Ready,
		Reason:     ready.Reason,
		DecidedAt:  now,
	}
	coordinator.policy.RecordDecision(decision)

	if !ready.Ready {
		return ready, fmt.Errorf("on-air permit denied: %s", ready.Reason)
	}
	return ready, nil
}

// Reevaluate recomputes historical clearances using the rules that were in
// force when each decision was taken. Current rule revisions never override
// the snapshotted ones.
func (coordinator *Coordinator) Reevaluate(eventID model.EventID) ReplayReport {
	return coordinator.policy.Replay(eventID)
}

func (coordinator *Coordinator) Reset(eventID model.EventID) {
	coordinator.evidence.Clear(eventID)
}

func firstViolation(result Result) string {
	if len(result.Violations) == 0 {
		return "audio quality not satisfied"
	}
	return result.Violations[0].Code
}
