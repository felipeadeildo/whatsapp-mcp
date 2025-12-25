package storage

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SaveChat saves or updates a chat (with COALESCE logic to preserve non-empty values)
func (s *MessageStore) SaveChat(chat Chat) error {
	if chat.JID == "" {
		return fmt.Errorf("chat JID cannot be empty")
	}

	return s.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "jid"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"push_name":         gorm.Expr("CASE WHEN excluded.push_name != '' THEN excluded.push_name ELSE chats.push_name END"),
			"contact_name":      gorm.Expr("CASE WHEN excluded.contact_name != '' THEN excluded.contact_name ELSE chats.contact_name END"),
			"last_message_time": clause.Column{Name: "last_message_time"},
			"unread_count":      clause.Column{Name: "unread_count"},
			"is_group":          clause.Column{Name: "is_group"},
		}),
	}).Create(&chat).Error
}

// GetChatByJID retrieves a chat by JID
func (s *MessageStore) GetChatByJID(jid string) (*Chat, error) {
	var chat Chat
	err := s.db.Where("jid = ?", jid).First(&chat).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &chat, nil
}

// ListChats returns all chats ordered by last message time
func (s *MessageStore) ListChats(limit int) ([]Chat, error) {
	var chats []Chat

	err := s.db.
		Order("last_message_time DESC").
		Limit(limit).
		Find(&chats).Error

	if err != nil {
		return nil, err
	}

	return chats, nil
}

// SearchChats searches chats by name or JID
func (s *MessageStore) SearchChats(search string, limit int) ([]Chat, error) {
	var chats []Chat
	searchPattern := "%" + search + "%"

	err := s.db.
		Where("push_name LIKE ? OR contact_name LIKE ? OR jid LIKE ?",
			searchPattern, searchPattern, searchPattern).
		Order("last_message_time DESC").
		Limit(limit).
		Find(&chats).Error

	if err != nil {
		return nil, err
	}

	return chats, nil
}
