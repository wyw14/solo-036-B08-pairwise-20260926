package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wyw14/solo-036/internal/tracker"
)

func launchTrackedRun(t *testing.T, r *Runtime) string {
	t.Helper()
	operation, err := r.Launch(context.Background(), LaunchRequest{
		StationID:      "station-a",
		RouteID:        "main-station-a-station-c",
		TargetPressure: 5,
	})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	runID := operation.Run.ID
	trackObservations(t, r, runID, 10, 20)
	return runID
}

func trackObservations(t *testing.T, r *Runtime, runID string, positions ...float64) {
	t.Helper()
	base := time.Now().UTC().Add(-2 * time.Minute)
	sources := []struct {
		id   string
		kind tracker.SourceKind
	}{
		{"mag-1", tracker.SourceMagnetic},
		{"acoustic-1", tracker.SourceAcoustic},
		{"pressure-1", tracker.SourcePressure},
	}
	for i, position := range positions {
		source := sources[i%len(sources)]
		if _, err := r.Track(runID, TrackingRequest{
			StationID:  "station-b",
			SourceID:   source.id,
			Kind:       source.kind,
			Position:   position,
			ObservedAt: base.Add(time.Duration(i) * 30 * time.Second),
		}); err != nil {
			t.Fatalf("track %s: %v", source.id, err)
		}
	}
}

func stripObservationsFromSnapshot(t *testing.T, directory string) {
	t.Helper()
	path := filepath.Join(directory, "snapshot.json")
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	control, ok := decoded["control"].(map[string]any)
	if !ok {
		t.Fatalf("snapshot has no control section")
	}
	delete(control, "observations")
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("encode snapshot: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o640); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
}

func TestRecoverPersistentRebuildsTrackingSources(t *testing.T) {
	directory := t.TempDir()
	first := NewRuntime()
	if err := first.ConfigurePersistence(directory); err != nil {
		t.Fatalf("configure persistence: %v", err)
	}
	runID := launchTrackedRun(t, first)
	generationBefore := first.Tracker.Generation(runID)
	if err := first.PersistSnapshot(); err != nil {
		t.Fatalf("persist snapshot: %v", err)
	}

	second := NewRuntime()
	if err := second.ConfigurePersistence(directory); err != nil {
		t.Fatalf("configure persistence: %v", err)
	}
	report, err := second.RecoverPersistent()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if len(report.DegradedRuns) != 0 {
		t.Fatalf("unexpected degraded runs: %v", report.DegradedRuns)
	}

	evidence := second.Lineage.Evidence(runID, "station-b", generationBefore)
	if len(evidence) != 2 {
		t.Fatalf("expected 2 rebuilt raw evidence points, got %d", len(evidence))
	}
	if got := second.Tracker.Generation(runID); got != generationBefore {
		t.Fatalf("tracker generation %d does not match pre-snapshot generation %d", got, generationBefore)
	}
	summaryGenerations := make(map[int64]bool)
	summaryFound := false
	for _, state := range second.Snapshot().Tracking {
		if state.RunID == runID {
			summaryFound = true
			summaryGenerations[state.Generation] = true
		}
	}
	if !summaryFound {
		t.Fatalf("tracking summary missing for %s after recovery", runID)
	}
	for _, point := range evidence {
		if !summaryGenerations[point.Generation] {
			t.Fatalf("evidence generation %d has no matching tracking summary", point.Generation)
		}
	}
	if _, err := second.GrantPassage(runID, "station-b"); err != nil {
		t.Fatalf("same run cannot verify passage evidence after recovery: %v", err)
	}
	if !second.Permits.IsOpen(runID) {
		t.Fatalf("passage permit was not granted from rebuilt evidence")
	}
}

func TestRecoverPersistentDegradesMissingEvidence(t *testing.T) {
	directory := t.TempDir()
	first := NewRuntime()
	if err := first.ConfigurePersistence(directory); err != nil {
		t.Fatalf("configure persistence: %v", err)
	}
	runID := launchTrackedRun(t, first)
	if _, err := first.GrantPassage(runID, "station-b"); err != nil {
		t.Fatalf("grant passage before snapshot: %v", err)
	}
	if err := first.PersistSnapshot(); err != nil {
		t.Fatalf("persist snapshot: %v", err)
	}
	stripObservationsFromSnapshot(t, directory)

	second := NewRuntime()
	if err := second.ConfigurePersistence(directory); err != nil {
		t.Fatalf("configure persistence: %v", err)
	}
	report, err := second.RecoverPersistent()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if len(report.DegradedRuns) != 1 || report.DegradedRuns[0] != runID {
		t.Fatalf("expected degraded run %s, got %v", runID, report.DegradedRuns)
	}
	if second.Permits.IsOpen(runID) {
		t.Fatalf("previous passage judgment was reused without evidence")
	}
	if _, err := second.GrantPassage(runID, "station-b"); err == nil || !strings.Contains(err.Error(), "degraded") {
		t.Fatalf("expected explicit degradation error, got %v", err)
	}

	snapshot := second.Snapshot()
	summaryFound := false
	for _, state := range snapshot.Tracking {
		if state.RunID == runID {
			summaryFound = true
		}
	}
	if !summaryFound {
		t.Fatalf("tracking summary should remain visible for %s", runID)
	}
	degradedRecord := false
	for _, permit := range snapshot.Permits {
		if permit.RunID == runID && !permit.Granted && strings.Contains(permit.Reason, "degraded after recovery") {
			degradedRecord = true
		}
	}
	if !degradedRecord {
		t.Fatalf("expected an explicit degraded permit record for %s", runID)
	}
	incidentFound := false
	for _, incident := range snapshot.Incidents {
		if incident.RunID == runID && strings.Contains(incident.Message, "degraded") {
			incidentFound = true
		}
	}
	if !incidentFound {
		t.Fatalf("expected a degradation incident for %s", runID)
	}

	if _, err := second.ReviseTracking(runID); err != nil {
		t.Fatalf("revise tracking: %v", err)
	}
	trackObservations(t, second, runID, 30, 40)
	if _, err := second.GrantPassage(runID, "station-b"); err != nil {
		t.Fatalf("passage should be grantable from freshly rebuilt evidence: %v", err)
	}
}
