package witness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/lock"
	"github.com/steveyegge/gastown/internal/workspace"
)

// handoffMu serializes in-process access to the handoff state file.
// Cross-process serialization is handled by lock.FlockAcquire on a
// sibling .flock file.
var handoffMu sync.Mutex

// beadHandoffRecord tracks how many times a single bead has triggered a polecat handoff.
type beadHandoffRecord struct {
	BeadID      string    `json:"bead_id"`
	Count       int       `json:"count"`
	LastHandoff time.Time `json:"last_handoff"`
	AlertedAt   time.Time `json:"alerted_at,omitempty"`
}

// beadHandoffState holds handoff counts for all tracked beads.
type beadHandoffState struct {
	Beads       map[string]*beadHandoffRecord `json:"beads"`
	LastUpdated time.Time                     `json:"last_updated"`
}

func beadHandoffStateFile(townRoot string) string {
	return filepath.Join(townRoot, "witness", "bead-handoff-counts.json")
}

func loadBeadHandoffState(townRoot string) *beadHandoffState {
	data, err := os.ReadFile(beadHandoffStateFile(townRoot)) //nolint:gosec // G304: path from trusted townRoot
	if err != nil {
		return &beadHandoffState{Beads: make(map[string]*beadHandoffRecord)}
	}
	var state beadHandoffState
	if err := json.Unmarshal(data, &state); err != nil {
		return &beadHandoffState{Beads: make(map[string]*beadHandoffRecord)}
	}
	if state.Beads == nil {
		state.Beads = make(map[string]*beadHandoffRecord)
	}
	return &state
}

func saveBeadHandoffState(townRoot string, state *beadHandoffState) error {
	stateFile := beadHandoffStateFile(townRoot)
	if err := os.MkdirAll(filepath.Dir(stateFile), 0755); err != nil {
		return fmt.Errorf("creating witness dir: %w", err)
	}
	state.LastUpdated = time.Now().UTC()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling handoff state: %w", err)
	}
	return os.WriteFile(stateFile, data, 0600)
}

// RecordBeadHandoff increments the handoff count for beadID and returns the new count.
// workDir is any path within the workspace; townRoot is resolved via workspace.Find.
// State file errors are non-fatal: the count is incremented in memory and returned.
func RecordBeadHandoff(workDir, beadID string) int {
	handoffMu.Lock()
	defer handoffMu.Unlock()

	townRoot, err := workspace.Find(workDir)
	if err != nil || townRoot == "" {
		townRoot = workDir
	}

	unlock, flockErr := lock.FlockAcquire(beadHandoffStateFile(townRoot) + ".flock")
	if flockErr == nil {
		defer unlock()
	}

	state := loadBeadHandoffState(townRoot)
	rec, ok := state.Beads[beadID]
	if !ok {
		rec = &beadHandoffRecord{BeadID: beadID}
		state.Beads[beadID] = rec
	}
	rec.Count++
	rec.LastHandoff = time.Now().UTC()
	_ = saveBeadHandoffState(townRoot, state)
	return rec.Count
}

// ShouldAlertHandoffSmell returns true if the handoff count has reached the
// configured threshold and no alert has been sent yet for this bead.
func ShouldAlertHandoffSmell(workDir, beadID string) bool {
	handoffMu.Lock()
	defer handoffMu.Unlock()

	townRoot, err := workspace.Find(workDir)
	if err != nil || townRoot == "" {
		townRoot = workDir
	}

	unlock, flockErr := lock.FlockAcquire(beadHandoffStateFile(townRoot) + ".flock")
	if flockErr == nil {
		defer unlock()
	}

	threshold := config.LoadOperationalConfig(townRoot).GetWitnessConfig().HandoffSmellThresholdV()

	state := loadBeadHandoffState(townRoot)
	rec, ok := state.Beads[beadID]
	if !ok {
		return false
	}
	return rec.Count >= threshold && rec.AlertedAt.IsZero()
}

// MarkHandoffSmellAlerted records that an alert was sent for beadID,
// suppressing repeat escalations until ResetBeadHandoffCount is called.
func MarkHandoffSmellAlerted(workDir, beadID string) {
	handoffMu.Lock()
	defer handoffMu.Unlock()

	townRoot, err := workspace.Find(workDir)
	if err != nil || townRoot == "" {
		townRoot = workDir
	}

	unlock, flockErr := lock.FlockAcquire(beadHandoffStateFile(townRoot) + ".flock")
	if flockErr == nil {
		defer unlock()
	}

	state := loadBeadHandoffState(townRoot)
	rec, ok := state.Beads[beadID]
	if !ok {
		return
	}
	rec.AlertedAt = time.Now().UTC()
	_ = saveBeadHandoffState(townRoot, state)
}

// ResetBeadHandoffCount clears the handoff counter and alert state for beadID.
// Use this after the bead is closed or the formula step is redesigned.
func ResetBeadHandoffCount(workDir, beadID string) error {
	handoffMu.Lock()
	defer handoffMu.Unlock()

	townRoot, err := workspace.Find(workDir)
	if err != nil || townRoot == "" {
		townRoot = workDir
	}

	unlock, flockErr := lock.FlockAcquire(beadHandoffStateFile(townRoot) + ".flock")
	if flockErr == nil {
		defer unlock()
	}

	state := loadBeadHandoffState(townRoot)
	delete(state.Beads, beadID)
	return saveBeadHandoffState(townRoot, state)
}
