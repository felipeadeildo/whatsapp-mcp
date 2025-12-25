package storage

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MessageStore manages message operations using GORM
type MessageStore struct {
	db *gorm.DB
}

// NewMessageStore creates a new message store
func NewMessageStore(db *gorm.DB) *MessageStore {
	return &MessageStore{db: db}
}

// SaveMessage saves or updates a message using UPSERT
func (s *MessageStore) SaveMessage(msg Message) error {
	return s.db.Clauses(clause.OnConflict{
		UpdateAll: true, // Update all columns on conflict
	}).Create(&msg).Error
}

// SaveBulk saves multiple messages in a transaction (for history sync)
func (s *MessageStore) SaveBulk(messages []Message) error {
	if len(messages) == 0 {
		return nil
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{
			UpdateAll: true,
		}).CreateInBatches(messages, 100).Error
	})
}

// GetMessageByID retrieves a single message by ID
func (s *MessageStore) GetMessageByID(messageID string) (*Message, error) {
	var msg Message
	err := s.db.Where("id = ?", messageID).First(&msg).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &msg, nil
}

// GetChatMessages retrieves messages for a chat with pagination
func (s *MessageStore) GetChatMessages(chatJID string, limit int, offset int) ([]Message, error) {
	var messages []Message

	err := s.db.
		Where("chat_jid = ?", chatJID).
		Order("timestamp DESC").
		Limit(limit).
		Offset(offset).
		Find(&messages).Error

	if err != nil {
		return nil, err
	}

	return messages, nil
}

// SearchMessages searches messages by text content
func (s *MessageStore) SearchMessages(q string, limit int) ([]Message, error) {
	var messages []Message

	err := s.db.
		Where("text LIKE ?", "%"+q+"%").
		Order("timestamp DESC").
		Limit(limit).
		Find(&messages).Error

	if err != nil {
		return nil, err
	}

	return messages, nil
}

// GetChatMessagesWithNames retrieves messages with sender/chat names (replaces messages_with_names view)
func (s *MessageStore) GetChatMessagesWithNames(chatJID string, limit int, offset int) ([]MessageWithNames, error) {
	var results []MessageWithNames

	err := s.db.
		Table("messages").
		Select(`
			messages.*,
			COALESCE(push_names.push_name, '') as sender_push_name,
			COALESCE(sender_chat.contact_name, '') as sender_contact_name,
			COALESCE(chat.contact_name, chat.push_name, messages.chat_jid) as chat_name
		`).
		Joins("LEFT JOIN push_names ON messages.sender_jid = push_names.jid").
		Joins("LEFT JOIN chats AS sender_chat ON messages.sender_jid = sender_chat.jid").
		Joins("LEFT JOIN chats AS chat ON messages.chat_jid = chat.jid").
		Where("messages.chat_jid = ?", chatJID).
		Order("messages.timestamp DESC").
		Limit(limit).
		Offset(offset).
		Scan(&results).Error

	return results, err
}

// SearchMessagesWithNames searches messages with sender/chat names
func (s *MessageStore) SearchMessagesWithNames(q string, limit int) ([]MessageWithNames, error) {
	var results []MessageWithNames

	err := s.db.
		Table("messages").
		Select(`
			messages.*,
			COALESCE(push_names.push_name, '') as sender_push_name,
			COALESCE(sender_chat.contact_name, '') as sender_contact_name,
			COALESCE(chat.contact_name, chat.push_name, messages.chat_jid) as chat_name
		`).
		Joins("LEFT JOIN push_names ON messages.sender_jid = push_names.jid").
		Joins("LEFT JOIN chats AS sender_chat ON messages.sender_jid = sender_chat.jid").
		Joins("LEFT JOIN chats AS chat ON messages.chat_jid = chat.jid").
		Where("messages.text LIKE ?", "%"+q+"%").
		Order("messages.timestamp DESC").
		Limit(limit).
		Scan(&results).Error

	return results, err
}
