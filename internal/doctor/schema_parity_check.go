package doctor

import (
	"fmt"
	"strings"

	"github.com/steveyegge/gastown/internal/doltserver"
)

// SchemaMigrationParityCheck verifies that all rig databases are at the same
// schema_version, and that none were provisioned outside the normal bd-init
// path (which would leave them with no config table or schema_version key).
//
// This surfaces the silent-drift scenario from GH#3770: a rig DB copied from
// a PoC or restored from an old backup can be many migrations behind. bd
// operations against that rig then fail with cryptic SQL errors ("column X
// could not be found") rather than a clear schema-version mismatch message.
type SchemaMigrationParityCheck struct {
	BaseCheck
}

// NewSchemaMigrationParityCheck creates a new schema parity check.
func NewSchemaMigrationParityCheck() *SchemaMigrationParityCheck {
	return &SchemaMigrationParityCheck{
		BaseCheck: BaseCheck{
			CheckName:        "schema-migration-parity",
			CheckDescription: "Check that all rig DBs are at the same schema_version",
			CheckCategory:    CategoryInfrastructure,
		},
	}
}

// Run checks schema_version parity across all known rig databases.
func (c *SchemaMigrationParityCheck) Run(ctx *CheckContext) *CheckResult {
	parity, err := doltserver.CheckRigsSchemaVersion(ctx.TownRoot)
	if err != nil {
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusWarning,
			Message:  fmt.Sprintf("Could not check schema versions: %v", err),
			Category: c.CheckCategory,
		}
	}

	if len(parity.Statuses) == 0 {
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusOK,
			Message:  "No rig databases found",
			Category: c.CheckCategory,
		}
	}

	// Build details table.
	var details []string
	for _, s := range parity.Statuses {
		switch {
		case s.Error != "":
			details = append(details, fmt.Sprintf("%-20s %-20s error: %s", s.RigName, s.DBName, s.Error))
		case !s.HasSchemaVersion:
			details = append(details, fmt.Sprintf("%-20s %-20s (no schema_version — uninitialized?)", s.RigName, s.DBName))
		default:
			marker := ""
			if s.SchemaVersion < parity.MaxVersion {
				marker = fmt.Sprintf(" ← behind (want %d)", parity.MaxVersion)
			}
			details = append(details, fmt.Sprintf("%-20s %-20s v%d%s", s.RigName, s.DBName, s.SchemaVersion, marker))
		}
	}

	if len(parity.UninitializedRigs) == 0 && len(parity.DriftedRigs) == 0 {
		ok := len(parity.Statuses) - len(parity.ErrorRigs)
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusOK,
			Message:  fmt.Sprintf("All %d rig DB(s) at schema_version %d", ok, parity.MaxVersion),
			Details:  details,
			Category: c.CheckCategory,
		}
	}

	var problems []string
	if len(parity.UninitializedRigs) > 0 {
		problems = append(problems,
			fmt.Sprintf("%d uninitialized: %s",
				len(parity.UninitializedRigs), strings.Join(parity.UninitializedRigs, ", ")))
	}
	if len(parity.DriftedRigs) > 0 {
		problems = append(problems,
			fmt.Sprintf("%d drifted: %s",
				len(parity.DriftedRigs), strings.Join(parity.DriftedRigs, ", ")))
	}

	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusError,
		Message: strings.Join(problems, "; "),
		Details: details,
		FixHint: "Run 'gt dolt migrate-status' for details; open each drifted rig's beads store (e.g. 'bd list --rig <name>') to trigger auto-migration",
		Category: c.CheckCategory,
	}
}
