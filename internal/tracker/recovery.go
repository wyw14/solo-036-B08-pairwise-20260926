package tracker

import (
	"sort"

	"github.com/wyw14/solo-036/internal/model"
)

// Restore rebuilds the source lineage and the tracking coordinator from
// observations persisted with a control snapshot, so that a recovered run
// keeps its raw evidence, its generation, and its summaries aligned.
//
// Observations are replayed in a deterministic order; derived observations
// whose parent chain cannot be rebuilt (or that fail lineage validation)
// degrade their run. A degraded run keeps whatever evidence could be rebuilt
// for inspection, but the caller must not reuse its previous passage
// judgments. Restore returns the sorted set of degraded run IDs.
func Restore(lineage *SourceLineage, coordinator *Coordinator, observations []SourceObservation) []string {
	ordered := append([]SourceObservation(nil), observations...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].ObservedAt.Equal(ordered[j].ObservedAt) {
			return ordered[i].ObservationID < ordered[j].ObservationID
		}
		return ordered[i].ObservedAt.Before(ordered[j].ObservedAt)
	})
	current := make(map[string]int64)
	for _, observation := range ordered {
		if observation.Generation > current[observation.RunID] {
			current[observation.RunID] = observation.Generation
		}
	}
	degraded := make(map[string]bool)
	pending := ordered
	for len(pending) > 0 {
		next := make([]SourceObservation, 0, len(pending))
		progressed := false
		for _, observation := range pending {
			if err := lineage.Add(observation); err != nil {
				next = append(next, observation)
				continue
			}
			progressed = true
			coordinator.replay(observation, current[observation.RunID])
		}
		if !progressed {
			for _, observation := range next {
				degraded[observation.RunID] = true
			}
			break
		}
		pending = next
	}
	out := make([]string, 0, len(degraded))
	for runID := range degraded {
		out = append(out, runID)
	}
	sort.Strings(out)
	return out
}

// replay folds one rebuilt observation into the coordinator. Only valid
// observations from the run's current generation become evidence points,
// matching the live behavior where Revise discards earlier generations.
func (c *Coordinator) replay(observation SourceObservation, generation int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation > c.generation[observation.RunID] {
		c.generation[observation.RunID] = generation
	}
	if !observation.Valid || observation.Generation != generation {
		return
	}
	key := observation.RunID + "/" + observation.StationID
	c.points[key] = append(c.points[key], model.PassageEvidence{
		RunID:      observation.RunID,
		Station:    observation.StationID,
		SourceID:   observation.PhysicalSourceID,
		Generation: observation.Generation,
		Position:   observation.Position,
		At:         observation.ObservedAt,
		Valid:      true,
	})
	if c.sources[key] == nil {
		c.sources[key] = make(map[string]bool)
	}
	c.sources[key][observation.PhysicalSourceID] = true
}
