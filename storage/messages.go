package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// represents a whatsapp message
type Message struct {
	ID          string
	ChatJID     string // Canonical JID
	SenderJID   string // Canonical JID
	Text        string
	Timestamp   time.Time
	IsFromMe    bool
	MessageType string
}

// represents a message with sender names (from view)
type MessageWithNames struct {
	Message
	SenderPushName    string // Current WhatsApp display name (from push_names table)
	SenderContactName string // Current saved contact name (from chats table)
	ChatName          string // Current chat name (for display)
}

// messages operations manager
type MessageStore struct {
	db *sql.DB
}

// message store constructor
func NewMessageStore(db *sql.DB) *MessageStore {
	return &MessageStore{db: db}
}

// saves a WhatsApp message to database
func (s *MessageStore) SaveMessage(msg Message) error {
	query := `
	INSERT OR REPLACE INTO messages
	(id, chat_jid, sender_jid, text, timestamp, is_from_me, message_type)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.Exec(
		query,
		msg.ID,
		msg.ChatJID,
		msg.SenderJID,
		msg.Text,
		msg.Timestamp.Unix(),
		msg.IsFromMe,
		msg.MessageType,
	)

	if err != nil {
		return fmt.Errorf("failed to save message: %w", err)
	}

	return nil
}

// saves multiple messages (optimized for history sync)
func (s *MessageStore) SaveBulk(messages []Message) error {
	tx, err := s.db.Begin()

	if err != nil {
		return err
	}

	defer tx.Rollback()

	stmt, err := tx.Prepare(`
	INSERT OR REPLACE INTO messages
	(id, chat_jid, sender_jid, text, timestamp, is_from_me, message_type)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}

	defer stmt.Close()

	for _, msg := range messages {
		_, err := stmt.Exec(
			msg.ID,
			msg.ChatJID,
			msg.SenderJID,
			msg.Text,
			msg.Timestamp.Unix(),
			msg.IsFromMe,
			msg.MessageType,
		)

		if err != nil {
			return fmt.Errorf("failed to insert message %s: %w", msg.ID, err)
		}
	}

	return tx.Commit()

}

// get messages by text
func (s *MessageStore) SearchMessages(q string, limit int) ([]Message, error) {
	query := `
	SELECT id, chat_jid, sender_jid, text, timestamp, is_from_me, message_type
	FROM messages
	WHERE text LIKE ?
	ORDER BY timestamp DESC
	LIMIT ?
	`

	rows, err := s.db.Query(query, "%"+q+"%", limit)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	return s.scanMessages(rows)
}

// get messages from a chat
func (s *MessageStore) GetChatMessages(chatJID string, limit int, offset int) ([]Message, error) {
	query := `
	SELECT id, chat_jid, sender_jid, text, timestamp, is_from_me, message_type
	FROM messages
	WHERE chat_jid = ?
	ORDER BY timestamp DESC
	LIMIT ? OFFSET ?
	`

	rows, err := s.db.Query(query, chatJID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanMessages(rows)
}

// get a message by id
func (s *MessageStore) GetMessageByID(messageID string) (*Message, error) {
	query := `
	SELECT id, chat_jid, sender_jid, text, timestamp, is_from_me, message_type
	FROM messages
	WHERE id = ?
	`

	row := s.db.QueryRow(query, messageID)

	var msg Message
	var timestampUnix int64

	err := row.Scan(
		&msg.ID,
		&msg.ChatJID,
		&msg.SenderJID,
		&msg.Text,
		&timestampUnix,
		&msg.IsFromMe,
		&msg.MessageType,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	msg.Timestamp = time.Unix(timestampUnix, 0)

	return &msg, nil
}

// helper to convert rows cursor into actual message objects
func (s *MessageStore) scanMessages(rows *sql.Rows) ([]Message, error) {
	var messages []Message

	for rows.Next() {
		var msg Message
		var timestampUnix int64

		err := rows.Scan(
			&msg.ID,
			&msg.ChatJID,
			&msg.SenderJID,
			&msg.Text,
			&timestampUnix,
			&msg.IsFromMe,
			&msg.MessageType,
		)
		if err != nil {
			return nil, err
		}

		msg.Timestamp = time.Unix(timestampUnix, 0)
		messages = append(messages, msg)
	}

	return messages, rows.Err()
}

// SearchMessagesWithNames searches messages and includes sender names from view
func (s *MessageStore) SearchMessagesWithNames(q string, limit int) ([]MessageWithNames, error) {
	query := `
	SELECT id, chat_jid, sender_jid, sender_push_name, sender_contact_name, chat_name,
	       text, timestamp, is_from_me, message_type
	FROM messages_with_names
	WHERE text LIKE ?
	ORDER BY timestamp DESC
	LIMIT ?
	`

	rows, err := s.db.Query(query, "%"+q+"%", limit)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	return s.scanMessagesWithNames(rows)
}

// GetChatMessagesWithNames gets chat messages and includes sender names from view
func (s *MessageStore) GetChatMessagesWithNames(chatJID string, limit int, offset int) ([]MessageWithNames, error) {
	query := `
	SELECT id, chat_jid, sender_jid, sender_push_name, sender_contact_name, chat_name,
	       text, timestamp, is_from_me, message_type
	FROM messages_with_names
	WHERE chat_jid = ?
	ORDER BY timestamp DESC
	LIMIT ? OFFSET ?
	`

	rows, err := s.db.Query(query, chatJID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanMessagesWithNames(rows)
}

// helper to scan messages with names from view
func (s *MessageStore) scanMessagesWithNames(rows *sql.Rows) ([]MessageWithNames, error) {
	var messages []MessageWithNames

	for rows.Next() {
		var msg MessageWithNames
		var timestampUnix int64

		err := rows.Scan(
			&msg.ID,
			&msg.ChatJID,
			&msg.SenderJID,
			&msg.SenderPushName,
			&msg.SenderContactName,
			&msg.ChatName,
			&msg.Text,
			&timestampUnix,
			&msg.IsFromMe,
			&msg.MessageType,
		)
		if err != nil {
			return nil, err
		}

		msg.Timestamp = time.Unix(timestampUnix, 0)
		messages = append(messages, msg)
	}

	return messages, rows.Err()
}
