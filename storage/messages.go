package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// represents a whatsapp message
type Message struct {
	ID                string
	ChatJIDPN         *string // Chat JID in PN format (nullable)
	ChatJIDLID        *string // Chat JID in LID format (nullable)
	ChatJID           string  // Canonical JID auto-generated (read-only)
	SenderJIDPN       *string // Sender JID in PN format (nullable)
	SenderJIDLID      *string // Sender JID in LID format (nullable)
	SenderJID         string  // Canonical JID auto-generated (read-only)
	SenderPushName    string  // Sender's WhatsApp display name (from PushName)
	SenderContactName string  // Sender's saved contact name (from contact store)
	Text              string
	Timestamp         time.Time
	IsFromMe          bool
	MessageType       string
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
	(id, chat_jid_pn, chat_jid_lid, sender_jid_pn, sender_jid_lid, sender_push_name, sender_contact_name, text, timestamp, is_from_me, message_type)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.Exec(
		query,
		msg.ID,
		msg.ChatJIDPN,
		msg.ChatJIDLID,
		msg.SenderJIDPN,
		msg.SenderJIDLID,
		msg.SenderPushName,
		msg.SenderContactName,
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
	(id, chat_jid_pn, chat_jid_lid, sender_jid_pn, sender_jid_lid, sender_push_name, sender_contact_name, text, timestamp, is_from_me, message_type)
	values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}

	defer stmt.Close()

	for _, msg := range messages {
		_, err := stmt.Exec(
			msg.ID,
			msg.ChatJIDPN,
			msg.ChatJIDLID,
			msg.SenderJIDPN,
			msg.SenderJIDLID,
			msg.SenderPushName,
			msg.SenderContactName,
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
	SELECT id, chat_jid_pn, chat_jid_lid, chat_jid, sender_jid_pn, sender_jid_lid, sender_jid,
	       sender_push_name, sender_contact_name, text, timestamp, is_from_me, message_type
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

// get messages from a chat (matches by any JID format)
func (s *MessageStore) GetChatMessages(chatJID string, limit int, offset int) ([]Message, error) {
	query := `
	SELECT id, chat_jid_pn, chat_jid_lid, chat_jid, sender_jid_pn, sender_jid_lid, sender_jid,
	       sender_push_name, sender_contact_name, text, timestamp, is_from_me, message_type
	FROM messages
	WHERE chat_jid = ? OR chat_jid_pn = ? OR chat_jid_lid = ?
	ORDER BY timestamp DESC
	LIMIT ? OFFSET ?
	`

	rows, err := s.db.Query(query, chatJID, chatJID, chatJID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanMessages(rows)
}

// get a message by id
func (s *MessageStore) GetMessageByID(messageID string) (*Message, error) {
	query := `
	SELECT id, chat_jid_pn, chat_jid_lid, chat_jid, sender_jid_pn, sender_jid_lid, sender_jid,
	       sender_push_name, sender_contact_name, text, timestamp, is_from_me, message_type
	FROM messages
	WHERE id = ?
	`

	row := s.db.QueryRow(query, messageID)

	var msg Message
	var timestampUnix int64

	err := row.Scan(
		&msg.ID,
		&msg.ChatJIDPN,
		&msg.ChatJIDLID,
		&msg.ChatJID,
		&msg.SenderJIDPN,
		&msg.SenderJIDLID,
		&msg.SenderJID,
		&msg.SenderPushName,
		&msg.SenderContactName,
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
			&msg.ChatJIDPN,
			&msg.ChatJIDLID,
			&msg.ChatJID,
			&msg.SenderJIDPN,
			&msg.SenderJIDLID,
			&msg.SenderJID,
			&msg.SenderPushName,
			&msg.SenderContactName,
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
