package storage

import (
	"database/sql"
	"os"
	"whatsapp-mcp/paths"

	_ "modernc.org/sqlite"
)

// init db and create schema
func InitDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite", paths.MessagesDBPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")

	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	if err := createSchema(db); err != nil {
		return nil, err
	}

	return db, nil
}

// create schema from the file
func createSchema(db *sql.DB) error {
	data, err := os.ReadFile("schema.sql")
	if err != nil {
		return err
	}

	schema := string(data)

	_, err = db.Exec(schema)

	return err
}
