package doltserver

import (
	"strings"
	"testing"
)

func TestSchemaMigrationsParity_IsHealthy(t *testing.T) {
	tests := []struct {
		name    string
		parity  SchemaMigrationsParity
		healthy bool
	}{
		{
			name: "all databases healthy",
			parity: SchemaMigrationsParity{
				ExpectedVersion: 32,
				Statuses: []RigSchemaStatus{
					{Name: "gastown", Version: 32},
					{Name: "hq", Version: 32},
				},
			},
			healthy: true,
		},
		{
			name: "one database missing table",
			parity: SchemaMigrationsParity{
				ExpectedVersion: 32,
				Missing:         []string{"datamask"},
			},
			healthy: false,
		},
		{
			name: "one database lagging",
			parity: SchemaMigrationsParity{
				ExpectedVersion: 32,
				Lagging:         []string{"old-rig"},
			},
			healthy: false,
		},
		{
			name:    "empty workspace",
			parity:  SchemaMigrationsParity{},
			healthy: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.parity.IsHealthy(); got != tt.healthy {
				t.Errorf("IsHealthy() = %v, want %v", got, tt.healthy)
			}
		})
	}
}

func TestSchemaMigrationsParity_WarningMessage(t *testing.T) {
	parity := SchemaMigrationsParity{
		ExpectedVersion: 32,
		Statuses: []RigSchemaStatus{
			{Name: "datamask", Version: 0, Missing: true},
			{Name: "gastown", Version: 25},
			{Name: "hq", Version: 32},
		},
		Missing: []string{"datamask"},
		Lagging: []string{"gastown"},
	}

	msg := parity.WarningMessage()
	if !strings.Contains(msg, "32") {
		t.Error("warning message should contain expected version 32")
	}
	if !strings.Contains(msg, "datamask") {
		t.Error("warning message should mention missing database")
	}
	if !strings.Contains(msg, "gastown") {
		t.Error("warning message should mention lagging database")
	}
	if !strings.Contains(msg, "gt dolt migrate") {
		t.Error("warning message should include actionable remediation command")
	}
	if !strings.Contains(msg, "gt dolt migrate-status") {
		t.Error("warning message should reference migrate-status command")
	}
}

func TestParseCSVSingleInt(t *testing.T) {
	tests := []struct {
		name   string
		input  []byte
		expect int
	}{
		{
			name:   "standard CSV with header",
			input:  []byte("COALESCE(MAX(version), 0)\n32\n"),
			expect: 32,
		},
		{
			name:   "zero value",
			input:  []byte("COUNT(*)\n0\n"),
			expect: 0,
		},
		{
			name:   "single line no header",
			input:  []byte("5"),
			expect: 0, // needs at least 2 lines
		},
		{
			name:   "empty output",
			input:  []byte(""),
			expect: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCSVSingleInt(tt.input)
			if got != tt.expect {
				t.Errorf("parseCSVSingleInt(%q) = %d, want %d", tt.input, got, tt.expect)
			}
		})
	}
}

func TestCheckSchemaMigrationsParity_NoServer(t *testing.T) {
	// When no Dolt server is running, all DB queries fail and are skipped.
	// The result should be an empty but healthy parity (no databases queryable).
	townRoot := t.TempDir()
	// Write a dummy config.yaml with an unused port so the server is unreachable.
	// ListDatabases on an empty townRoot returns no databases, so CheckSchemaMigrationsParity
	// returns an empty-but-healthy parity without hitting the server.
	parity, err := CheckSchemaMigrationsParity(townRoot)
	if err != nil {
		t.Fatalf("CheckSchemaMigrationsParity: unexpected error: %v", err)
	}
	if !parity.IsHealthy() {
		t.Errorf("empty workspace should report healthy; got missing=%v lagging=%v",
			parity.Missing, parity.Lagging)
	}
}
