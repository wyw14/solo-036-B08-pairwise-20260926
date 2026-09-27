package tracker

import (
	"testing"
	"time"

	"github.com/wyw14/solo-036/internal/model"
)

func TestRestoreRebuildsGenerationsAndLineage(t *testing.T) {
	base := time.Now().UTC()
	events := []model.Event{
		{ID: "e1", RunID: "run-1", Kind: "tracking", Epoch: 1, Payload: "station-b/magnetic/north/magnetic/10.500", At: base},
		{ID: "e2", RunID: "run-1", Kind: "tracking", Epoch: 1, Payload: "station-b/pressure/main/pressure/11.000", At: base.Add(time.Second)},
		{ID: "e3", RunID: "run-1", Kind: "tracking_revision", Epoch: 2, At: base.Add(2 * time.Second)},
		{ID: "e4", RunID: "run-1", Kind: "tracking", Epoch: 2, Payload: "station-b/acoustic/array/12.000", At: base.Add(3 * time.Second)},
	}
	coordinator := NewCoordinator()
	lineage := NewSourceLineage()
	restored, err := Restore(coordinator, lineage, events)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(restored) != 3 {
		t.Fatalf("restored observations = %d, want 3", len(restored))
	}
	if got := coordinator.Generation("run-1"); got != 2 {
		t.Errorf("generation = %d, want 2", got)
	}
	if got := lineage.Evidence("run-1", "station-b", 1); len(got) != 2 {
		t.Errorf("generation 1 evidence = %d, want 2", len(got))
	}
	second := lineage.Observations("run-1", "station-b", 2)
	if len(second) != 1 {
		t.Fatalf("generation 2 observations = %d, want 1", len(second))
	}
	if second[0].PhysicalSourceID != "acoustic/array" {
		t.Errorf("generation 2 source = %q, want acoustic/array", second[0].PhysicalSourceID)
	}
	if second[0].Kind != SourceMagnetic {
		t.Errorf("legacy payload kind = %q, want magnetic default", second[0].Kind)
	}
	if !lineage.HasIndependentEvidence("run-1", 1, 2) {
		t.Error("generation 1 lacks independent evidence, want magnetic + pressure")
	}
	if lineage.HasIndependentEvidence("run-1", 2, 2) {
		t.Error("generation 2 has independent evidence from a single source")
	}
}

func TestRestoreAbortsOnGenerationMismatch(t *testing.T) {
	base := time.Now().UTC()
	events := []model.Event{
		{ID: "e1", RunID: "run-1", Kind: "tracking", Epoch: 1, Payload: "station-b/magnetic/magnetic/10.500", At: base},
		{ID: "e2", RunID: "run-1", Kind: "tracking_revision", Epoch: 3, At: base.Add(time.Second)},
	}
	if _, err := Restore(NewCoordinator(), NewSourceLineage(), events); err == nil {
		t.Fatal("restore accepted a revision generation that does not match the replay")
	}
}

func TestRestoreAbortsOnMalformedPayload(t *testing.T) {
	base := time.Now().UTC()
	for _, payload := range []string{"garbage", "station-b/magnetic", "station-b/magnetic/not-a-number"} {
		events := []model.Event{
			{ID: "e1", RunID: "run-1", Kind: "tracking", Epoch: 1, Payload: payload, At: base},
		}
		if _, err := Restore(NewCoordinator(), NewSourceLineage(), events); err == nil {
			t.Errorf("restore accepted malformed payload %q", payload)
		}
	}
}
