package migrations

import (
	"context"
	"database/sql"

	"whatsapp-mcp/storage"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func init() {
	goose.AddMigrationContext(upInitialSchema, downInitialSchema)
}

func upInitialSchema(ctx context.Context, tx *sql.Tx) error {
	// Wrap the transaction in a GORM DB instance
	gormDB, err := gorm.Open(sqlite.Dialector{Conn: tx}, &gorm.Config{})
	if err != nil {
		return err
	}

	// Create all tables using GORM's AutoMigrate
	return gormDB.AutoMigrate(
		&storage.Chat{},
		&storage.Message{},
		&storage.PushName{},
		&storage.GroupParticipant{},
	)
}

func downInitialSchema(ctx context.Context, tx *sql.Tx) error {
	// Drop all tables in reverse order (respecting foreign keys)
	_, err := tx.ExecContext(ctx, `
		DROP TABLE IF EXISTS group_participants;
		DROP TABLE IF EXISTS messages;
		DROP TABLE IF EXISTS push_names;
		DROP TABLE IF EXISTS chats;
	`)
	return err
}
