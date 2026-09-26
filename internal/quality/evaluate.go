package quality

import (
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

// EvidenceVerdict records how one piece of evidence fared under a rule set.
type EvidenceVerdict struct {
	ProbeID  string   `json:"probe_id"`
	Kind     string   `json:"kind"`
	Value    float64  `json:"value"`
	InRange  bool     `json:"in_range"`
	Fresh    bool     `json:"fresh"`
	Accepted bool     `json:"accepted"`
	WaivedBy []string `json:"waived_by,omitempty"`
}

// ProbeVerdict records whether one probe-kind requirement was satisfied.
type ProbeVerdict struct {
	Kind            string   `json:"kind"`
	RequiredSources int      `json:"required_sources"`
	ActualSources   int      `json:"actual_sources"`
	Satisfied       bool     `json:"satisfied"`
	WaivedBy        []string `json:"waived_by,omitempty"`
}

// Evaluation is the full, explainable outcome of assessing evidence
// against a rule set at a moment in time.
type Evaluation struct {
	Rule         RuleVersion       `json:"rule"`
	Ready        bool              `json:"ready"`
	VideoLocked  bool              `json:"video_locked"`
	CaptionReady bool              `json:"caption_ready"`
	AudioReady   bool              `json:"audio_ready"`
	Evidence     []EvidenceVerdict `json:"evidence"`
	Probes       []ProbeVerdict    `json:"probes"`
	Exceptions   []string          `json:"exceptions,omitempty"`
	Reason       string            `json:"reason"`
	EvaluatedAt  time.Time         `json:"evaluated_at"`
}

// Evaluate is a pure function: the same rule set, evidence, exceptions and
// instant always produce the same verdict, which is what makes historical
// permits recalculable.
func Evaluate(set RuleSet, evidence []model.AudioEvidence, exceptions []Exception, now time.Time, videoLocked bool, captionReady bool) Evaluation {
	now = now.UTC()
	loudnessWaivers := exceptionIDs(exceptions, ExceptionLoudnessRange, "")
	ageWaivers := exceptionIDs(exceptions, ExceptionEvidenceAge, "")

	verdicts := make([]EvidenceVerdict, 0, len(evidence))
	for _, item := range evidence {
		verdict := EvidenceVerdict{
			ProbeID: item.ProbeID,
			Kind:    item.Kind,
			Value:   item.Value,
			InRange: item.Value >= set.Thresholds.MinimumLoudness && item.Value <= set.Thresholds.MaximumLoudness,
			Fresh:   now.Sub(item.MeasuredAt.UTC()) <= time.Duration(set.Thresholds.MaximumAgeMillis)*time.Millisecond,
		}
		inRange := verdict.InRange
		if !inRange && len(loudnessWaivers) > 0 {
			inRange = true
			verdict.WaivedBy = append(verdict.WaivedBy, loudnessWaivers...)
		}
		fresh := verdict.Fresh
		if !fresh && len(ageWaivers) > 0 {
			fresh = true
			verdict.WaivedBy = append(verdict.WaivedBy, ageWaivers...)
		}
		verdict.Accepted = inRange && fresh
		verdicts = append(verdicts, verdict)
	}

	probes := make([]ProbeVerdict, 0, len(set.Probes))
	for _, requirement := range set.Probes {
		origins := make(map[string]bool)
		for index, item := range evidence {
			if item.Kind != requirement.Kind || !verdicts[index].Accepted {
				continue
			}
			origin := item.ProbeID
			if item.DerivedFrom != "" {
				origin = item.DerivedFrom
			}
			origins[origin] = true
		}
		verdict := ProbeVerdict{
			Kind:            requirement.Kind,
			RequiredSources: requirement.MinimumSources,
			ActualSources:   len(origins),
		}
		verdict.Satisfied = verdict.ActualSources >= requirement.MinimumSources
		if !verdict.Satisfied {
			if waivers := exceptionIDs(exceptions, ExceptionProbeSources, requirement.Kind); len(waivers) > 0 {
				verdict.Satisfied = true
				verdict.WaivedBy = waivers
			}
		}
		probes = append(probes, verdict)
	}

	evaluation := Evaluation{
		Rule:         set.Key(),
		VideoLocked:  videoLocked,
		CaptionReady: captionReady,
		AudioReady:   true,
		Evidence:     verdicts,
		Probes:       probes,
		EvaluatedAt:  now,
	}
	for _, verdict := range probes {
		if !verdict.Satisfied {
			evaluation.AudioReady = false
			break
		}
	}
	evaluation.Ready = evaluation.VideoLocked && evaluation.AudioReady && evaluation.CaptionReady
	used := make(map[string]bool)
	for _, verdict := range verdicts {
		for _, id := range verdict.WaivedBy {
			used[id] = true
		}
	}
	for _, verdict := range probes {
		for _, id := range verdict.WaivedBy {
			used[id] = true
		}
	}
	for _, exception := range exceptions {
		if used[exception.ID] {
			evaluation.Exceptions = append(evaluation.Exceptions, exception.ID)
		}
	}
	switch {
	case !evaluation.VideoLocked:
		evaluation.Reason = "video is not locked"
	case !evaluation.AudioReady:
		evaluation.Reason = "independent audio evidence is missing"
	case !evaluation.CaptionReady:
		evaluation.Reason = "caption chain is not ready"
	default:
		evaluation.Reason = "all playout interlocks passed"
	}
	return evaluation
}

func exceptionIDs(exceptions []Exception, item string, kind string) []string {
	ids := make([]string, 0)
	for _, exception := range exceptions {
		if exception.Item == item && exception.Kind == kind {
			ids = append(ids, exception.ID)
		}
	}
	return ids
}
