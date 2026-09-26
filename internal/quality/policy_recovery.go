package quality

import (
	"fmt"

	"github.com/wyw14/solo-035/internal/model"
)

// PolicyRecord is the checkpoint representation of the full quality rule
// ledger so historical bindings, approvals and decisions survive restarts.
type PolicyRecord struct {
	Rules      []RuleSet     `json:"rules"`
	Bindings   []RuleBinding `json:"bindings"`
	Exceptions []Exception   `json:"exceptions"`
	Decisions  []Decision    `json:"decisions"`
}

// Export snapshots the ledger. Pass nil event IDs to export everything.
func (policy *Policy) Export(eventIDs []model.EventID) PolicyRecord {
	policy.mu.RLock()
	defer policy.mu.RUnlock()
	record := PolicyRecord{}
	for _, rule := range policy.rules {
		record.Rules = append(record.Rules, rule)
	}
	scoped := make(map[model.EventID]bool)
	if eventIDs != nil {
		for _, id := range eventIDs {
			scoped[id] = true
		}
	}
	for _, byRevision := range policy.bindings {
		for _, binding := range byRevision {
			if eventIDs != nil && !scoped[binding.EventID] {
				continue
			}
			record.Bindings = append(record.Bindings, binding)
		}
	}
	for _, exception := range policy.exceptions {
		if eventIDs != nil && !scoped[exception.EventID] {
			continue
		}
		record.Exceptions = append(record.Exceptions, exception)
	}
	for eventID, decisions := range policy.decisions {
		if eventIDs != nil && !scoped[eventID] {
			continue
		}
		record.Decisions = append(record.Decisions, decisions...)
	}
	return record
}

// Restore rebuilds the ledger. Rules are validated; bindings and decisions
// are kept verbatim because they are historical snapshots.
func (policy *Policy) Restore(record PolicyRecord) error {
	policy.mu.Lock()
	defer policy.mu.Unlock()
	policy.rules = make(map[string]RuleSet)
	policy.versions = make(map[Profile]int)
	policy.published = make(map[Profile][]string)
	policy.bindings = make(map[model.EventID]map[model.RevisionID]RuleBinding)
	policy.exceptions = make(map[string]Exception)
	policy.decisions = make(map[model.EventID][]Decision)
	policy.exceptionSeq = 0

	if len(record.Rules) == 0 {
		policy.seedDefaults()
		return nil
	}
	for _, rule := range record.Rules {
		if err := rule.Validate(); err != nil {
			return err
		}
		if _, exists := policy.rules[rule.ID]; exists {
			return duplicate("rule", rule.ID)
		}
		policy.rules[rule.ID] = rule
		if rule.Version > policy.versions[rule.Profile] {
			policy.versions[rule.Profile] = rule.Version
		}
	}
	for profile := range policy.versions {
		for version := 1; version <= policy.versions[profile]; version++ {
			id := ruleID(profile, version)
			if _, ok := policy.rules[id]; !ok {
				return missing(id)
			}
			policy.published[profile] = append(policy.published[profile], id)
		}
	}
	for _, binding := range record.Bindings {
		if _, ok := policy.rules[binding.RuleID]; !ok {
			return missing(binding.RuleID)
		}
		if policy.bindings[binding.EventID] == nil {
			policy.bindings[binding.EventID] = make(map[model.RevisionID]RuleBinding)
		}
		policy.bindings[binding.EventID][binding.Revision] = binding
	}
	for _, exception := range record.Exceptions {
		if err := exception.Validate(); err != nil {
			return err
		}
		policy.exceptions[exception.ID] = exception
		var sequence int
		if _, err := fmt.Sscanf(exception.ID, "exception-%d", &sequence); err == nil && sequence > policy.exceptionSeq {
			policy.exceptionSeq = sequence
		}
	}
	for _, decision := range record.Decisions {
		policy.decisions[decision.EventID] = append(policy.decisions[decision.EventID], decision)
	}
	return nil
}

func duplicate(kind, id string) error {
	return fmt.Errorf("quality recovery has duplicate %s %s", kind, id)
}

func missing(id string) error {
	return fmt.Errorf("quality recovery references missing rule %s", id)
}
