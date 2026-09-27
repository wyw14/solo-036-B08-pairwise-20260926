package tracker

import (
	"testing"
	"time"
)

func TestRestoreAlignsGenerations(t *testing.T) {
	lineage := NewSourceLineage()
	coordinator := NewCoordinator()
	base := time.Now().UTC()
	observations := []SourceObservation{
		{RunID: "run-1", StationID: "station-b", ObservationID: "obs-1", PhysicalSourceID: "mag-1", Kind: SourceMagnetic, Position: 10, Generation: 1, ObservedAt: base, Valid: true},
		{RunID: "run-1", StationID: "station-b", ObservationID: "obs-2", PhysicalSourceID: "mag-1", Kind: SourceMagnetic, Position: 20, Generation: 2, ObservedAt: base.Add(time.Minute), Valid: true},
		{RunID: "run-1", StationID: "station-b", ObservationID: "obs-3", PhysicalSourceID: "acoustic-1", Kind: SourceAcoustic, Position: 30, Generation: 2, ObservedAt: base.Add(2 * time.Minute), Valid: true},
	}
	if degraded := Restore(lineage, coordinator, observations); len(degraded) != 0 {
		t.Fatalf("unexpected degraded runs: %v", degraded)
	}
	if got := coordinator.Generation("run-1"); got != 2 {
		t.Fatalf("coordinator generation = %d, want 2", got)
	}
	points := coordinator.Evidence("run-1", "station-b")
	if len(points) != 2 {
		t.Fatalf("coordinator evidence = %d points, want 2 from the current generation", len(points))
	}
	for _, point := range points {
		if point.Generation != 2 {
			t.Fatalf("coordinator carried stale generation %d evidence", point.Generation)
		}
	}
	if got := lineage.Observations("run-1", "station-b", 1); len(got) != 1 {
		t.Fatalf("lineage lost earlier generation: %d observations", len(got))
	}
	if got := lineage.Observations("run-1", "station-b", 2); len(got) != 2 {
		t.Fatalf("lineage current generation = %d observations, want 2", len(got))
	}
}

func TestRestoreDegradesBrokenParentChain(t *testing.T) {
	lineage := NewSourceLineage()
	coordinator := NewCoordinator()
	base := time.Now().UTC()
	observations := []SourceObservation{
		{RunID: "run-1", StationID: "station-b", ObservationID: "obs-1", PhysicalSourceID: "mag-1", Kind: SourceMagnetic, Position: 10, Generation: 1, ObservedAt: base, Valid: true},
		{RunID: "run-1", StationID: "station-b", ObservationID: "obs-2", PhysicalSourceID: "mag-1", ParentObservationID: "missing", Kind: SourceDerivedPosition, Position: 11, Generation: 1, ObservedAt: base.Add(time.Minute), Valid: true},
		{RunID: "run-2", StationID: "station-b", ObservationID: "obs-3", PhysicalSourceID: "mag-9", Kind: SourceMagnetic, Position: 12, Generation: 1, ObservedAt: base.Add(2 * time.Minute), Valid: true},
	}
	degraded := Restore(lineage, coordinator, observations)
	if len(degraded) != 1 || degraded[0] != "run-1" {
		t.Fatalf("degraded runs = %v, want [run-1]", degraded)
	}
	if got := lineage.Observations("run-2", "station-b", 1); len(got) != 1 {
		t.Fatalf("unrelated run was not rebuilt: %d observations", len(got))
	}
}

func TestRestoreRebuildsOutOfOrderDerivations(t *testing.T) {
	lineage := NewSourceLineage()
	coordinator := NewCoordinator()
	base := time.Now().UTC()
	observations := []SourceObservation{
		{RunID: "run-1", StationID: "station-b", ObservationID: "aaa-parent", PhysicalSourceID: "mag-1", Kind: SourceMagnetic, Position: 10, Generation: 1, ObservedAt: base, Valid: true},
		{RunID: "run-1", StationID: "station-b", ObservationID: "zzz-child", PhysicalSourceID: "mag-1", ParentObservationID: "aaa-parent", Kind: SourceDerivedPosition, Position: 11, Generation: 1, ObservedAt: base, Valid: true},
	}
	if degraded := Restore(lineage, coordinator, observations); len(degraded) != 0 {
		t.Fatalf("out-of-order derivation should rebuild, degraded: %v", degraded)
	}
	if got := lineage.Observations("run-1", "station-b", 1); len(got) != 2 {
		t.Fatalf("lineage = %d observations, want 2", len(got))
	}
}
