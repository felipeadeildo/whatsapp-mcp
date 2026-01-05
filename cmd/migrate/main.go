// Migrate is a CLI tool for managing database schema migrations.
//
// This command provides utilities for creating, viewing, and applying
// database migrations for the WhatsApp MCP server. It ensures safe,
// versioned schema changes with checksum validation.
//
// Commands:
//
//	create <description>  - Create a new migration file
//	status                - Show migration status
//	upgrade [version]     - Apply pending migrations (all or up to version)
//
// Examples:
//
//	# Create a new migration
//	go run cmd/migrate/main.go create add_user_table
//
//	# Check migration status
//	go run cmd/migrate/main.go status
//
//	# Apply all pending migrations
//	go run cmd/migrate/main.go upgrade
//
//	# Upgrade to specific version
//	go run cmd/migrate/main.go upgrade 5
//
// Migration files are stored in storage/migrations/ and are automatically
// embedded in the application binary. Never modify applied migrations.
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"whatsapp-mcp/paths"
	"whatsapp-mcp/storage"

	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "create":
		if len(os.Args) < 3 {
			fmt.Println("Error: migration description required")
			fmt.Println("Usage: go run cmd/migrate/main.go create <description>")
			fmt.Println("\nExample: go run cmd/migrate/main.go create add_message_reactions")
			os.Exit(1)
		}
		description := strings.Join(os.Args[2:], "_")
		if err := createMigration(description); err != nil {
			fmt.Printf("Error creating migration: %v\n", err)
			os.Exit(1)
		}
	case "status":
		if err := showStatus(); err != nil {
			fmt.Printf("Error showing status: %v\n", err)
			os.Exit(1)
		}
	case "upgrade":
		target := "latest"
		if len(os.Args) > 2 {
			target = os.Args[2]
		}
		if err := runUpgrade(target); err != nil {
			fmt.Printf("Error running upgrade: %v\n", err)
			os.Exit(1)
		}
	default:
		printUsage()
		os.Exit(1)
	}
}

// printUsage prints the usage information for the migration tool.
func printUsage() {
	fmt.Println("Migration CLI Tool")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  go run cmd/migrate/main.go create <description>")
	fmt.Println("  go run cmd/migrate/main.go status")
	fmt.Println("  go run cmd/migrate/main.go upgrade [version|latest]")
	fmt.Println("")
	fmt.Println("Commands:")
	fmt.Println("  create      Create a new migration file")
	fmt.Println("  status      Show migration status (applied and pending)")
	fmt.Println("  upgrade     Apply migrations up to specified version or latest")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  go run cmd/migrate/main.go create add_message_reactions")
	fmt.Println("  go run cmd/migrate/main.go create add_user_preferences_table")
	fmt.Println("  go run cmd/migrate/main.go status")
	fmt.Println("  go run cmd/migrate/main.go upgrade latest")
	fmt.Println("  go run cmd/migrate/main.go upgrade 2")
}

// createMigration creates a new migration file with the given description.
func createMigration(description string) error {
	// sanitize description
	description = sanitizeDescription(description)

	// ensure migrations directory exists
	if err := os.MkdirAll(paths.MigrationsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create migrations directory: %w", err)
	}

	// get next version number
	nextVersion, err := getNextVersion(paths.MigrationsDir)
	if err != nil {
		return fmt.Errorf("failed to determine next version: %w", err)
	}

	// create filename
	filename := fmt.Sprintf("%03d_%s.sql", nextVersion, description)
	migrationPath := filepath.Join(paths.MigrationsDir, filename)

	// create file with template
	template := generateMigrationTemplate(nextVersion, description)

	if err := os.WriteFile(migrationPath, []byte(template), 0o644); err != nil {
		return fmt.Errorf("failed to write migration file: %w", err)
	}

	fmt.Printf("Created migration: %s\n", migrationPath)
	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Println("1. Edit the migration file and add your SQL statements")
	fmt.Println("2. Run the application to apply the migration")
	fmt.Println("")

	return nil
}

// sanitizeDescription removes invalid characters from a migration description.
func sanitizeDescription(description string) string {
	// replace spaces and invalid characters with underscores
	re := regexp.MustCompile(`[^a-zA-Z0-9_]+`)
	sanitized := re.ReplaceAllString(description, "_")
	sanitized = strings.Trim(sanitized, "_")
	sanitized = strings.ToLower(sanitized)
	return sanitized
}

// getNextVersion determines the next migration version number.
func getNextVersion(migrationsDir string) (int, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			// directory doesn't exist, this is version 1
			return 1, nil
		}
		return 0, err
	}

	maxVersion := 0
	migrationPattern := regexp.MustCompile(`^(\d{3})_.*\.sql$`)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		matches := migrationPattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			continue
		}

		version, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}

		if version > maxVersion {
			maxVersion = version
		}
	}

	return maxVersion + 1, nil
}

// generateMigrationTemplate generates a migration file template with metadata.
func generateMigrationTemplate(version int, description string) string {
	now := time.Now().Format("2006-01-02")
	prevVersion := version - 1
	prevVersionStr := "none"
	if prevVersion > 0 {
		prevVersionStr = fmt.Sprintf("%03d", prevVersion)
	}

	return fmt.Sprintf(`-- Migration: %03d_%s
-- Description: %s
-- Previous: %s
-- Version: %03d
-- Created: %s

-- Add your SQL statements below
-- Example:
-- CREATE TABLE IF NOT EXISTS example (
--     id INTEGER PRIMARY KEY,
--     name TEXT NOT NULL
-- );

-- Data transformation example:
-- UPDATE existing_table SET new_column = 'default_value' WHERE new_column IS NULL;

-- Create indexes:
-- CREATE INDEX IF NOT EXISTS idx_example_name ON example(name);
`,
		version,
		description,
		strings.ReplaceAll(description, "_", " "),
		prevVersionStr,
		version,
		now,
	)
}

// openDB opens a connection to the database.
func openDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite", storage.GetConnectionString())
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	return db, nil
}

// showStatus displays the current migration status.
func showStatus() error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	migrator := storage.NewMigrator(db)
	statuses, err := migrator.GetMigrationStatus()
	if err != nil {
		return fmt.Errorf("failed to get migration status: %w", err)
	}

	if len(statuses) == 0 {
		fmt.Printf("\nNo migration files found in %s/\n", paths.MigrationsDir)
		fmt.Println("\nTo create your first migration, run:")
		fmt.Println("  go run cmd/migrate/main.go create <description>")
		fmt.Println("\nExample:")
		fmt.Println("  go run cmd/migrate/main.go create initial_schema")
		return nil
	}

	// Count applied and pending migrations
	appliedCount := 0
	pendingCount := 0
	for _, status := range statuses {
		if status.Applied {
			appliedCount++
		} else {
			pendingCount++
		}
	}

	fmt.Println("\nMigration Status:")
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("%-10s %-10s %-35s %s\n", "Version", "Status", "Description", "Applied At")
	fmt.Println(strings.Repeat("-", 80))

	for _, status := range statuses {
		statusStr := "pending"
		appliedAtStr := "-"

		if status.Applied {
			statusStr = "applied"
			if status.AppliedAt != nil {
				appliedAtStr = status.AppliedAt.Format("2006-01-02 15:04:05")
			}
		}

		fmt.Printf("%-10d %-10s %-35s %s\n",
			status.Version,
			statusStr,
			truncateString(status.Description, 35),
			appliedAtStr,
		)
	}
	fmt.Println(strings.Repeat("-", 80))
	fmt.Printf("Total: %d migrations (%d applied, %d pending)\n", len(statuses), appliedCount, pendingCount)

	if pendingCount > 0 {
		fmt.Println("\nTo apply pending migrations, run:")
		fmt.Println("  go run cmd/migrate/main.go upgrade latest")
	}

	return nil
}

// truncateString truncates a string to the specified maximum length.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// runUpgrade runs database migrations up to the specified target version.
func runUpgrade(target string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	migrator := storage.NewMigrator(db)

	if target == "latest" {
		fmt.Println("Upgrading to latest version...")
		return migrator.Migrate()
	}

	// parse target version
	version, err := strconv.Atoi(target)
	if err != nil {
		return fmt.Errorf("invalid version number: %s (use 'latest' or a positive integer). Example: 'upgrade 2' or 'upgrade latest'", target)
	}

	if version <= 0 {
		return fmt.Errorf("version must be a positive number (got %d). Example: 'upgrade 2' or 'upgrade latest'", version)
	}

	fmt.Printf("Upgrading to version %d...\n", version)
	return migrator.MigrateTo(version)
}
