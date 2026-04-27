package storage

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newTestDB returns a fresh in-memory SQLite with all embedded migrations
// applied. It's the shared fixture for every storage-package test that
// exercises real SQL behavior.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := NewMigrator(db).Migrate(); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return db
}

// seedChatAndMessage inserts a chat row + one message row, satisfying the
// FK between messages.chat_jid and chats.jid. Returns the message ID it
// inserted so tests can target it.
func seedChatAndMessage(t *testing.T, db *sql.DB, msgID, chatJID, senderJID, text string, ts time.Time) {
	t.Helper()

	if _, err := db.Exec(
		`INSERT OR IGNORE INTO chats (jid, last_message_time, is_group) VALUES (?, ?, 0)`,
		chatJID, ts.Unix(),
	); err != nil {
		t.Fatalf("seed chat: %v", err)
	}
	if _, err := db.Exec(
		`INSERT INTO messages (id, chat_jid, sender_jid, text, timestamp, is_from_me, message_type)
		 VALUES (?, ?, ?, ?, ?, 0, 'text')`,
		msgID, chatJID, senderJID, text, ts.Unix(),
	); err != nil {
		t.Fatalf("seed message: %v", err)
	}
}

func TestMarkMessageEdited(t *testing.T) {
	db := newTestDB(t)
	store := NewMessageStore(db)

	now := time.Now().Truncate(time.Second)
	seedChatAndMessage(t, db, "msg1", "chat@s.whatsapp.net", "sender@s.whatsapp.net", "original", now)

	editTime := now.Add(time.Minute)
	if err := store.MarkMessageEdited("msg1", "edited!", editTime); err != nil {
		t.Fatalf("MarkMessageEdited: %v", err)
	}

	var text string
	var editedAt sql.NullInt64
	if err := db.QueryRow(`SELECT text, edited_at FROM messages WHERE id = 'msg1'`).Scan(&text, &editedAt); err != nil {
		t.Fatalf("readback: %v", err)
	}
	if text != "edited!" {
		t.Errorf("text: got %q, want %q", text, "edited!")
	}
	if !editedAt.Valid {
		t.Error("expected edited_at to be set")
	}
	if editedAt.Valid && editedAt.Int64 != editTime.Unix() {
		t.Errorf("edited_at: got %d, want %d", editedAt.Int64, editTime.Unix())
	}
}

func TestMarkMessageEditedNotFoundReturnsErrNoRows(t *testing.T) {
	db := newTestDB(t)
	store := NewMessageStore(db)

	err := store.MarkMessageEdited("ghost-msg", "new", time.Now())
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestMarkMessageDeleted(t *testing.T) {
	db := newTestDB(t)
	store := NewMessageStore(db)

	now := time.Now().Truncate(time.Second)
	seedChatAndMessage(t, db, "msg2", "chat@s.whatsapp.net", "sender@s.whatsapp.net", "to delete", now)

	deleteTime := now.Add(time.Hour)
	if err := store.MarkMessageDeleted("msg2", deleteTime); err != nil {
		t.Fatalf("MarkMessageDeleted: %v", err)
	}

	var deletedAt sql.NullInt64
	if err := db.QueryRow(`SELECT deleted_at FROM messages WHERE id = 'msg2'`).Scan(&deletedAt); err != nil {
		t.Fatalf("readback: %v", err)
	}
	if !deletedAt.Valid {
		t.Fatal("expected deleted_at to be set")
	}
	if deletedAt.Int64 != deleteTime.Unix() {
		t.Errorf("deleted_at: got %d, want %d", deletedAt.Int64, deleteTime.Unix())
	}
}

func TestMarkMessageDeletedNotFoundReturnsErrNoRows(t *testing.T) {
	db := newTestDB(t)
	store := NewMessageStore(db)

	err := store.MarkMessageDeleted("ghost", time.Now())
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestSaveReactionInsertsNewRow(t *testing.T) {
	db := newTestDB(t)
	store := NewMessageStore(db)

	now := time.Now().Truncate(time.Second)
	seedChatAndMessage(t, db, "msg3", "chat@s.whatsapp.net", "sender@s.whatsapp.net", "react to me", now)

	r := Reaction{
		MessageID: "msg3",
		ChatJID:   "chat@s.whatsapp.net",
		SenderJID: "u1@s.whatsapp.net",
		Emoji:     "❤️",
		Timestamp: now.Add(time.Second),
	}
	if err := store.SaveReaction(r); err != nil {
		t.Fatalf("SaveReaction: %v", err)
	}

	got, err := store.GetReactionsForMessage("msg3")
	if err != nil {
		t.Fatalf("GetReactionsForMessage: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 reaction, got %d", len(got))
	}
	if got[0].Emoji != "❤️" {
		t.Errorf("emoji: got %q, want ❤️", got[0].Emoji)
	}
	if got[0].SenderJID != "u1@s.whatsapp.net" {
		t.Errorf("sender: got %q", got[0].SenderJID)
	}
}

func TestSaveReactionUpsertsOnSameSender(t *testing.T) {
	db := newTestDB(t)
	store := NewMessageStore(db)

	now := time.Now().Truncate(time.Second)
	seedChatAndMessage(t, db, "msg4", "chat@s.whatsapp.net", "sender@s.whatsapp.net", "x", now)

	first := Reaction{
		MessageID: "msg4",
		ChatJID:   "chat@s.whatsapp.net",
		SenderJID: "u1@s.whatsapp.net",
		Emoji:     "👍",
		Timestamp: now,
	}
	if err := store.SaveReaction(first); err != nil {
		t.Fatalf("first SaveReaction: %v", err)
	}

	// same sender, different emoji, later timestamp
	second := Reaction{
		MessageID: "msg4",
		ChatJID:   "chat@s.whatsapp.net",
		SenderJID: "u1@s.whatsapp.net",
		Emoji:     "🔥",
		Timestamp: now.Add(time.Minute),
	}
	if err := store.SaveReaction(second); err != nil {
		t.Fatalf("second SaveReaction: %v", err)
	}

	got, err := store.GetReactionsForMessage("msg4")
	if err != nil {
		t.Fatalf("GetReactionsForMessage: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row after upsert, got %d", len(got))
	}
	if got[0].Emoji != "🔥" {
		t.Errorf("expected upserted emoji 🔥, got %q", got[0].Emoji)
	}
}

func TestSaveReactionWithEmptyEmojiRepresentsRemoval(t *testing.T) {
	db := newTestDB(t)
	store := NewMessageStore(db)

	now := time.Now().Truncate(time.Second)
	seedChatAndMessage(t, db, "msg5", "chat@s.whatsapp.net", "sender@s.whatsapp.net", "x", now)

	if err := store.SaveReaction(Reaction{
		MessageID: "msg5",
		ChatJID:   "chat@s.whatsapp.net",
		SenderJID: "u1@s.whatsapp.net",
		Emoji:     "❤️",
		Timestamp: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReaction(Reaction{
		MessageID: "msg5",
		ChatJID:   "chat@s.whatsapp.net",
		SenderJID: "u1@s.whatsapp.net",
		Emoji:     "", // removal
		Timestamp: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetReactionsForMessage("msg5")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Emoji != "" {
		t.Errorf("expected single empty-emoji row, got %v", got)
	}
}

func TestMultipleSendersOnSameMessage(t *testing.T) {
	db := newTestDB(t)
	store := NewMessageStore(db)

	now := time.Now().Truncate(time.Second)
	seedChatAndMessage(t, db, "msg6", "chat@s.whatsapp.net", "sender@s.whatsapp.net", "x", now)

	for i, jid := range []string{"a@s.whatsapp.net", "b@s.whatsapp.net", "c@s.whatsapp.net"} {
		if err := store.SaveReaction(Reaction{
			MessageID: "msg6",
			ChatJID:   "chat@s.whatsapp.net",
			SenderJID: jid,
			Emoji:     "❤️",
			Timestamp: now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("save %s: %v", jid, err)
		}
	}

	got, err := store.GetReactionsForMessage("msg6")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 reactions, got %d", len(got))
	}
}

func TestReactionsCascadeOnMessageDelete(t *testing.T) {
	db := newTestDB(t)
	store := NewMessageStore(db)

	now := time.Now().Truncate(time.Second)
	seedChatAndMessage(t, db, "msg7", "chat@s.whatsapp.net", "sender@s.whatsapp.net", "x", now)

	if err := store.SaveReaction(Reaction{
		MessageID: "msg7",
		ChatJID:   "chat@s.whatsapp.net",
		SenderJID: "u1@s.whatsapp.net",
		Emoji:     "❤️",
		Timestamp: now,
	}); err != nil {
		t.Fatal(err)
	}

	// hard-delete the message; FK cascade should remove the reaction
	if _, err := db.Exec(`DELETE FROM messages WHERE id = 'msg7'`); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetReactionsForMessage("msg7")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected reactions to cascade-delete, got %d", len(got))
	}
}
