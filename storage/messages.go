package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// Message represents a WhatsApp message.
type Message struct {
	ID          string
	ChatJID     string // Canonical JID
	SenderJID   string // Canonical JID
	Text        string
	Timestamp   time.Time
	IsFromMe    bool
	MessageType string
}

// MessageWithNames represents a message with sender and chat names from the database view.
type MessageWithNames struct {
	Message
	SenderPushName    string         // Current WhatsApp display name (from push_names table)
	SenderContactName string         // Current saved contact name (from chats table)
	ChatName          string         // Current chat name (for display)
	MediaMetadata     *MediaMetadata // Associated media metadata (null if no media)
}

// MessageStore handles message operations on the database.
type MessageStore struct {
	db *sql.DB
}

// NewMessageStore creates a new message store instance.
func NewMessageStore(db *sql.DB) *MessageStore {
	return &MessageStore{db: db}
}

// SaveMessage saves a WhatsApp message to the database.
func (s *MessageStore) SaveMessage(msg Message) error {
	query := `
	INSERT INTO messages
	(id, chat_jid, sender_jid, text, timestamp, is_from_me, message_type)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		chat_jid = excluded.chat_jid,
		sender_jid = excluded.sender_jid,
		text = excluded.text,
		timestamp = excluded.timestamp,
		is_from_me = excluded.is_from_me,
		message_type = excluded.message_type
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

// SaveBulk saves multiple messages in a single transaction.
// This is optimized for history sync operations.
func (s *MessageStore) SaveBulk(messages []Message) error {
	tx, err := s.db.Begin()

	if err != nil {
		return err
	}

	defer tx.Rollback()

	stmt, err := tx.Prepare(`
	INSERT INTO messages
	(id, chat_jid, sender_jid, text, timestamp, is_from_me, message_type)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		chat_jid = excluded.chat_jid,
		sender_jid = excluded.sender_jid,
		text = excluded.text,
		timestamp = excluded.timestamp,
		is_from_me = excluded.is_from_me,
		message_type = excluded.message_type
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

// SearchMessages searches messages by text content.
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

// GetChatMessages retrieves messages from a specific chat.
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

// GetMessageByID retrieves a message by its ID.
// It returns nil if the message is not found.
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

// GetOldestMessage retrieves the oldest message from a specific chat.
// This is used for history sync requests.
func (s *MessageStore) GetOldestMessage(chatJID string) (*Message, error) {
	query := `
	SELECT id, chat_jid, sender_jid, text, timestamp, is_from_me, message_type
	FROM messages
	WHERE chat_jid = ?
	ORDER BY timestamp ASC
	LIMIT 1
	`

	row := s.db.QueryRow(query, chatJID)

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

// GetChatMessagesOlderThan retrieves messages older than a specific timestamp.
// This is used for retrieving newly loaded messages from history sync.
func (s *MessageStore) GetChatMessagesOlderThan(chatJID string, timestamp time.Time, limit int) ([]MessageWithNames, error) {
	query := `
	SELECT id, chat_jid, sender_jid, sender_push_name, sender_contact_name, chat_name,
	       text, timestamp, is_from_me, message_type,
	       media_file_path, media_file_name, media_file_size, media_mime_type,
	       media_width, media_height, media_duration, media_download_status,
	       media_download_timestamp, media_download_error
	FROM messages_with_names
	WHERE chat_jid = ? AND timestamp < ?
	ORDER BY timestamp DESC
	LIMIT ?
	`

	rows, err := s.db.Query(query, chatJID, timestamp.Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanMessagesWithNames(rows)
}

// GetChatMessagesWithNamesFiltered retrieves chat messages with advanced filtering.
func (s *MessageStore) GetChatMessagesWithNamesFiltered(
	chatJID string,
	limit int,
	beforeTimestamp *time.Time,
	afterTimestamp *time.Time,
	senderJID string,
) ([]MessageWithNames, error) {
	query := `
	SELECT id, chat_jid, sender_jid, sender_push_name, sender_contact_name, chat_name,
	       text, timestamp, is_from_me, message_type,
	       media_file_path, media_file_name, media_file_size, media_mime_type,
	       media_width, media_height, media_duration, media_download_status,
	       media_download_timestamp, media_download_error
	FROM messages_with_names
	WHERE chat_jid = ?
	`

	args := []any{chatJID}

	// add timestamp filters
	if beforeTimestamp != nil {
		query += " AND timestamp < ?"
		args = append(args, beforeTimestamp.Unix())
	}

	if afterTimestamp != nil {
		query += " AND timestamp > ?"
		args = append(args, afterTimestamp.Unix())
	}

	// add sender filter
	if senderJID != "" {
		query += " AND sender_jid = ?"
		args = append(args, senderJID)
	}

	query += " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanMessagesWithNames(rows)
}

// SaveMessageProto stores the serialized protobuf for a sent message.
// This is used for retry receipt handling when the recipient can't decrypt.
func (s *MessageStore) SaveMessageProto(messageID string, protoBytes []byte) error {
	_, err := s.db.Exec(
		"UPDATE messages SET message_proto = ? WHERE id = ?",
		protoBytes, messageID,
	)
	return err
}

// GetMessageProto retrieves the serialized protobuf for a message.
// Returns nil if the message or proto is not found.
func (s *MessageStore) GetMessageProto(messageID string) ([]byte, error) {
	var protoBytes []byte
	err := s.db.QueryRow(
		"SELECT message_proto FROM messages WHERE id = ?",
		messageID,
	).Scan(&protoBytes)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return protoBytes, nil
}

// scanMessages converts SQL rows into Message objects.
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

// SearchMessagesWithNamesFiltered searches messages with pattern matching and sender filtering.
// It uses GLOB patterns if useGlob is true, otherwise uses LIKE for fuzzy matching.
func (s *MessageStore) SearchMessagesWithNamesFiltered(
	query string,
	useGlob bool,
	senderJID string,
	limit int,
) ([]MessageWithNames, error) {
	var sqlQuery string
	var args []any

	// choose LIKE or GLOB based on pattern type
	if useGlob {
		sqlQuery = `
		SELECT id, chat_jid, sender_jid, sender_push_name, sender_contact_name, chat_name,
		       text, timestamp, is_from_me, message_type,
		       media_file_path, media_file_name, media_file_size, media_mime_type,
		       media_width, media_height, media_duration, media_download_status,
		       media_download_timestamp, media_download_error
		FROM messages_with_names
		WHERE text GLOB ?
		`
		args = append(args, query)
	} else {
		sqlQuery = `
		SELECT id, chat_jid, sender_jid, sender_push_name, sender_contact_name, chat_name,
		       text, timestamp, is_from_me, message_type,
		       media_file_path, media_file_name, media_file_size, media_mime_type,
		       media_width, media_height, media_duration, media_download_status,
		       media_download_timestamp, media_download_error
		FROM messages_with_names
		WHERE text LIKE ?
		`
		args = append(args, "%"+query+"%")
	}

	// add sender filter
	if senderJID != "" {
		sqlQuery += " AND sender_jid = ?"
		args = append(args, senderJID)
	}

	sqlQuery += " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanMessagesWithNames(rows)
}

// SearchMessagesWithNames searches messages and includes sender names from view
func (s *MessageStore) SearchMessagesWithNames(q string, limit int) ([]MessageWithNames, error) {
	query := `
	SELECT id, chat_jid, sender_jid, sender_push_name, sender_contact_name, chat_name,
	       text, timestamp, is_from_me, message_type,
	       media_file_path, media_file_name, media_file_size, media_mime_type,
	       media_width, media_height, media_duration, media_download_status,
	       media_download_timestamp, media_download_error
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
	       text, timestamp, is_from_me, message_type,
	       media_file_path, media_file_name, media_file_size, media_mime_type,
	       media_width, media_height, media_duration, media_download_status,
	       media_download_timestamp, media_download_error
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

// scanMessagesWithNames converts SQL rows into MessageWithNames objects.
func (s *MessageStore) scanMessagesWithNames(rows *sql.Rows) ([]MessageWithNames, error) {
	var messages []MessageWithNames

	for rows.Next() {
		var msg MessageWithNames
		var timestampUnix int64

		// media metadata fields (nullable)
		var mediaFilePath, mediaFileName, mediaMimeType sql.NullString
		var mediaFileSize sql.NullInt64
		var mediaWidth, mediaHeight, mediaDuration sql.NullInt64
		var mediaDownloadStatus, mediaDownloadError sql.NullString
		var mediaDownloadTimestamp sql.NullInt64

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
			// media metadata fields
			&mediaFilePath,
			&mediaFileName,
			&mediaFileSize,
			&mediaMimeType,
			&mediaWidth,
			&mediaHeight,
			&mediaDuration,
			&mediaDownloadStatus,
			&mediaDownloadTimestamp,
			&mediaDownloadError,
		)
		if err != nil {
			return nil, err
		}

		msg.Timestamp = time.Unix(timestampUnix, 0)

		// populate media metadata if present
		if mediaFileName.Valid && mediaMimeType.Valid {
			meta := &MediaMetadata{
				MessageID:      msg.ID,
				FileName:       mediaFileName.String,
				FileSize:       mediaFileSize.Int64,
				MimeType:       mediaMimeType.String,
				DownloadStatus: "pending",
			}

			if mediaFilePath.Valid {
				meta.FilePath = mediaFilePath.String
			}
			if mediaWidth.Valid {
				w := int(mediaWidth.Int64)
				meta.Width = &w
			}
			if mediaHeight.Valid {
				h := int(mediaHeight.Int64)
				meta.Height = &h
			}
			if mediaDuration.Valid {
				d := int(mediaDuration.Int64)
				meta.Duration = &d
			}
			if mediaDownloadStatus.Valid {
				meta.DownloadStatus = mediaDownloadStatus.String
			}
			if mediaDownloadTimestamp.Valid {
				ts := time.Unix(mediaDownloadTimestamp.Int64, 0)
				meta.DownloadTimestamp = &ts
			}
			if mediaDownloadError.Valid {
				meta.DownloadError = mediaDownloadError.String
			}

			msg.MediaMetadata = meta
		}

		messages = append(messages, msg)
	}

	return messages, rows.Err()
}
