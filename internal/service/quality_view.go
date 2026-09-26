package service

import "github.com/wyw14/solo-035/internal/quality"

// QualityView exposes the versioned rule ledger: the catalog of published
// rule sets, the per-revision bindings, exception approvals and the historical
// clearance decisions.
type QualityView struct {
	Rules      []quality.RuleSet     `json:"rules"`
	Bindings   []quality.RuleBinding `json:"bindings"`
	Exceptions []quality.Exception   `json:"exceptions"`
	Decisions  []quality.Decision    `json:"decisions"`
}

func (runtime *Runtime) QualityOverview() QualityView {
	return QualityView{
		Rules:      runtime.QualityPolicy.Catalog(),
		Bindings:   runtime.QualityPolicy.Bindings(),
		Exceptions: runtime.QualityPolicy.Exceptions(),
		Decisions:  runtime.QualityPolicy.Decisions(""),
	}
}
