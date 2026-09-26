package service

import (
	"context"
	"fmt"
	"time"

	"github.com/wyw14/solo-035/internal/backup"
	"github.com/wyw14/solo-035/internal/model"
	"github.com/wyw14/solo-035/internal/quality"
)

type ControlRequest struct {
	Action          string                     `json:"action"`
	EventID         string                     `json:"event_id"`
	SourceID        string                     `json:"source_id"`
	EncoderID       string                     `json:"encoder_id"`
	Healthy         bool                       `json:"healthy"`
	Bitrate         int                        `json:"bitrate"`
	Count           int64                      `json:"count"`
	Message         string                     `json:"message"`
	DelayMillis     int64                      `json:"delay_millis"`
	EventType       string                     `json:"event_type,omitempty"`
	Approver        string                     `json:"approver,omitempty"`
	Item            string                     `json:"item,omitempty"`
	Kind            string                     `json:"kind,omitempty"`
	Minimum         float64                    `json:"minimum,omitempty"`
	Maximum         float64                    `json:"maximum,omitempty"`
	MaxAgeMillis    int64                      `json:"max_age_millis,omitempty"`
	Probes          []quality.ProbeRequirement `json:"probes,omitempty"`
	PermitID        string                     `json:"permit_id,omitempty"`
	ExpiresInMillis int64                      `json:"expires_in_millis,omitempty"`
}

type ControlResult struct {
	Action string `json:"action"`
	Status string `json:"status"`
	Value  any    `json:"value,omitempty"`
}

func (runtime *Runtime) ExecuteControl(ctx context.Context, request ControlRequest) (ControlResult, error) {
	result := ControlResult{Action: request.Action, Status: "accepted"}
	eventID := model.EventID(request.EventID)
	sourceID := model.SourceID(request.SourceID)
	coordinator := NewCoordinator(runtime)
	switch request.Action {
	case "hold-event":
		value, err := runtime.EventFlow.Hold(eventID)
		result.Value = value
		return result, err
	case "mark-failed-over":
		value, err := runtime.EventFlow.MarkFailedOver(eventID)
		result.Value = value
		return result, err
	case "advance-program":
		value, err := coordinator.AdvanceProgram(eventID)
		result.Value = value
		return result, err
	case "advance-source":
		value, err := runtime.Events.AdvanceSource(eventID, runtime.Clock())
		result.Value = value
		return result, err
	case "retire-source":
		runtime.SourceFlow.Retire(sourceID)
		result.Value = sourceID
		return result, nil
	case "close-sync":
		runtime.Sync.Close(eventID)
		result.Value = eventID
		return result, nil
	case "set-encoder-health":
		if !runtime.EncoderFlow.SetHealth(request.EncoderID, request.Healthy) {
			return result, fmt.Errorf("encoder %s does not exist", request.EncoderID)
		}
		result.Value = map[string]any{"encoder_id": request.EncoderID, "healthy": request.Healthy}
		return result, nil
	case "adjust-bitrate":
		program, ok := runtime.MuxState.Get(eventID)
		if !ok {
			return result, fmt.Errorf("program for event %s does not exist", eventID)
		}
		updated, err := program.WithBitrate(request.Bitrate)
		if err != nil {
			return result, err
		}
		if err := runtime.MuxState.Put(updated); err != nil {
			return result, err
		}
		result.Value = updated
		return result, nil
	case "stop-program":
		runtime.Mux.Stop(eventID)
		runtime.Playout.Stop(eventID)
		result.Value = eventID
		return result, nil
	case "audio-failover":
		if err := runtime.Audio.Failover(eventID); err != nil {
			return result, err
		}
		result.Value, _ = runtime.AudioState.Path(eventID)
		return result, nil
	case "caption-error":
		event, ok := runtime.Events.Get(eventID)
		if !ok {
			return result, fmt.Errorf("event %s not found", eventID)
		}
		result.Value = runtime.CaptionFlow.ReportError(eventID, event.Revision, request.Message)
		return result, nil
	case "quality-reset":
		runtime.Quality.Reset(eventID)
		result.Value = eventID
		return result, nil
	case "publish-quality-rules":
		thresholds := quality.Thresholds{MinimumLoudness: request.Minimum, MaximumLoudness: request.Maximum, MaximumAgeMillis: request.MaxAgeMillis}
		set, err := runtime.Rulebook.Publish(request.EventType, thresholds, request.Probes, runtime.Clock())
		if err != nil {
			return result, err
		}
		result.Value = set
		return result, nil
	case "grant-quality-exception":
		event, ok := runtime.Events.Get(eventID)
		if !ok {
			return result, fmt.Errorf("event %s not found", eventID)
		}
		selection, ok := runtime.Selections.Current(event.ID, event.Revision)
		if !ok {
			return result, fmt.Errorf("event %s revision %s has no pinned quality rule", event.ID, event.Revision)
		}
		exception, err := runtime.Exceptions.Grant(event.ID, selection.Rule, request.Item, request.Kind, request.Approver, request.Message, runtime.Clock(), time.Duration(request.ExpiresInMillis)*time.Millisecond)
		if err != nil {
			return result, err
		}
		result.Value = exception
		return result, nil
	case "recalculate-permit":
		record, ok := runtime.Permits.Get(request.PermitID)
		if !ok {
			return result, fmt.Errorf("permit %s not found", request.PermitID)
		}
		result.Value = quality.Recalculate(record, runtime.Clock())
		return result, nil
	case "record-gap":
		if err := runtime.Receiver.RecordGap(sourceID, request.Count); err != nil {
			return result, err
		}
		result.Value = map[string]any{"source": sourceID, "dropped": runtime.Receiver.Dropped(sourceID)}
		return result, nil
	case "schedule-failover":
		event, ok := runtime.Events.Get(eventID)
		if !ok {
			return result, fmt.Errorf("event %s not found", eventID)
		}
		decision, err := backup.Evaluate(event, 1, 0, runtime.Clock())
		if err != nil {
			return result, err
		}
		if err := decision.Validate(event); err != nil {
			return result, err
		}
		worker, err := coordinator.ScheduleFailover(ctx, eventID, time.Duration(request.DelayMillis)*time.Millisecond)
		if err != nil {
			return result, err
		}
		if err := <-worker; err != nil {
			return result, err
		}
		result.Value = decision
		return result, nil
	case "cancel-failover":
		if err := coordinator.CancelFailover(eventID); err != nil {
			return result, err
		}
		result.Value = eventID
		return result, nil
	case "recover-journal":
		report, err := runtime.Recover(func(model.EventID) model.SourceID {
			if sourceID.Valid() {
				return sourceID
			}
			return model.NewSourceID()
		})
		result.Value = report
		return result, err
	default:
		return result, fmt.Errorf("unsupported control action %q", request.Action)
	}
}
