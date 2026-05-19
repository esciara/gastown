package doltserver

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// RigSchemaStatus holds the schema migration state of a single rig database.
type RigSchemaStatus struct {
	// RigName is the logical rig name (e.g. "gastown", "hq").
	RigName string
	// DBName is the Dolt database name (may differ from RigName for rigs that
	// use a prefix-based DB name, e.g. "gt" for laneassist).
	DBName string
	// SchemaVersion is the value of config.schema_version, or 0 if absent.
	SchemaVersion int
	// HasSchemaVersion is true when the config table exists and contains schema_version.
	HasSchemaVersion bool
	// Error is non-empty when the rig could not be queried (server unreachable,
	// database absent, etc.).
	Error string
}

// SchemaParity summarises the cross-rig parity result.
type SchemaParity struct {
	// Statuses contains one entry per known rig database.
	Statuses []RigSchemaStatus
	// MaxVersion is the highest schema_version seen across all reachable rigs.
	MaxVersion int
	// DriftedRigs names rigs whose schema_version is below MaxVersion.
	DriftedRigs []string
	// UninitializedRigs names rigs whose config table or schema_version key is
	// missing (provisioned outside the normal bd-init path).
	UninitializedRigs []string
	// ErrorRigs names rigs that could not be queried at all.
	ErrorRigs []string
}

// CheckRigsSchemaVersion connects to the running Dolt server and queries
// schema_version from the config table of every known rig database.
// It returns a SchemaParity that the caller can inspect to detect drift.
//
// If the server is not running or the config table does not exist in a DB,
// those conditions are recorded in the returned statuses rather than causing
// an error return.
func CheckRigsSchemaVersion(townRoot string) (*SchemaParity, error) {
	config := DefaultConfig(townRoot)

	// Build the set of (rigName, dbName) pairs to check.
	type rigDB struct {
		rigName string
		dbName  string
	}
	var targets []rigDB

	// Town-level: hq database.
	townBeadsDir := filepath.Join(townRoot, ".beads")
	if dbName := readExistingDoltDatabase(townBeadsDir); dbName != "" {
		targets = append(targets, rigDB{"hq", dbName})
	} else if _, err := os.Stat(townBeadsDir); err == nil {
		// .beads exists but no dolt DB configured; use "hq" as fallback.
		targets = append(targets, rigDB{"hq", "hq"})
	}

	// Per-rig databases from rigs.json.
	rigsPath := filepath.Join(townRoot, "mayor", "rigs.json")
	rigs := loadRigNames(rigsPath) // defined in migration_check.go
	rigNames := make([]string, 0, len(rigs))
	for name := range rigs {
		rigNames = append(rigNames, name)
	}
	sort.Strings(rigNames)
	for _, rigName := range rigNames {
		beadsDir := FindRigBeadsDir(townRoot, rigName)
		dbName := rigName
		if beadsDir != "" {
			if d := readExistingDoltDatabase(beadsDir); d != "" {
				dbName = d
			}
		}
		targets = append(targets, rigDB{rigName, dbName})
	}

	if len(targets) == 0 {
		return &SchemaParity{}, nil
	}

	// Open a single MySQL connection to the Dolt server.
	// Use a short connection timeout; if the server is not running we surface
	// the error in each status rather than failing the function entirely.
	dsn := fmt.Sprintf("%s@tcp(%s)/?timeout=5s&readTimeout=10s&writeTimeout=5s",
		config.userDSN(), config.HostPort())
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening Dolt connection: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(15 * time.Second)
	defer db.Close()

	parity := &SchemaParity{}

	for _, t := range targets {
		status := queryRigSchemaVersion(db, t.rigName, t.dbName)
		parity.Statuses = append(parity.Statuses, status)

		if status.Error != "" {
			parity.ErrorRigs = append(parity.ErrorRigs, t.rigName)
		} else if !status.HasSchemaVersion {
			parity.UninitializedRigs = append(parity.UninitializedRigs, t.rigName)
		} else if status.SchemaVersion > parity.MaxVersion {
			parity.MaxVersion = status.SchemaVersion
		}
	}

	// Second pass: collect drifted rigs (reachable but below MaxVersion).
	for _, s := range parity.Statuses {
		if s.Error == "" && s.HasSchemaVersion && s.SchemaVersion < parity.MaxVersion {
			parity.DriftedRigs = append(parity.DriftedRigs, s.RigName)
		}
	}

	return parity, nil
}

// queryRigSchemaVersion queries the schema_version for a single rig DB.
func queryRigSchemaVersion(db *sql.DB, rigName, dbName string) RigSchemaStatus {
	status := RigSchemaStatus{RigName: rigName, DBName: dbName}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Check the config table exists before querying it.
	var tableCount int
	err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = ? AND table_name = 'config'",
		dbName,
	).Scan(&tableCount)
	if err != nil {
		status.Error = fmt.Sprintf("information_schema query failed: %v", err)
		return status
	}
	if tableCount == 0 {
		// config table missing — rig was provisioned without bd init.
		return status
	}

	// Query schema_version from the config table.
	var version int
	err = db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT `value` FROM `%s`.`config` WHERE `key` = 'schema_version'",
			escapeDoltIdentifier(dbName)),
	).Scan(&version)
	if err == sql.ErrNoRows {
		// config table present but schema_version key missing.
		return status
	}
	if err != nil {
		status.Error = fmt.Sprintf("reading schema_version: %v", err)
		return status
	}

	status.HasSchemaVersion = true
	status.SchemaVersion = version
	return status
}

// escapeDoltIdentifier escapes a database/table name for safe use in SQL
// identifiers (backtick-quoted). Only replaces backticks; Dolt identifiers
// are restricted to alphanumeric + underscore in practice.
func escapeDoltIdentifier(name string) string {
	return strings.ReplaceAll(name, "`", "``")
}

// SchemaDriftError is returned by CheckRigSchemaParity when one or more rig
// databases are uninitialized or behind the expected schema version.
type SchemaDriftError struct {
	Uninitialized []string
	Drifted       []string
	MaxVersion    int
}

func (e *SchemaDriftError) Error() string {
	var parts []string
	if len(e.Uninitialized) > 0 {
		parts = append(parts,
			fmt.Sprintf("rig(s) with no schema_version (provisioned outside bd init): %s",
				strings.Join(e.Uninitialized, ", ")))
	}
	if len(e.Drifted) > 0 {
		parts = append(parts,
			fmt.Sprintf("rig(s) below schema_version %d: %s",
				e.MaxVersion, strings.Join(e.Drifted, ", ")))
	}
	return strings.Join(parts, "; ")
}
