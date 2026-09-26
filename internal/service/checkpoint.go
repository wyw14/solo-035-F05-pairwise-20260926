package service

import (
	"fmt"

	"github.com/wyw14/solo-035/internal/backup"
	"github.com/wyw14/solo-035/internal/event"
	"github.com/wyw14/solo-035/internal/journal"
	"github.com/wyw14/solo-035/internal/model"
	"github.com/wyw14/solo-035/internal/quality"
	"github.com/wyw14/solo-035/internal/syncer"
)

type ControlCheckpoint struct {
	Events     []event.RecoveryRecord   `json:"events"`
	Rules      []quality.RuleSet        `json:"rules"`
	Selections []quality.RuleSelection  `json:"selections"`
	Exceptions []quality.Exception      `json:"exceptions"`
	Permits    []quality.PermitRecord   `json:"permits"`
	Sync       []syncer.Recovery        `json:"sync"`
	Backup     []backup.RecoveryRecord  `json:"backup"`
	Quality    []quality.RecoveryRecord `json:"quality"`
	Journal    []journal.Record         `json:"journal"`
}

func (runtime *Runtime) ExportCheckpoint() ControlCheckpoint {
	events := runtime.Events.ExportRecovery()
	ids := make([]model.EventID, 0, len(events))
	for _, record := range events {
		ids = append(ids, record.Event.ID)
	}
	return ControlCheckpoint{
		Events:     events,
		Rules:      runtime.Rulebook.Export(),
		Selections: runtime.Selections.Export(),
		Exceptions: runtime.Exceptions.Export(),
		Permits:    runtime.Permits.Export(),
		Sync:       runtime.SyncState.ExportRecovery(),
		Backup:     runtime.BackupState.ExportRecovery(),
		Quality:    runtime.Evidence.Export(ids),
		Journal:    runtime.Journal.Records(),
	}
}

func (runtime *Runtime) RestoreCheckpoint(checkpoint ControlCheckpoint) (RecoveryReport, error) {
	if err := runtime.Events.Restore(checkpoint.Events, runtime.Clock()); err != nil {
		return RecoveryReport{}, fmt.Errorf("restore events: %w", err)
	}
	if len(checkpoint.Rules) == 0 {
		if err := runtime.backfillLegacyCheckpoint(checkpoint); err != nil {
			return RecoveryReport{}, err
		}
	} else {
		if err := runtime.Rulebook.Restore(checkpoint.Rules); err != nil {
			return RecoveryReport{}, fmt.Errorf("restore quality rules: %w", err)
		}
		if err := runtime.Selections.Restore(checkpoint.Selections); err != nil {
			return RecoveryReport{}, fmt.Errorf("restore rule selections: %w", err)
		}
		if err := runtime.Exceptions.Restore(checkpoint.Exceptions); err != nil {
			return RecoveryReport{}, fmt.Errorf("restore quality exceptions: %w", err)
		}
		if err := runtime.Permits.Restore(checkpoint.Permits); err != nil {
			return RecoveryReport{}, fmt.Errorf("restore quality permits: %w", err)
		}
		if err := runtime.validateRuleReferences(); err != nil {
			return RecoveryReport{}, err
		}
	}
	if err := runtime.SyncState.RestoreRecovery(checkpoint.Sync); err != nil {
		return RecoveryReport{}, fmt.Errorf("restore synchronization: %w", err)
	}
	if err := runtime.BackupState.RestoreRecovery(checkpoint.Backup); err != nil {
		return RecoveryReport{}, fmt.Errorf("restore failovers: %w", err)
	}
	if err := runtime.Evidence.Restore(checkpoint.Quality); err != nil {
		return RecoveryReport{}, fmt.Errorf("restore quality evidence: %w", err)
	}
	sources := make(map[model.EventID]model.SourceID)
	for _, item := range checkpoint.Sync {
		sources[item.EventID] = item.Source
	}
	return runtime.RestoreFromRecords(checkpoint.Journal, func(eventID model.EventID) model.SourceID {
		sourceID := sources[eventID]
		if sourceID == "" {
			sourceID = model.NewSourceID()
		}
		return sourceID
	})
}

// validateRuleReferences ensures selections and exceptions never point at a
// rule version absent from the restored rulebook.
func (runtime *Runtime) validateRuleReferences() error {
	for _, selection := range runtime.Selections.Export() {
		if _, ok := runtime.Rulebook.Version(selection.Rule); !ok {
			return fmt.Errorf("rule selection for event %s references missing %s v%d", selection.EventID, selection.Rule.EventType, selection.Rule.Version)
		}
	}
	for _, exception := range runtime.Exceptions.Export() {
		if _, ok := runtime.Rulebook.Version(exception.Rule); !ok {
			return fmt.Errorf("exception %s references missing %s v%d", exception.ID, exception.Rule.EventType, exception.Rule.Version)
		}
	}
	return nil
}

// backfillLegacyCheckpoint loads checkpoints written before versioned
// quality rules: seed the default rulebook, treat events without a type as
// news, and pin every current revision to news v1.
func (runtime *Runtime) backfillLegacyCheckpoint(checkpoint ControlCheckpoint) error {
	book, err := seedRulebook(runtime.Clock)
	if err != nil {
		return fmt.Errorf("seed quality rules: %w", err)
	}
	runtime.Rulebook = book
	runtime.Selections = quality.NewSelectionLog()
	runtime.Exceptions = quality.NewExceptionRegistry()
	runtime.Permits = quality.NewPermitLedger()
	runtime.PermitFlow = quality.NewPermitEngine(runtime.Evidence, runtime.Rulebook, runtime.Selections, runtime.Exceptions, runtime.Permits, runtime.Clock)
	for _, record := range checkpoint.Events {
		if record.Event.Type == "" {
			record.Event.Type = "news"
			if err := runtime.Events.Update(record.Event); err != nil {
				return fmt.Errorf("backfill event %s type: %w", record.Event.ID, err)
			}
		}
		rules, ok := runtime.Rulebook.Current(record.Event.Type)
		if !ok {
			return fmt.Errorf("backfill event %s: event type %s has no rules", record.Event.ID, record.Event.Type)
		}
		if err := runtime.Selections.Pin(record.Event.ID, record.Event.Revision, rules.Key(), runtime.Clock()); err != nil {
			return fmt.Errorf("backfill rule selection for event %s: %w", record.Event.ID, err)
		}
	}
	return nil
}
