package service

import (
	"strings"
	"testing"
	"time"

	"github.com/wyw14/solo-036/internal/journal"
	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/tracker"
)

func trackOperation(t *testing.T, r *Runtime, runID, station, source string, kind tracker.SourceKind, position float64, at time.Time) {
	t.Helper()
	if _, err := r.Track(runID, TrackingRequest{StationID: station, SourceID: source, Kind: kind, Position: position, ObservedAt: at}); err != nil {
		t.Fatalf("track %s: %v", source, err)
	}
}

func TestRecoverPersistentRebuildsTrackingSources(t *testing.T) {
	directory := t.TempDir()
	original := NewRuntime()
	if err := original.ConfigurePersistence(directory); err != nil {
		t.Fatalf("configure persistence: %v", err)
	}
	created, err := original.CreateOperation("station-a")
	if err != nil {
		t.Fatalf("create operation: %v", err)
	}
	runID := created.ID
	if _, err := original.Routes.Reserve("main-station-a-station-c", runID, created.Epoch); err != nil {
		t.Fatalf("reserve route: %v", err)
	}
	base := time.Now().UTC()
	trackOperation(t, original, runID, "station-b", "magnetic/north", tracker.SourceMagnetic, 12.5, base)
	trackOperation(t, original, runID, "station-b", "pressure/main", tracker.SourcePressure, 12.6, base.Add(5*time.Second))
	if _, err := original.GrantPassage(runID, "station-b"); err != nil {
		t.Fatalf("grant passage before snapshot: %v", err)
	}
	generation, err := original.ReviseTracking(runID)
	if err != nil {
		t.Fatalf("revise tracking: %v", err)
	}
	if generation != 2 {
		t.Fatalf("generation after revise = %d, want 2", generation)
	}
	trackOperation(t, original, runID, "station-b", "acoustic/array", tracker.SourceAcoustic, 13.0, base.Add(10*time.Second))
	if err := original.PersistSnapshot(); err != nil {
		t.Fatalf("persist snapshot: %v", err)
	}

	recovered := NewRuntime()
	if err := recovered.ConfigurePersistence(directory); err != nil {
		t.Fatalf("configure recovery persistence: %v", err)
	}
	report, err := recovered.RecoverPersistent()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if report.RecoveredTracking != 3 {
		t.Errorf("recovered tracking = %d, want 3", report.RecoveredTracking)
	}
	if report.RecoveredPermits != 1 || report.DegradedPermits != 0 || report.DegradedTracking != 0 {
		t.Errorf("unexpected report: %+v", report)
	}
	if got := recovered.Tracker.Generation(runID); got != 2 {
		t.Errorf("recovered generation = %d, want 2", got)
	}
	first := recovered.Lineage.Evidence(runID, "station-b", 1)
	if len(first) != 2 {
		t.Fatalf("generation 1 evidence = %d, want 2", len(first))
	}
	sources := map[string]bool{}
	for _, evidence := range first {
		sources[evidence.SourceID] = true
	}
	if !sources["magnetic/north"] || !sources["pressure/main"] {
		t.Errorf("generation 1 sources = %v, want magnetic/north and pressure/main", sources)
	}
	if got := recovered.Lineage.Evidence(runID, "station-b", 2); len(got) != 1 {
		t.Errorf("generation 2 evidence = %d, want 1", len(got))
	}
	if !recovered.Permits.IsOpen(runID) {
		t.Error("recovered permit evaluator is closed, want open")
	}
	snapshot := recovered.Snapshot()
	tracking := 0
	for _, state := range snapshot.Tracking {
		if state.RunID == runID {
			tracking++
			if !state.Online {
				t.Errorf("tracking summary %s/%s is offline after full recovery", state.StationID, state.SourceID)
			}
		}
	}
	if tracking != 3 {
		t.Errorf("recovered tracking summaries = %d, want 3", tracking)
	}
	granted := false
	for _, permit := range snapshot.Permits {
		if permit.RunID == runID && permit.Granted {
			granted = true
		}
	}
	if !granted {
		t.Error("recovered permit is not granted")
	}

	// The recovered run must keep accepting observations in the restored
	// generation and must be able to verify passage evidence again.
	if _, err := recovered.Routes.Reserve("main-station-a-station-c", runID, created.Epoch); err != nil {
		t.Fatalf("reserve recovered route: %v", err)
	}
	trackOperation(t, recovered, runID, "station-b", "magnetic/east", tracker.SourceMagnetic, 13.5, base.Add(15*time.Second))
	if got := recovered.Tracker.Generation(runID); got != 2 {
		t.Errorf("generation after recovered track = %d, want 2", got)
	}
	if got := recovered.Lineage.Evidence(runID, "station-b", 2); len(got) != 2 {
		t.Fatalf("generation 2 evidence after recovered track = %d, want 2", len(got))
	}
	if _, err := recovered.GrantPassage(runID, "station-b"); err != nil {
		t.Errorf("grant passage after recovery: %v", err)
	}
}

func TestRecoverPersistentDegradesMissingEvidence(t *testing.T) {
	directory := t.TempDir()
	store, err := journal.NewFileStore(directory)
	if err != nil {
		t.Fatalf("new file store: %v", err)
	}
	now := time.Now().UTC()
	if err := store.Append(model.Event{ID: "seed-1", RunID: "run-1", Kind: "state", Epoch: 1, Payload: "loaded", At: now}); err != nil {
		t.Fatalf("append seed event: %v", err)
	}
	control := model.ControlSnapshot{
		Revision:   "rev-1",
		CapturedAt: now,
		Stations: []model.Station{
			{ID: "station-a", Name: "Launcher", Role: model.StationLauncher, Kilometer: 0, PressureLimit: 10, Enabled: true},
			{ID: "station-b", Name: "Receiver", Role: model.StationReceiver, Kilometer: 10, PressureLimit: 10, Enabled: true},
		},
		Tracking: []model.TrackingState{
			{RunID: "run-1", StationID: "station-a", SourceID: "magnetic/north", Generation: 1, Position: 5, Online: true, ObservedAt: now},
		},
		Permits: []model.PermitState{
			{RunID: "run-1", Name: "repressurization", Generation: 1, Granted: true, Reason: "continuous durable passage evidence", UpdatedAt: now},
		},
		Runs: []model.Run{
			{ID: "run-1", Station: "station-a", State: model.StateTracking, Epoch: 2, CreatedAt: now, UpdatedAt: now},
		},
	}
	if err := store.WriteSnapshot(control); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	recovered := NewRuntime()
	if err := recovered.ConfigurePersistence(directory); err != nil {
		t.Fatalf("configure recovery persistence: %v", err)
	}
	report, err := recovered.RecoverPersistent()
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if report.DegradedPermits != 1 || report.RecoveredPermits != 0 {
		t.Errorf("permit counts = %d recovered, %d degraded; want 0 recovered, 1 degraded", report.RecoveredPermits, report.DegradedPermits)
	}
	if report.DegradedTracking != 1 {
		t.Errorf("degraded tracking = %d, want 1", report.DegradedTracking)
	}
	if recovered.Permits.IsOpen("run-1") {
		t.Error("permit evaluator is open despite missing evidence")
	}
	snapshot := recovered.Snapshot()
	if len(snapshot.Permits) != 1 {
		t.Fatalf("recovered permits = %d, want 1", len(snapshot.Permits))
	}
	permit := snapshot.Permits[0]
	if permit.Granted {
		t.Error("permit is still granted despite missing evidence")
	}
	if !strings.Contains(permit.Reason, "degraded") {
		t.Errorf("permit reason %q does not explain the degradation", permit.Reason)
	}
	if len(snapshot.Tracking) != 1 || snapshot.Tracking[0].Online {
		t.Errorf("tracking summary = %+v, want one offline entry", snapshot.Tracking)
	}
	if len(snapshot.Incidents) < 2 {
		t.Errorf("incidents = %d, want at least 2 degradation incidents", len(snapshot.Incidents))
	}
	if _, err := recovered.GrantPassage("run-1", "station-a"); err == nil {
		t.Error("grant passage succeeded without durable evidence")
	}
}
