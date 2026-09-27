package service

import (
	"sort"

	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/tracker"
)

func observationToModel(observation tracker.SourceObservation) model.ObservationState {
	return model.ObservationState{
		RunID:               observation.RunID,
		StationID:           observation.StationID,
		ObservationID:       observation.ObservationID,
		PhysicalSourceID:    observation.PhysicalSourceID,
		ParentObservationID: observation.ParentObservationID,
		Kind:                string(observation.Kind),
		Position:            observation.Position,
		Generation:          observation.Generation,
		ObservedAt:          observation.ObservedAt,
		Valid:               observation.Valid,
	}
}

func observationFromModel(state model.ObservationState) tracker.SourceObservation {
	return tracker.SourceObservation{
		RunID:               state.RunID,
		StationID:           state.StationID,
		ObservationID:       state.ObservationID,
		PhysicalSourceID:    state.PhysicalSourceID,
		ParentObservationID: state.ParentObservationID,
		Kind:                tracker.SourceKind(state.Kind),
		Position:            state.Position,
		Generation:          state.Generation,
		ObservedAt:          state.ObservedAt,
		Valid:               state.Valid,
	}
}

// degradedTrackingRuns compares the restored summaries against the rebuilt
// raw observations. A run is degraded when its observations could not be
// rebuilt, or when the snapshot carries tracking/permit summaries for it but
// no raw observations at all. Degraded runs must not reuse their previous
// passage judgments. It returns runID -> reason, sorted by run ID when
// listed.
func degradedTrackingRuns(snapshot model.ControlSnapshot, unrestorable []string) map[string]string {
	degraded := make(map[string]string)
	for _, runID := range unrestorable {
		degraded[runID] = "tracking observations could not be rebuilt from the snapshot"
	}
	withObservations := make(map[string]bool)
	for _, observation := range snapshot.Observations {
		withObservations[observation.RunID] = true
	}
	needsEvidence := make(map[string]bool)
	for _, tracking := range snapshot.Tracking {
		needsEvidence[tracking.RunID] = true
	}
	for _, permit := range snapshot.Permits {
		if permit.Granted {
			needsEvidence[permit.RunID] = true
		}
	}
	for runID := range needsEvidence {
		if _, already := degraded[runID]; already {
			continue
		}
		if !withObservations[runID] {
			degraded[runID] = "snapshot has tracking or permit summaries but no raw observations"
		}
	}
	return degraded
}

// summaryGeneration returns the highest generation the snapshot summaries
// claim for a run, so degradation records stay aligned with the run's
// tracking generation.
func summaryGeneration(snapshot model.ControlSnapshot, runID string) int64 {
	var generation int64
	for _, tracking := range snapshot.Tracking {
		if tracking.RunID == runID && tracking.Generation > generation {
			generation = tracking.Generation
		}
	}
	for _, permit := range snapshot.Permits {
		if permit.RunID == runID && permit.Generation > generation {
			generation = permit.Generation
		}
	}
	for _, observation := range snapshot.Observations {
		if observation.RunID == runID && observation.Generation > generation {
			generation = observation.Generation
		}
	}
	if generation <= 0 {
		generation = 1
	}
	return generation
}

func sortedDegradedRuns(degraded map[string]string) []string {
	out := make([]string, 0, len(degraded))
	for runID := range degraded {
		out = append(out, runID)
	}
	sort.Strings(out)
	return out
}
