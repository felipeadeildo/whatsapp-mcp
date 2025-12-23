package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// represents a conversation
type Chat struct {
	JIDPN           *string // JID in PN format (nullable)
	JIDLID          *string // JID in LID format (nullable)
	JID             string  // canonical JID (auto-generated, read-only)
	PushName        string  // Sender's WhatsApp display name (from PushName in messages)
	ContactName     string  // Saved contact name (from WhatsApp contact store)
	LastMessageTime time.Time
	UnreadCount     int
	IsGroup         bool
}

// findExistingChat checks if a chat already exists with either PN or LID format
// returns the existing chat if found, nil otherwise
func (s *MessageStore) findExistingChat(jidPN, jidLID string) (*Chat, error) {
	query := `
	SELECT jid_pn, jid_lid, jid, push_name, contact_name, last_message_time, unread_count, is_group
	FROM chats
	WHERE jid_pn = ? OR jid_pn = ? OR jid_lid = ? OR jid_lid = ? OR jid = ? OR jid = ?
	LIMIT 1
	`

	row := s.db.QueryRow(query, jidPN, jidLID, jidPN, jidLID, jidPN, jidLID)

	var chat Chat
	var lastMsgUnix int64

	err := row.Scan(
		&chat.JIDPN,
		&chat.JIDLID,
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

// saves/updates chat information with deduplication
func (s *MessageStore) SaveChat(chat Chat) error {
	// check for existing chat in any format to avoid duplicates
	pn := ""
	if chat.JIDPN != nil {
		pn = *chat.JIDPN
	}
	lid := ""
	if chat.JIDLID != nil {
		lid = *chat.JIDLID
	}

	// validate: at least one JID must be provided (CHECK constraint)
	if chat.JIDPN == nil && chat.JIDLID == nil {
		return fmt.Errorf("chat must have at least one JID (PN or LID)")
	}

	existing, err := s.findExistingChat(pn, lid)
	if err != nil {
		return err
	}

	// if found, merge with existing (prefer non-empty names)
	if existing != nil {
		// keep existing JID formats if they have them
		if existing.JIDPN != nil && chat.JIDPN == nil {
			chat.JIDPN = existing.JIDPN
		}
		if existing.JIDLID != nil && chat.JIDLID == nil {
			chat.JIDLID = existing.JIDLID
		}
		// prefer non-empty names for both fields
		if chat.PushName == "" && existing.PushName != "" {
			chat.PushName = existing.PushName
		}
		if chat.ContactName == "" && existing.ContactName != "" {
			chat.ContactName = existing.ContactName
		}
	}

	// compute canonical JID (COALESCE of PN and LID)
	canonicalJID := ""
	if chat.JIDPN != nil {
		canonicalJID = *chat.JIDPN
	} else if chat.JIDLID != nil {
		canonicalJID = *chat.JIDLID
	}

	// double-check: canonical JID must not be empty
	if canonicalJID == "" {
		return fmt.Errorf("canonical JID cannot be empty")
	}

	query := `
	INSERT INTO chats (jid, jid_pn, jid_lid, push_name, contact_name, last_message_time, unread_count, is_group)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(jid) DO UPDATE SET
	    jid_pn = COALESCE(excluded.jid_pn, chats.jid_pn),
	    jid_lid = COALESCE(excluded.jid_lid, chats.jid_lid),
	    push_name = COALESCE(NULLIF(excluded.push_name, ''), chats.push_name),
	    contact_name = COALESCE(NULLIF(excluded.contact_name, ''), chats.contact_name),
	    last_message_time = excluded.last_message_time,
	    is_group = excluded.is_group
	`

	_, err = s.db.Exec(
		query,
		canonicalJID,
		chat.JIDPN,
		chat.JIDLID,
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
	SELECT jid_pn, jid_lid, jid, push_name, contact_name, last_message_time, unread_count, is_group
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
			&chat.JIDPN,
			&chat.JIDLID,
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
