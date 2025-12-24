package storage

import (
	"time"
)

// Message represents a WhatsApp message
type Message struct {
	ID          string    `gorm:"primaryKey;type:text"`
	ChatJID     string    `gorm:"type:text;not null;index:idx_chat_timestamp"`
	SenderJID   string    `gorm:"type:text;not null;index:idx_sender"`
	Text        string    `gorm:"type:text"`
	Timestamp   time.Time `gorm:"not null;index:idx_chat_timestamp,sort:desc"`
	IsFromMe    bool      `gorm:"not null"`
	MessageType string    `gorm:"type:text;not null"`
	CreatedAt   time.Time `gorm:"autoCreateTime"`

	// Relationships
	Chat   Chat  `gorm:"foreignKey:ChatJID;references:JID;constraint:OnDelete:CASCADE"`
	Sender *Chat `gorm:"foreignKey:SenderJID;references:JID"`
}

// Chat represents a WhatsApp conversation or contact
type Chat struct {
	JID             string    `gorm:"primaryKey;type:text"`
	PushName        string    `gorm:"type:text"`
	ContactName     string    `gorm:"type:text"`
	LastMessageTime time.Time `gorm:"type:integer"`
	UnreadCount     int       `gorm:"default:0"`
	IsGroup         bool      `gorm:"default:false"`
	CreatedAt       time.Time `gorm:"autoCreateTime"`

	// Relationships
	Messages          []Message          `gorm:"foreignKey:ChatJID"`
	SentMessages      []Message          `gorm:"foreignKey:SenderJID"`
	GroupParticipants []GroupParticipant `gorm:"foreignKey:GroupJID"`
}

// GetDisplayName returns the best available name
func (c Chat) GetDisplayName() string {
	if c.ContactName != "" {
		return c.ContactName
	}
	if c.PushName != "" {
		return c.PushName
	}
	return c.JID
}

// PushName represents a WhatsApp display name cache entry
type PushName struct {
	JID       string `gorm:"primaryKey;type:text"`
	PushName  string `gorm:"type:text;not null"`
	UpdatedAt int64  `gorm:"not null;autoUpdateTime:nano"`
}

// GroupParticipant represents a member of a WhatsApp group
type GroupParticipant struct {
	GroupJID       string `gorm:"primaryKey;type:text"`
	ParticipantJID string `gorm:"primaryKey;type:text"`
	IsAdmin        bool   `gorm:"default:false"`
	JoinedAt       int64  `gorm:"type:integer"`

	// Relationships
	Group Chat `gorm:"foreignKey:GroupJID;references:JID;constraint:OnDelete:CASCADE"`
}

// MessageWithNames is a DTO for enriched message queries (replaces messages_with_names view)
type MessageWithNames struct {
	Message
	SenderPushName    string `json:"sender_push_name"`
	SenderContactName string `json:"sender_contact_name"`
	ChatName          string `json:"chat_name"`
}

// GetSenderDisplayName returns the best available sender name
func (m MessageWithNames) GetSenderDisplayName() string {
	if m.SenderContactName != "" {
		return m.SenderContactName
	}
	if m.SenderPushName != "" {
		return m.SenderPushName
	}
	return m.SenderJID
}
