package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	itMux "github.com/steveyegge/gastown/internal/tmux"
)

// TestRunNudgePollerInitializesSocket verifies that runNudgePoller calls
// session.InitRegistry before creating the Tmux instance. The poller runs as a
// detached background process; without InitRegistry the package-level default
// socket is never set and all tmux send-keys calls use the default socket,
// failing with "can't find window: %N" (#3761).
func TestRunNudgePollerInitializesSocket(t *testing.T) {
	// Create a minimal town directory with the required workspace marker.
	townRoot := t.TempDir()
	mayorDir := filepath.Join(townRoot, "mayor")
	if err := os.MkdirAll(mayorDir, 0755); err != nil {
		t.Fatalf("mkdir mayor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mayorDir, "town.json"), []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatalf("write town.json: %v", err)
	}

	// Save and restore the default socket so other tests are not affected.
	origSocket := itMux.GetDefaultSocket()
	itMux.SetDefaultSocket("")
	t.Cleanup(func() { itMux.SetDefaultSocket(origSocket) })

	// Run from townRoot so workspace.FindFromCwdOrError() succeeds.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(townRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	// runNudgePoller returns an error because the session doesn't exist, but
	// InitRegistry must have been called before the HasSession check.
	_ = runNudgePoller(nudgePollerCmd, []string{"nonexistent-session-xyz"})

	if got := itMux.GetDefaultSocket(); got == "" {
		t.Error("runNudgePoller did not initialize the tmux socket via session.InitRegistry")
	}
}

func TestShouldSkipDrainUntilIdle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		hasPromptDetection bool
		waitErr            error
		want               bool
	}{
		{"prompt aware idle", true, nil, false},
		{"prompt aware busy", true, errors.New("timeout"), true},
		{"no prompt detection busy", false, errors.New("timeout"), false},
		{"no prompt detection idle", false, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSkipDrainUntilIdle(tt.hasPromptDetection, tt.waitErr); got != tt.want {
				t.Errorf("shouldSkipDrainUntilIdle(%v, %v) = %v, want %v", tt.hasPromptDetection, tt.waitErr, got, tt.want)
			}
		})
	}
}
