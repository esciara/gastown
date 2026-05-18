package tmux

import (
	"os"
	"testing"
)

func preserveSocketEnv(t *testing.T) {
	t.Helper()
	orig := defaultSocket
	origTownSocket, hadTownSocket := os.LookupEnv("GT_TOWN_SOCKET")
	origTmuxSocket, hadTmuxSocket := os.LookupEnv("GT_TMUX_SOCKET")
	t.Cleanup(func() {
		defaultSocket = orig
		restoreEnv("GT_TOWN_SOCKET", origTownSocket, hadTownSocket)
		restoreEnv("GT_TMUX_SOCKET", origTmuxSocket, hadTmuxSocket)
	})
}

func restoreEnv(key, value string, ok bool) {
	if ok {
		_ = os.Setenv(key, value)
		return
	}
	_ = os.Unsetenv(key)
}

func TestSetGetDefaultSocket(t *testing.T) {
	preserveSocketEnv(t)

	// Initially empty
	SetDefaultSocket("")
	if got := GetDefaultSocket(); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	SetDefaultSocket("mytown")
	if got := GetDefaultSocket(); got != "mytown" {
		t.Errorf("expected %q, got %q", "mytown", got)
	}
}

func TestNewTmuxInheritsSocket(t *testing.T) {
	preserveSocketEnv(t)

	SetDefaultSocket("testtown")
	tmx := NewTmux()
	if tmx.socketName != "testtown" {
		t.Errorf("NewTmux() socketName = %q, want %q", tmx.socketName, "testtown")
	}
}

func TestNewTmuxUsesGTTownSocketFallback(t *testing.T) {
	preserveSocketEnv(t)

	SetDefaultSocket("")
	_ = os.Setenv("GT_TOWN_SOCKET", "gastown-test-afa2e3")
	_ = os.Unsetenv("GT_TMUX_SOCKET")

	tmx := NewTmux()
	if tmx.socketName != "gastown-test-afa2e3" {
		t.Errorf("NewTmux() socketName = %q, want %q", tmx.socketName, "gastown-test-afa2e3")
	}
}

func TestNewTmuxUsesExplicitGTTmuxSocketFallback(t *testing.T) {
	preserveSocketEnv(t)

	SetDefaultSocket("")
	_ = os.Unsetenv("GT_TOWN_SOCKET")
	_ = os.Setenv("GT_TMUX_SOCKET", "gastown-test-afa2e3")

	tmx := NewTmux()
	if tmx.socketName != "gastown-test-afa2e3" {
		t.Errorf("NewTmux() socketName = %q, want %q", tmx.socketName, "gastown-test-afa2e3")
	}
}

func TestNewTmuxIgnoresAutoGTTmuxSocketFallback(t *testing.T) {
	preserveSocketEnv(t)

	SetDefaultSocket("")
	_ = os.Unsetenv("GT_TOWN_SOCKET")
	_ = os.Setenv("GT_TMUX_SOCKET", "auto")

	tmx := NewTmux()
	if tmx.socketName != "" {
		t.Errorf("NewTmux() socketName = %q, want empty for auto fallback", tmx.socketName)
	}
}

func TestNewTmuxIgnoresDefaultGTTmuxSocketFallback(t *testing.T) {
	preserveSocketEnv(t)

	SetDefaultSocket("")
	_ = os.Unsetenv("GT_TOWN_SOCKET")
	_ = os.Setenv("GT_TMUX_SOCKET", "default")

	tmx := NewTmux()
	if tmx.socketName != "" {
		t.Errorf("NewTmux() socketName = %q, want empty for default fallback", tmx.socketName)
	}
}

func TestBuildCommandUsesExplicitGTTmuxSocketFallback(t *testing.T) {
	preserveSocketEnv(t)

	SetDefaultSocket("")
	_ = os.Unsetenv("GT_TOWN_SOCKET")
	_ = os.Setenv("GT_TMUX_SOCKET", "gastown-test-afa2e3")

	cmd := BuildCommand("has-session", "-t", "gt-crew-auction_watcher")
	expected := []string{"tmux", "-u", "-L", "gastown-test-afa2e3", "has-session", "-t", "gt-crew-auction_watcher"}
	if len(cmd.Args) != len(expected) {
		t.Fatalf("args = %v, want %v", cmd.Args, expected)
	}
	for i, a := range cmd.Args {
		if a != expected[i] {
			t.Errorf("args[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestNewTmuxWithSocket(t *testing.T) {
	tmx := NewTmuxWithSocket("custom")
	if tmx.socketName != "custom" {
		t.Errorf("NewTmuxWithSocket() socketName = %q, want %q", tmx.socketName, "custom")
	}
}

func TestBuildCommandNoSocket(t *testing.T) {
	preserveSocketEnv(t)

	SetDefaultSocket("")
	_ = os.Unsetenv("GT_TOWN_SOCKET")
	_ = os.Unsetenv("GT_TMUX_SOCKET")
	cmd := BuildCommand("list-sessions")
	args := cmd.Args
	// Should be: tmux -u list-sessions
	expected := []string{"tmux", "-u", "list-sessions"}
	if len(args) != len(expected) {
		t.Fatalf("args = %v, want %v", args, expected)
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("args[%d] = %q, want %q", i, a, expected[i])
		}
	}
}

func TestBuildCommandWithSocket(t *testing.T) {
	preserveSocketEnv(t)

	SetDefaultSocket("mytown")
	cmd := BuildCommand("has-session", "-t", "hq-mayor")
	args := cmd.Args
	// Should be: tmux -u -L mytown has-session -t hq-mayor
	expected := []string{"tmux", "-u", "-L", "mytown", "has-session", "-t", "hq-mayor"}
	if len(args) != len(expected) {
		t.Fatalf("args = %v, want %v", args, expected)
	}
	for i, a := range args {
		if a != expected[i] {
			t.Errorf("args[%d] = %q, want %q", i, a, expected[i])
		}
	}
}
