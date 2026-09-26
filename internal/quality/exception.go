package quality

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/solo-035/internal/model"
)

// Exception items identify the individual rules an approval may waive.
const (
	ExceptionLoudnessRange = "loudness-range"
	ExceptionEvidenceAge   = "evidence-age"
	ExceptionProbeSources  = "probe-sources"
)

// Exception is an approved, auditable waiver of one rule item for one
// event under one rule version. Exceptions expire rather than being
// revoked, so the registry stays append-only.
type Exception struct {
	ID         string        `json:"id"`
	EventID    model.EventID `json:"event_id"`
	Rule       RuleVersion   `json:"rule"`
	Item       string        `json:"item"`
	Kind       string        `json:"kind,omitempty"`
	ApprovedBy string        `json:"approved_by"`
	Reason     string        `json:"reason"`
	GrantedAt  time.Time     `json:"granted_at"`
	ExpiresAt  *time.Time    `json:"expires_at,omitempty"`
}

func (exception Exception) Active(now time.Time) bool {
	if exception.ExpiresAt == nil {
		return true
	}
	return now.Before(*exception.ExpiresAt)
}

type ExceptionRegistry struct {
	mu    sync.RWMutex
	items []Exception
}

func NewExceptionRegistry() *ExceptionRegistry {
	return &ExceptionRegistry{}
}

func (registry *ExceptionRegistry) Grant(eventID model.EventID, rule RuleVersion, item string, kind string, approver string, reason string, now time.Time, ttl time.Duration) (Exception, error) {
	if !eventID.Valid() {
		return Exception{}, fmt.Errorf("exception has an invalid event")
	}
	switch item {
	case ExceptionLoudnessRange, ExceptionEvidenceAge:
		if kind != "" {
			return Exception{}, fmt.Errorf("exception %s does not take a probe kind", item)
		}
	case ExceptionProbeSources:
		if kind == "" {
			return Exception{}, fmt.Errorf("exception %s requires a probe kind", item)
		}
	default:
		return Exception{}, fmt.Errorf("exception item %q is not supported", item)
	}
	if approver == "" {
		return Exception{}, fmt.Errorf("exception approver is required")
	}
	if reason == "" {
		return Exception{}, fmt.Errorf("exception reason is required")
	}
	exception := Exception{
		ID:         uuid.NewString(),
		EventID:    eventID,
		Rule:       rule,
		Item:       item,
		Kind:       kind,
		ApprovedBy: approver,
		Reason:     reason,
		GrantedAt:  now.UTC(),
	}
	if ttl > 0 {
		expires := now.UTC().Add(ttl)
		exception.ExpiresAt = &expires
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.items = append(registry.items, exception)
	return exception, nil
}

func (registry *ExceptionRegistry) Active(eventID model.EventID, rule RuleVersion, now time.Time) []Exception {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	active := make([]Exception, 0)
	for _, exception := range registry.items {
		if exception.EventID == eventID && exception.Rule == rule && exception.Active(now) {
			active = append(active, exception)
		}
	}
	return active
}

func (registry *ExceptionRegistry) List(eventID model.EventID) []Exception {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	items := make([]Exception, 0)
	for _, exception := range registry.items {
		if eventID == "" || exception.EventID == eventID {
			items = append(items, exception)
		}
	}
	return items
}

func (registry *ExceptionRegistry) Export() []Exception {
	return registry.List("")
}

func (registry *ExceptionRegistry) Restore(items []Exception) error {
	seen := make(map[string]bool, len(items))
	for _, exception := range items {
		if exception.ID == "" || seen[exception.ID] {
			return fmt.Errorf("exception identity is empty or duplicated")
		}
		seen[exception.ID] = true
		if !exception.EventID.Valid() {
			return fmt.Errorf("exception %s has an invalid event", exception.ID)
		}
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.items = append([]Exception(nil), items...)
	return nil
}
