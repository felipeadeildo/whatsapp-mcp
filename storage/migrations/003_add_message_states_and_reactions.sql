-- Migration: 003_add_message_states_and_reactions
-- Description: add message states and reactions
-- Previous: 002
-- Version: 003
-- Created: 2026-04-27

-- Track when a message was edited or deleted (NULL = neither)
ALTER TABLE messages ADD COLUMN edited_at INTEGER;
ALTER TABLE messages ADD COLUMN deleted_at INTEGER;

-- Partial indexes for the rare-case lookups (most messages are neither edited nor deleted)
CREATE INDEX IF NOT EXISTS idx_messages_edited ON messages(edited_at) WHERE edited_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_messages_deleted ON messages(deleted_at) WHERE deleted_at IS NOT NULL;

-- Reactions: many reactions per message, one per (message_id, sender_jid).
-- Empty emoji string represents a reaction that was removed by the sender.
CREATE TABLE IF NOT EXISTS message_reactions (
    message_id TEXT NOT NULL,
    chat_jid   TEXT NOT NULL,
    sender_jid TEXT NOT NULL,
    emoji      TEXT NOT NULL,
    timestamp  INTEGER NOT NULL,
    PRIMARY KEY (message_id, sender_jid),
    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_reactions_message ON message_reactions(message_id);
CREATE INDEX IF NOT EXISTS idx_reactions_chat ON message_reactions(chat_jid);

-- Recreate messages_with_names view to expose edited_at and deleted_at columns.
-- The original view in 001_initial_schema.sql used CREATE VIEW IF NOT EXISTS, so
-- it was created once and remains; we drop and recreate to add the new columns.
DROP VIEW IF EXISTS messages_with_names;

CREATE VIEW messages_with_names AS
SELECT
    m.id,
    m.chat_jid,
    m.sender_jid,

    -- Get sender's current push name (WhatsApp display name)
    COALESCE(p.push_name, '') as sender_push_name,

    -- Get sender's current contact name (saved contact)
    COALESCE(c_sender.contact_name, '') as sender_contact_name,

    -- Get chat name (for display)
    COALESCE(
        c_chat.contact_name,  -- Saved contact name for DMs
        c_chat.push_name,     -- Push name for DMs or group name for groups
        m.chat_jid            -- Fallback to JID
    ) as chat_name,

    -- Original message fields
    m.text,
    m.timestamp,
    m.is_from_me,
    m.message_type,
    m.created_at,

    -- Message state fields (added in 003)
    m.edited_at,
    m.deleted_at,

    -- Media metadata fields (nullable)
    media.file_path as media_file_path,
    media.file_name as media_file_name,
    media.file_size as media_file_size,
    media.mime_type as media_mime_type,
    media.width as media_width,
    media.height as media_height,
    media.duration as media_duration,
    media.download_status as media_download_status,
    media.download_timestamp as media_download_timestamp,
    media.download_error as media_download_error
FROM messages m
LEFT JOIN push_names p ON m.sender_jid = p.jid
LEFT JOIN chats c_sender ON m.sender_jid = c_sender.jid
LEFT JOIN chats c_chat ON m.chat_jid = c_chat.jid
LEFT JOIN media_metadata media ON m.id = media.message_id;
