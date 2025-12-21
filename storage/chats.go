package storage

import (
	"database/sql"
	"time"
)

// represents a conversation
type Chat struct {
	JIDPN           *string // JID in PN format (nullable)
	JIDLID          *string // JID in LID format (nullable)
	JID             string  // canonical JID (auto-generated, read-only)
	Name            string
	LastMessageTime time.Time
	UnreadCount     int
	IsGroup         bool
}

// findExistingChat checks if a chat already exists with either PN or LID format
// returns the existing chat if found, nil otherwise
func (s *MessageStore) findExistingChat(jidPN, jidLID string) (*Chat, error) {
	query := `
	SELECT jid_pn, jid_lid, jid, name, last_message_time, unread_count, is_group
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
		&chat.Name,
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
		// prefer non-empty names
		if chat.Name == "" && existing.Name != "" {
			chat.Name = existing.Name
		}
	}

	// compute canonical JID (COALESCE of PN and LID)
	canonicalJID := ""
	if chat.JIDPN != nil {
		canonicalJID = *chat.JIDPN
	} else if chat.JIDLID != nil {
		canonicalJID = *chat.JIDLID
	}

	query := `
	INSERT INTO chats (jid, jid_pn, jid_lid, name, last_message_time, unread_count, is_group)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(jid) DO UPDATE SET
	    jid_pn = COALESCE(excluded.jid_pn, chats.jid_pn),
	    jid_lid = COALESCE(excluded.jid_lid, chats.jid_lid),
	    name = COALESCE(NULLIF(excluded.name, ''), chats.name),
	    last_message_time = excluded.last_message_time,
	    is_group = excluded.is_group
	`

	_, err = s.db.Exec(
		query,
		canonicalJID,
		chat.JIDPN,
		chat.JIDLID,
		chat.Name,
		chat.LastMessageTime.Unix(),
		chat.UnreadCount,
		chat.IsGroup,
	)

	return err
}

// return all chats ordered by last message timestamp
func (s *MessageStore) ListChats(limit int) ([]Chat, error) {
	query := `
	SELECT jid_pn, jid_lid, jid, name, last_message_time, unread_count, is_group
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
			&chat.Name,
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
