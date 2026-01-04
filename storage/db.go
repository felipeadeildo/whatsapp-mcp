package storage

import (
	"database/sql"
	"fmt"
	"whatsapp-mcp/paths"

	_ "modernc.org/sqlite"
)

// initializes the database and runs migrations
func InitDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite", paths.MessagesDBPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")

	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	// run migrations
	migrator := NewMigrator(db)
	if err := migrator.Migrate(); err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return db, nil
}
