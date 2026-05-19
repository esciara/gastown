package doltserver

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RigSchemaStatus records the schema_migrations state for a single rig database.
type RigSchemaStatus struct {
	// Name is the rig database name.
	Name string
	// Version is MAX(version) from schema_migrations, or 0 if the table exists
	// but is empty.
	Version int
	// Missing is true when the schema_migrations table does not exist in this database.
	Missing bool
}

// SchemaMigrationsParity holds the cross-rig parity check result.
type SchemaMigrationsParity struct {
	// Statuses contains the schema state for each database, sorted by name.
	Statuses []RigSchemaStatus
	// ExpectedVersion is the highest version seen across all databases that have
	// schema_migrations. Used as the parity baseline.
	ExpectedVersion int
	// Lagging contains database names that have the table but are behind ExpectedVersion.
	Lagging []string
	// Missing contains database names that are missing the schema_migrations table.
	Missing []string
}

// IsHealthy returns true when all rig databases are at the expected schema version.
func (p *SchemaMigrationsParity) IsHealthy() bool {
	return len(p.Missing) == 0 && len(p.Lagging) == 0
}

// WarningMessage returns a human-readable, actionable summary of parity problems.
func (p *SchemaMigrationsParity) WarningMessage() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("schema_migrations parity: expected version %d, found problems:\n", p.ExpectedVersion))
	for _, name := range p.Missing {
		b.WriteString(fmt.Sprintf("  %-24s  (no schema_migrations table)\n", name))
	}
	for _, name := range p.Lagging {
		for _, s := range p.Statuses {
			if s.Name == name {
				b.WriteString(fmt.Sprintf("  %-24s  version %d (behind by %d)\n",
					name, s.Version, p.ExpectedVersion-s.Version))
				break
			}
		}
	}
	b.WriteString("\nTo apply missing migrations run: gt dolt migrate <rig>\n")
	b.WriteString("To park a rig that is being abandoned run: gt rig park <rig>\n")
	b.WriteString("To inspect current state run: gt dolt migrate-status")
	return b.String()
}

// CheckSchemaMigrationsParity queries the running Dolt server for the
// schema_migrations max version in each registered rig database and returns
// a parity report. Databases that cannot be queried (e.g., server unreachable)
// are silently skipped — they are already flagged by the Dolt health check.
func CheckSchemaMigrationsParity(townRoot string) (*SchemaMigrationsParity, error) {
	databases, err := ListDatabases(townRoot)
	if err != nil {
		return nil, fmt.Errorf("listing databases: %w", err)
	}

	config := DefaultConfig(townRoot)
	parity := &SchemaMigrationsParity{}

	for _, db := range databases {
		ver, missing, queryErr := querySchemaMigrationsMaxVersion(config, db)
		if queryErr != nil {
			// Skip — other health checks surface connectivity issues.
			continue
		}
		parity.Statuses = append(parity.Statuses, RigSchemaStatus{
			Name:    db,
			Version: ver,
			Missing: missing,
		})
	}

	sort.Slice(parity.Statuses, func(i, j int) bool {
		return parity.Statuses[i].Name < parity.Statuses[j].Name
	})

	// Derive the expected version as the maximum across databases that have the table.
	for _, s := range parity.Statuses {
		if !s.Missing && s.Version > parity.ExpectedVersion {
			parity.ExpectedVersion = s.Version
		}
	}

	for _, s := range parity.Statuses {
		switch {
		case s.Missing:
			parity.Missing = append(parity.Missing, s.Name)
		case s.Version < parity.ExpectedVersion:
			parity.Lagging = append(parity.Lagging, s.Name)
		}
	}

	return parity, nil
}

// querySchemaMigrationsMaxVersion returns (version, missing, error) for a rig DB.
// missing is true when the schema_migrations table does not exist.
func querySchemaMigrationsMaxVersion(config *Config, db string) (version int, missing bool, err error) {
	if !validSQLName(db) {
		return 0, false, fmt.Errorf("invalid database name %q", db)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use information_schema to check table existence without risking a
	// confusing error when the table is absent.
	existsQuery := fmt.Sprintf(
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='%s' AND table_name='schema_migrations'",
		db,
	)
	cmd := buildDoltSQLCmd(ctx, config, "-r", "csv", "-q", existsQuery)
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return 0, false, fmt.Errorf("checking schema_migrations existence for %s: %w", db, runErr)
	}
	if parseCSVSingleInt(out) == 0 {
		return 0, true, nil
	}

	// Table exists — query max version.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()

	verQuery := fmt.Sprintf("SELECT COALESCE(MAX(version), 0) FROM `%s`.schema_migrations", db)
	cmd2 := buildDoltSQLCmd(ctx2, config, "-r", "csv", "-q", verQuery)
	out2, runErr2 := cmd2.CombinedOutput()
	if runErr2 != nil {
		return 0, false, fmt.Errorf("querying schema_migrations version for %s: %w", db, runErr2)
	}

	ver := parseCSVSingleInt(out2)
	return ver, false, nil
}

// parseCSVSingleInt parses the second line of CSV output as an integer.
// Returns 0 on any parse failure.
func parseCSVSingleInt(output []byte) int {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(lines[len(lines)-1]))
	return n
}
