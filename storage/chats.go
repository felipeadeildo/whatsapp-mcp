package storage

import (
	"time"
)

// represents a conversation
type Chat struct {
	JID             string
	Name            string
	LastMessageTime time.Time
	UnreadCount     int
	IsGroup         bool
}

// saves/updates a chat informations
func (s *MessageStore) SaveChat(chat Chat) error {
	query := `
        INSERT INTO chats (jid, name, last_message_time, unread_count, is_group)
        VALUES (?, ?, ?, ?, ?)
        ON CONFLICT(jid) DO UPDATE SET
            name = excluded.name,
            last_message_time = excluded.last_message_time,
            is_group = excluded.is_group
    `

	_, err := s.db.Exec(
		query,
		chat.JID,
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
        SELECT jid, name, last_message_time, unread_count, is_group
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
