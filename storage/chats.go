package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// represents a conversation
type Chat struct {
	JID             string // canonical JID (required)
	PushName        string // sender's WhatsApp display name (from PushName in messages)
	ContactName     string // saved contact name (from WhatsApp contact store)
	LastMessageTime time.Time
	UnreadCount     int
	IsGroup         bool
}

// retrieves a chat by its canonical JID
// returns the chat if found, nil otherwise
func (s *MessageStore) GetChatByJID(jid string) (*Chat, error) {
	query := `
	SELECT jid, push_name, contact_name, last_message_time, unread_count, is_group
	FROM chats
	WHERE jid = ?
	`

	row := s.db.QueryRow(query, jid)

	var chat Chat
	var lastMsgUnix int64

	err := row.Scan(
		&chat.JID,
		&chat.PushName,
		&chat.ContactName,
		&lastMsgUnix,
		&chat.UnreadCount,
		&chat.IsGroup,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	chat.LastMessageTime = time.Unix(lastMsgUnix, 0)
	return &chat, nil
}

// saves/updates chat information
func (s *MessageStore) SaveChat(chat Chat) error {
	if chat.JID == "" {
		return fmt.Errorf("chat JID cannot be empty")
	}

	query := `
	INSERT INTO chats (jid, push_name, contact_name, last_message_time, unread_count, is_group)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(jid) DO UPDATE SET
	    push_name = COALESCE(NULLIF(excluded.push_name, ''), chats.push_name),
	    contact_name = COALESCE(NULLIF(excluded.contact_name, ''), chats.contact_name),
	    last_message_time = excluded.last_message_time,
	    unread_count = excluded.unread_count,
	    is_group = excluded.is_group
	`

	_, err := s.db.Exec(
		query,
		chat.JID,
		chat.PushName,
		chat.ContactName,
		chat.LastMessageTime.Unix(),
		chat.UnreadCount,
		chat.IsGroup,
	)

	return err
}

// return all chats ordered by last message timestamp
func (s *MessageStore) ListChats(limit int) ([]Chat, error) {
	query := `
	SELECT jid, push_name, contact_name, last_message_time, unread_count, is_group
	FROM chats
	ORDER BY last_message_time DESC
	LIMIT ?
	`

	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chats []Chat
	for rows.Next() {
		var chat Chat
		var lastMsgUnix int64

		err := rows.Scan(
			&chat.JID,
			&chat.PushName,
			&chat.ContactName,
			&lastMsgUnix,
			&chat.UnreadCount,
			&chat.IsGroup,
		)
		if err != nil {
			return nil, err
		}

		chat.LastMessageTime = time.Unix(lastMsgUnix, 0)
		chats = append(chats, chat)
	}

	return chats, rows.Err()
}

// searches chats with pattern matching support
func (s *MessageStore) SearchChatsFiltered(search string, useGlob bool, limit int) ([]Chat, error) {
	var query string
	var searchPattern string

	// choose LIKE or GLOB based on pattern type
	if useGlob {
		query = `
		SELECT jid, push_name, contact_name, last_message_time, unread_count, is_group
		FROM chats
		WHERE push_name GLOB ? OR contact_name GLOB ? OR jid GLOB ?
		ORDER BY last_message_time DESC
		LIMIT ?
		`
		searchPattern = search
	} else {
		query = `
		SELECT jid, push_name, contact_name, last_message_time, unread_count, is_group
		FROM chats
		WHERE push_name LIKE ? OR contact_name LIKE ? OR jid LIKE ?
		ORDER BY last_message_time DESC
		LIMIT ?
		`
		searchPattern = "%" + search + "%"
	}

	rows, err := s.db.Query(query, searchPattern, searchPattern, searchPattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chats []Chat
	for rows.Next() {
		var chat Chat
		var lastMsgUnix int64

		err := rows.Scan(
			&chat.JID,
			&chat.PushName,
			&chat.ContactName,
			&lastMsgUnix,
			&chat.UnreadCount,
			&chat.IsGroup,
		)
		if err != nil {
			return nil, err
		}

		chat.LastMessageTime = time.Unix(lastMsgUnix, 0)
		chats = append(chats, chat)
	}

	return chats, rows.Err()
}

// search chats by name or JID with fuzzy matching
func (s *MessageStore) SearchChats(search string, limit int) ([]Chat, error) {
	query := `
	SELECT jid, push_name, contact_name, last_message_time, unread_count, is_group
	FROM chats
	WHERE push_name LIKE ? OR contact_name LIKE ? OR jid LIKE ?
	ORDER BY last_message_time DESC
	LIMIT ?
	`

	searchPattern := "%" + search + "%"
	rows, err := s.db.Query(query, searchPattern, searchPattern, searchPattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chats []Chat
	for rows.Next() {
		var chat Chat
		var lastMsgUnix int64

		err := rows.Scan(
			&chat.JID,
			&chat.PushName,
			&chat.ContactName,
			&lastMsgUnix,
			&chat.UnreadCount,
			&chat.IsGroup,
		)
		if err != nil {
			return nil, err
		}

		chat.LastMessageTime = time.Unix(lastMsgUnix, 0)
		chats = append(chats, chat)
	}

	return chats, rows.Err()
}
