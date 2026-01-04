package storage

import (
	"crypto/sha256"
	"database/sql"
	"embed"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"whatsapp-mcp/paths"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migration represents a single database migration
type Migration struct {
	Version     int
	Description string
	SQL         string
	Checksum    string
	Filename    string
}

// Migrator handles database migrations
type Migrator struct {
	db *sql.DB
}

// NewMigrator creates a new migrator instance
func NewMigrator(db *sql.DB) *Migrator {
	return &Migrator{db: db}
}

// Migrate runs all pending migrations
func (m *Migrator) Migrate() error {
	// 1. ensure schema_migrations table exists
	if err := m.ensureMigrationTable(); err != nil {
		return fmt.Errorf("failed to create migration table: %w", err)
	}

	// 2. get current schema version from database
	currentVersion, err := m.getCurrentVersion()
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	// 3. load all migration files from embedded FS
	migrations, err := m.loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// 4. validate migration checksums for already-applied migrations
	if err := m.validateAppliedMigrations(migrations, currentVersion); err != nil {
		return fmt.Errorf("migration validation failed: %w", err)
	}

	// 5. apply pending migrations in order
	pendingMigrations := m.filterPendingMigrations(migrations, currentVersion)

	if len(pendingMigrations) == 0 {
		log.Println("Database schema is up to date")
		return nil
	}

	log.Printf("Applying %d pending migration(s)...", len(pendingMigrations))
	for _, migration := range pendingMigrations {
		log.Printf("  - Migration %d: %s", migration.Version, migration.Description)
		if err := m.applyMigration(migration); err != nil {
			return fmt.Errorf("failed to apply migration %d (%s): %w",
				migration.Version, migration.Description, err)
		}
	}

	log.Println("Migrations completed successfully")
	return nil
}

// ensureMigrationTable creates the schema_migrations table if it doesn't exist
func (m *Migrator) ensureMigrationTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		description TEXT NOT NULL,
		applied_at INTEGER NOT NULL,
		checksum TEXT NOT NULL
	);
	`
	_, err := m.db.Exec(query)
	return err
}

// getCurrentVersion gets the highest version number from applied migrations
func (m *Migrator) getCurrentVersion() (int, error) {
	var version int
	err := m.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version)
	if err != nil {
		return 0, err
	}
	log.Printf("Current schema version: %d", version)
	return version, nil
}

// loadMigrations loads all migration files from the embedded filesystem
func (m *Migrator) loadMigrations() ([]Migration, error) {
	var migrations []Migration

	// read migrations directory
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// parse each .sql file
	migrationPattern := regexp.MustCompile(`^(\d{3})_(.+)\.sql$`)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		matches := migrationPattern.FindStringSubmatch(filename)
		if matches == nil {
			// skip files that don't match the pattern
			continue
		}

		versionStr := matches[1]
		description := matches[2]

		version, err := strconv.Atoi(versionStr)
		if err != nil {
			return nil, fmt.Errorf("invalid version number in %s: %w", filename, err)
		}

		// read file content
		content, err := migrationsFS.ReadFile("migrations/" + filename)
		if err != nil {
			return nil, fmt.Errorf("failed to read migration file %s: %w", filename, err)
		}

		// calculate checksum
		hash := sha256.Sum256(content)
		checksum := fmt.Sprintf("%x", hash)

		migrations = append(migrations, Migration{
			Version:     version,
			Description: strings.ReplaceAll(description, "_", " "),
			SQL:         string(content),
			Checksum:    checksum,
			Filename:    filename,
		})
	}

	// sort by version number
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	// validate version sequence (no gaps or duplicates)
	for i, migration := range migrations {
		expectedVersion := i + 1
		if migration.Version != expectedVersion {
			return nil, fmt.Errorf("migration version sequence error: expected %d, got %d (%s)",
				expectedVersion, migration.Version, migration.Filename)
		}
	}

	return migrations, nil
}

// validateAppliedMigrations validates that already-applied migrations haven't been modified
func (m *Migrator) validateAppliedMigrations(migrations []Migration, currentVersion int) error {
	for _, migration := range migrations {
		if migration.Version > currentVersion {
			// this migration hasn't been applied yet, skip validation
			continue
		}

		// get the stored checksum from database
		var storedChecksum string
		err := m.db.QueryRow(
			"SELECT checksum FROM schema_migrations WHERE version = ?",
			migration.Version,
		).Scan(&storedChecksum)

		if err != nil {
			return fmt.Errorf("migration %d (%s) is missing from schema_migrations table. Database may be corrupted or partially migrated",
				migration.Version, migration.Filename)
		}

		// compare checksums
		if storedChecksum != migration.Checksum {
			return fmt.Errorf(
				"migration %d (%s) has been modified after being applied (checksum mismatch). "+
					"Never modify applied migrations - create a new migration instead. "+
					"If this is a development environment, you may need to reset your database",
				migration.Version, migration.Filename,
			)
		}
	}

	return nil
}

// filterPendingMigrations filters migrations to only those not yet applied
func (m *Migrator) filterPendingMigrations(migrations []Migration, currentVersion int) []Migration {
	var pending []Migration
	for _, migration := range migrations {
		if migration.Version > currentVersion {
			pending = append(pending, migration)
		}
	}
	return pending
}

// applyMigration applies a single migration within a transaction
func (m *Migrator) applyMigration(migration Migration) error {
	// start transaction
	tx, err := m.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// execute the migration SQL
	_, err = tx.Exec(migration.SQL)
	if err != nil {
		return fmt.Errorf("failed to execute migration SQL: %w", err)
	}

	// record the migration in schema_migrations
	_, err = tx.Exec(
		`INSERT INTO schema_migrations (version, description, applied_at, checksum)
		 VALUES (?, ?, ?, ?)`,
		migration.Version,
		migration.Description,
		time.Now().Unix(),
		migration.Checksum,
	)
	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	// commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration: %w", err)
	}

	return nil
}

// MigrationStatus represents the status of a migration
type MigrationStatus struct {
	Version     int
	Description string
	Filename    string
	Applied     bool
	AppliedAt   *time.Time
}

// GetMigrationStatus returns information about applied and pending migrations
func (m *Migrator) GetMigrationStatus() ([]MigrationStatus, error) {
	migrations, err := m.loadMigrations()
	if err != nil {
		return nil, err
	}

	var statuses []MigrationStatus
	for _, migration := range migrations {
		var appliedAt sql.NullInt64
		err := m.db.QueryRow(
			"SELECT applied_at FROM schema_migrations WHERE version = ?",
			migration.Version,
		).Scan(&appliedAt)

		// Only ignore sql.ErrNoRows (migration not applied yet)
		// Return any other errors
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("failed to query migration status for version %d: %w", migration.Version, err)
		}

		status := MigrationStatus{
			Version:     migration.Version,
			Description: migration.Description,
			Filename:    migration.Filename,
			Applied:     appliedAt.Valid,
		}

		if appliedAt.Valid {
			t := time.Unix(appliedAt.Int64, 0)
			status.AppliedAt = &t
		}

		statuses = append(statuses, status)
	}

	return statuses, nil
}

// MigrateTo runs migrations up to a specific target version
// if targetVersion is 0 or negative, applies all migrations (same as Migrate)
func (m *Migrator) MigrateTo(targetVersion int) error {
	// 1. ensure schema_migrations table exists
	if err := m.ensureMigrationTable(); err != nil {
		return fmt.Errorf("failed to create migration table: %w", err)
	}

	// 2. get current schema version from database
	currentVersion, err := m.getCurrentVersion()
	if err != nil {
		return fmt.Errorf("failed to get current version: %w", err)
	}

	// 3. load all migration files from embedded FS
	migrations, err := m.loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	// 4. validate target version
	if targetVersion > 0 {
		found := false
		for _, migration := range migrations {
			if migration.Version == targetVersion {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("target version %d does not exist. Check available migrations in %s/", targetVersion, paths.MigrationsDir)
		}
	}

	// 5. validate migration checksums for already-applied migrations
	if err := m.validateAppliedMigrations(migrations, currentVersion); err != nil {
		return fmt.Errorf("migration validation failed: %w", err)
	}

	// 6. filter migrations to apply (only up to target version)
	var migrationsToApply []Migration
	for _, migration := range migrations {
		if migration.Version > currentVersion {
			if targetVersion <= 0 || migration.Version <= targetVersion {
				migrationsToApply = append(migrationsToApply, migration)
			}
		}
	}

	if len(migrationsToApply) == 0 {
		if targetVersion > 0 && targetVersion <= currentVersion {
			log.Printf("Already at version %d or higher (current: %d)", targetVersion, currentVersion)
		} else {
			log.Println("Database schema is up to date")
		}
		return nil
	}

	log.Printf("Applying %d migration(s)...", len(migrationsToApply))
	for _, migration := range migrationsToApply {
		log.Printf("  - Migration %d: %s", migration.Version, migration.Description)
		if err := m.applyMigration(migration); err != nil {
			return fmt.Errorf("failed to apply migration %d (%s): %w",
				migration.Version, migration.Description, err)
		}
	}

	log.Println("Migrations completed successfully")
	return nil
}
