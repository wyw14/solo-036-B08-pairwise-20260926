package tracker

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wyw14/solo-036/internal/model"
)

// Restore rebuilds the coordinator and the source lineage from durable
// tracking events so a recovered run keeps its generation and its raw
// observations instead of restarting at generation one with an empty
// lineage. Events must be replayed in journal order; a generation mismatch
// between the replayed state and the recorded event aborts the restore
// rather than silently misaligning the run. It returns the observations
// that were rebuilt so callers can reconcile tracking summaries against
// the raw evidence.
func Restore(c *Coordinator, lineage *SourceLineage, events []model.Event) ([]SourceObservation, error) {
	restored := make([]SourceObservation, 0)
	for index, event := range events {
		switch event.Kind {
		case "tracking_revision":
			generation := c.Revise(event.RunID)
			if event.Epoch > 0 && generation != event.Epoch {
				return restored, fmt.Errorf("tracking revision for run %s restored generation %d, journal recorded %d", event.RunID, generation, event.Epoch)
			}
		case "tracking":
			observation, err := observationFromEvent(event, index)
			if err != nil {
				return restored, err
			}
			evidence := c.Observe(event.RunID, observation.StationID, observation.PhysicalSourceID, observation.Position, observation.ObservedAt)
			if event.Epoch > 0 && evidence.Generation != event.Epoch {
				return restored, fmt.Errorf("tracking event for run %s restored generation %d, journal recorded %d", event.RunID, evidence.Generation, event.Epoch)
			}
			observation.Generation = evidence.Generation
			if err := lineage.Add(observation); err != nil {
				return restored, fmt.Errorf("restore observation for run %s: %w", event.RunID, err)
			}
			restored = append(restored, observation)
		}
	}
	return restored, nil
}

// observationFromEvent parses the tracking payload written by the service:
// "station/source/kind/position", or "station/source/position" in journals
// written before the kind was recorded. Source ids may contain slashes, so
// the station is the first field, the position the last, and the kind —
// when present and valid — the second to last. A missing kind defaults to
// magnetic, mirroring the default the live tracking path applies.
func observationFromEvent(event model.Event, index int) (SourceObservation, error) {
	fields := strings.Split(event.Payload, "/")
	if len(fields) < 3 {
		return SourceObservation{}, fmt.Errorf("tracking event for run %s has malformed payload %q", event.RunID, event.Payload)
	}
	position, err := strconv.ParseFloat(fields[len(fields)-1], 64)
	if err != nil {
		return SourceObservation{}, fmt.Errorf("tracking event for run %s has invalid position: %w", event.RunID, err)
	}
	kind := SourceMagnetic
	sourceFields := fields[1 : len(fields)-1]
	if candidate := SourceKind(fields[len(fields)-2]); len(fields) >= 4 && validSourceKind(candidate) {
		kind = candidate
		sourceFields = fields[1 : len(fields)-2]
	}
	observationID := strings.TrimSpace(event.ID)
	if observationID == "" {
		observationID = fmt.Sprintf("%s/restored/%d", event.RunID, index)
	}
	return SourceObservation{
		RunID:            event.RunID,
		StationID:        fields[0],
		ObservationID:    observationID,
		PhysicalSourceID: strings.Join(sourceFields, "/"),
		Kind:             kind,
		Position:         position,
		ObservedAt:       event.At,
		Valid:            true,
	}, nil
}

func validSourceKind(kind SourceKind) bool {
	switch kind {
	case SourcePressure, SourceAcoustic, SourceMagnetic, SourceDerivedPosition:
		return true
	default:
		return false
	}
}
