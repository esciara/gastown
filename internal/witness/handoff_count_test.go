package witness

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/steveyegge/gastown/internal/config"
)

func TestRecordBeadHandoff_Increments(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "witness"), 0755); err != nil {
		t.Fatal(err)
	}

	count := RecordBeadHandoff(tmpDir, "bead-1")
	if count != 1 {
		t.Errorf("first RecordBeadHandoff = %d, want 1", count)
	}

	count = RecordBeadHandoff(tmpDir, "bead-1")
	if count != 2 {
		t.Errorf("second RecordBeadHandoff = %d, want 2", count)
	}
}

func TestRecordBeadHandoff_IndependentBeads(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "witness"), 0755); err != nil {
		t.Fatal(err)
	}

	RecordBeadHandoff(tmpDir, "bead-a")
	RecordBeadHandoff(tmpDir, "bead-a")

	count := RecordBeadHandoff(tmpDir, "bead-b")
	if count != 1 {
		t.Errorf("bead-b count = %d, want 1 (independent from bead-a)", count)
	}
}

func TestShouldAlertHandoffSmell_BelowThreshold(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "witness"), 0755); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < config.DefaultWitnessHandoffSmellThreshold-1; i++ {
		RecordBeadHandoff(tmpDir, "bead-2")
	}
	if ShouldAlertHandoffSmell(tmpDir, "bead-2") {
		t.Error("ShouldAlertHandoffSmell = true before reaching threshold")
	}
}

func TestShouldAlertHandoffSmell_AtThreshold(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "witness"), 0755); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < config.DefaultWitnessHandoffSmellThreshold; i++ {
		RecordBeadHandoff(tmpDir, "bead-3")
	}
	if !ShouldAlertHandoffSmell(tmpDir, "bead-3") {
		t.Error("ShouldAlertHandoffSmell = false at threshold")
	}
}

func TestShouldAlertHandoffSmell_SuppressedAfterMark(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "witness"), 0755); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < config.DefaultWitnessHandoffSmellThreshold; i++ {
		RecordBeadHandoff(tmpDir, "bead-4")
	}
	if !ShouldAlertHandoffSmell(tmpDir, "bead-4") {
		t.Fatal("expected alert before marking")
	}

	MarkHandoffSmellAlerted(tmpDir, "bead-4")

	// Further handoffs should not trigger another alert.
	RecordBeadHandoff(tmpDir, "bead-4")
	if ShouldAlertHandoffSmell(tmpDir, "bead-4") {
		t.Error("ShouldAlertHandoffSmell = true after MarkHandoffSmellAlerted")
	}
}

func TestResetBeadHandoffCount(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "witness"), 0755); err != nil {
		t.Fatal(err)
	}

	RecordBeadHandoff(tmpDir, "bead-5")
	RecordBeadHandoff(tmpDir, "bead-5")
	MarkHandoffSmellAlerted(tmpDir, "bead-5")

	if err := ResetBeadHandoffCount(tmpDir, "bead-5"); err != nil {
		t.Fatalf("ResetBeadHandoffCount error: %v", err)
	}

	// After reset, alert should not trigger.
	if ShouldAlertHandoffSmell(tmpDir, "bead-5") {
		t.Error("ShouldAlertHandoffSmell = true after reset")
	}

	// Re-increment should start from 1.
	count := RecordBeadHandoff(tmpDir, "bead-5")
	if count != 1 {
		t.Errorf("RecordBeadHandoff after reset = %d, want 1", count)
	}
}

func TestShouldAlertHandoffSmell_UnknownBead(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "witness"), 0755); err != nil {
		t.Fatal(err)
	}

	if ShouldAlertHandoffSmell(tmpDir, "nonexistent") {
		t.Error("ShouldAlertHandoffSmell = true for unknown bead")
	}
}

func TestRecordBeadHandoff_ConcurrentSafe(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "witness"), 0755); err != nil {
		t.Fatal(err)
	}

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			RecordBeadHandoff(tmpDir, "bead-race")
		}()
	}
	wg.Wait()

	state := loadBeadHandoffState(tmpDir)
	rec, ok := state.Beads["bead-race"]
	if !ok {
		t.Fatal("bead-race record not found")
	}
	if rec.Count != goroutines {
		t.Errorf("concurrent count = %d, want %d", rec.Count, goroutines)
	}
}
