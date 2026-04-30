package storage

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func openTranscriptTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`
		CREATE TABLE transcripts (
			message_id TEXT PRIMARY KEY,
			text       TEXT NOT NULL,
			created_at INTEGER NOT NULL DEFAULT (unixepoch())
		)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func TestTranscriptStoreMissReturnsFoundFalse(t *testing.T) {
	store := NewTranscriptStore(openTranscriptTestDB(t))

	text, found, err := store.Get("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error on miss: %v", err)
	}
	if found {
		t.Errorf("expected found=false on miss, got true")
	}
	if text != "" {
		t.Errorf("expected empty text on miss, got %q", text)
	}
}

func TestTranscriptStoreSaveAndGet(t *testing.T) {
	store := NewTranscriptStore(openTranscriptTestDB(t))

	const id = "ABC123"
	const want = "Bom dia, tudo bem?"

	if err := store.Save(id, want); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, found, err := store.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !found {
		t.Fatalf("expected found=true after save, got false")
	}
	if got != want {
		t.Errorf("text: got %q, want %q", got, want)
	}
}

func TestTranscriptStoreSaveOverwrites(t *testing.T) {
	store := NewTranscriptStore(openTranscriptTestDB(t))

	const id = "XYZ"
	if err := store.Save(id, "first"); err != nil {
		t.Fatalf("save first: %v", err)
	}
	if err := store.Save(id, "second"); err != nil {
		t.Fatalf("save second: %v", err)
	}

	got, _, err := store.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != "second" {
		t.Errorf("expected overwrite to win, got %q", got)
	}
}
