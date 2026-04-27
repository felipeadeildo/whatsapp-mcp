package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// Reaction is a single reaction emoji sent by one user against one message.
// An empty Emoji represents a reaction that was explicitly removed by the
// sender; receivers / readers should treat it as "no reaction" rather than
// the literal empty character.
type Reaction struct {
	MessageID string
	ChatJID   string
	SenderJID string
	Emoji     string
	Timestamp time.Time
}

// SaveReaction inserts or replaces a reaction. The (message_id, sender_jid)
// pair forms the unique key, so a sender's later reaction supersedes their
// previous one (including the "removed" state, represented by an empty
// emoji). Older timestamps are preserved if a duplicate write arrives out of
// order.
func (s *MessageStore) SaveReaction(r Reaction) error {
	const query = `
	INSERT INTO message_reactions (message_id, chat_jid, sender_jid, emoji, timestamp)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(message_id, sender_jid) DO UPDATE SET
		emoji = excluded.emoji,
		timestamp = MAX(message_reactions.timestamp, excluded.timestamp)
	`
	_, err := s.db.Exec(
		query,
		r.MessageID,
		r.ChatJID,
		r.SenderJID,
		r.Emoji,
		r.Timestamp.Unix(),
	)
	if err != nil {
		return fmt.Errorf("failed to save reaction for message %s: %w", r.MessageID, err)
	}
	return nil
}

// GetReactionsForMessage returns every reaction recorded against a single
// message. Empty-emoji entries are included so callers can render "removed"
// state if they want; callers that only care about active reactions should
// filter on emoji != "".
func (s *MessageStore) GetReactionsForMessage(messageID string) ([]Reaction, error) {
	const query = `
	SELECT message_id, chat_jid, sender_jid, emoji, timestamp
	FROM message_reactions
	WHERE message_id = ?
	ORDER BY timestamp ASC
	`
	rows, err := s.db.Query(query, messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to query reactions: %w", err)
	}
	defer rows.Close()

	var reactions []Reaction
	for rows.Next() {
		var r Reaction
		var ts int64
		if err := rows.Scan(&r.MessageID, &r.ChatJID, &r.SenderJID, &r.Emoji, &ts); err != nil {
			return nil, fmt.Errorf("failed to scan reaction: %w", err)
		}
		r.Timestamp = time.Unix(ts, 0)
		reactions = append(reactions, r)
	}
	return reactions, rows.Err()
}

// MarkMessageEdited updates a message's text and stamps its edited_at column.
// It returns sql.ErrNoRows when the message is not found, which lets callers
// distinguish "edited a message we never stored" (e.g. an event arrived for
// a message synced after the local horizon) from a real database failure.
func (s *MessageStore) MarkMessageEdited(messageID, newText string, ts time.Time) error {
	const query = `
	UPDATE messages
	SET text = ?, edited_at = ?
	WHERE id = ?
	`
	res, err := s.db.Exec(query, newText, ts.Unix(), messageID)
	if err != nil {
		return fmt.Errorf("failed to mark message %s edited: %w", messageID, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// MarkMessageDeleted stamps the deleted_at column on an existing message.
// The message row is preserved so historical context is not lost; readers
// should treat a non-null deleted_at as the WhatsApp "Message deleted"
// placeholder. Returns sql.ErrNoRows when the message is not found.
func (s *MessageStore) MarkMessageDeleted(messageID string, ts time.Time) error {
	const query = `
	UPDATE messages
	SET deleted_at = ?
	WHERE id = ?
	`
	res, err := s.db.Exec(query, ts.Unix(), messageID)
	if err != nil {
		return fmt.Errorf("failed to mark message %s deleted: %w", messageID, err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
