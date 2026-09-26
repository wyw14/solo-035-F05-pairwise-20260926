package model

import (
	"errors"
	"fmt"
	"time"
)

type EventPhase string

// Profile identifies the broadcast family of an event. Different families
// carry different on-air quality requirements.
type Profile string

const (
	ProfileStandard Profile = "standard"
	ProfileNews     Profile = "news"
	ProfileSports   Profile = "sports"
	ProfileMusic    Profile = "music"
)

func ValidProfile(profile Profile) bool {
	switch profile {
	case "", ProfileStandard, ProfileNews, ProfileSports, ProfileMusic:
		return true
	default:
		return false
	}
}

const (
	PhasePreparing  EventPhase = "preparing"
	PhaseRehearsal  EventPhase = "rehearsal"
	PhaseArmed      EventPhase = "armed"
	PhaseOnAir      EventPhase = "on-air"
	PhaseHolding    EventPhase = "holding"
	PhaseFailedOver EventPhase = "failed-over"
	PhaseClosed     EventPhase = "closed"
)

var phaseTransitions = map[EventPhase]map[EventPhase]bool{
	PhasePreparing:  {PhaseRehearsal: true, PhaseClosed: true},
	PhaseRehearsal:  {PhaseArmed: true, PhasePreparing: true, PhaseClosed: true},
	PhaseArmed:      {PhaseOnAir: true, PhaseHolding: true, PhaseClosed: true},
	PhaseOnAir:      {PhaseHolding: true, PhaseFailedOver: true, PhaseClosed: true},
	PhaseHolding:    {PhaseOnAir: true, PhaseFailedOver: true, PhaseClosed: true},
	PhaseFailedOver: {PhaseOnAir: true, PhaseHolding: true, PhaseClosed: true},
	PhaseClosed:     {},
}

type LiveEvent struct {
	ID               EventID    `json:"id"`
	Name             string     `json:"name"`
	Profile          Profile    `json:"profile"`
	Phase            EventPhase `json:"phase"`
	Revision         RevisionID `json:"revision"`
	CommandEpoch     uint64     `json:"command_epoch"`
	SourceGeneration uint64     `json:"source_generation"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func NewLiveEvent(name string, now time.Time) (LiveEvent, error) {
	return NewLiveEventForProfile(name, ProfileStandard, now)
}

func NewLiveEventForProfile(name string, profile Profile, now time.Time) (LiveEvent, error) {
	if name == "" {
		return LiveEvent{}, errors.New("event name is required")
	}
	if profile == "" {
		profile = ProfileStandard
	}
	if !ValidProfile(profile) {
		return LiveEvent{}, fmt.Errorf("event broadcast profile %q is not supported", profile)
	}
	return LiveEvent{
		ID:               NewEventID(),
		Name:             name,
		Profile:          profile,
		Phase:            PhasePreparing,
		Revision:         NewRevisionID(),
		CommandEpoch:     1,
		SourceGeneration: 1,
		CreatedAt:        now.UTC(),
		UpdatedAt:        now.UTC(),
	}, nil
}

func (event LiveEvent) CanTransition(next EventPhase) bool {
	return phaseTransitions[event.Phase][next]
}

func (event LiveEvent) Transition(next EventPhase, now time.Time) (LiveEvent, error) {
	if !event.CanTransition(next) {
		return event, fmt.Errorf("event %s cannot transition from %s to %s", event.ID, event.Phase, next)
	}
	event.Phase = next
	event.CommandEpoch++
	event.UpdatedAt = now.UTC()
	return event, nil
}

func (event LiveEvent) NewProgram(now time.Time) LiveEvent {
	event.Revision = NewRevisionID()
	event.CommandEpoch++
	event.UpdatedAt = now.UTC()
	return event
}

func (event LiveEvent) NewSource(now time.Time) LiveEvent {
	event.SourceGeneration++
	event.CommandEpoch++
	event.UpdatedAt = now.UTC()
	return event
}
