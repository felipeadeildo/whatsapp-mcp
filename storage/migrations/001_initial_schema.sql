-- Migration: 001_initial_schema
-- Description: Initial database schema for WhatsApp MCP
-- Previous: none
-- Version: 001
-- Created: 2026-01-04

-- Main messages table
CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY, -- Unique WhatsApp message ID

    -- Canonical JIDs
    chat_jid TEXT NOT NULL,   -- Chat JID in canonical format
    sender_jid TEXT NOT NULL, -- Sender JID in canonical format

    -- Message data
    text TEXT, -- Text content (null for media)
    timestamp INTEGER NOT NULL, -- Unix timestamp
    is_from_me BOOLEAN NOT NULL, -- true if I sent it
    message_type TEXT NOT NULL, -- 'text', 'image', 'video', etc
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    -- Relationship with chats
    FOREIGN KEY (chat_jid) REFERENCES chats(jid) ON DELETE CASCADE
);

-- Indexes for fast search
CREATE INDEX IF NOT EXISTS idx_chat_timestamp ON messages(chat_jid, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_text_search ON messages(text) WHERE text IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sender ON messages(sender_jid);

-- Media metadata table
CREATE TABLE IF NOT EXISTS media_metadata (
    message_id TEXT PRIMARY KEY,

    -- File information
    file_path TEXT,                   -- Relative path from data/media/ (null if not downloaded)
    file_name TEXT NOT NULL,          -- Original filename from WhatsApp
    file_size INTEGER NOT NULL,       -- Size in bytes
    mime_type TEXT NOT NULL,          -- MIME type (image/jpeg, video/mp4, etc.)

    -- Media-specific metadata
    width INTEGER,                    -- For images/videos
    height INTEGER,                   -- For images/videos
    duration INTEGER,                 -- For audio/video (seconds)

    -- WhatsApp metadata (for download)
    media_key BLOB,
    direct_path TEXT,
    file_sha256 BLOB,
    file_enc_sha256 BLOB,

    -- Download tracking
    download_status TEXT NOT NULL DEFAULT 'pending',  -- pending, downloaded, failed, expired
    download_timestamp INTEGER,
    download_error TEXT,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    FOREIGN KEY (message_id) REFERENCES messages(id) ON DELETE CASCADE
);

-- Indexes for media queries
CREATE INDEX IF NOT EXISTS idx_media_message ON media_metadata(message_id);
CREATE INDEX IF NOT EXISTS idx_media_status ON media_metadata(download_status);
CREATE INDEX IF NOT EXISTS idx_media_type ON media_metadata(mime_type);

-- Chats table (conversations)
CREATE TABLE IF NOT EXISTS chats (
    jid TEXT PRIMARY KEY NOT NULL, -- Canonical JID

    push_name TEXT, -- Sender's WhatsApp display name or group name
    contact_name TEXT, -- Saved contact name (from WhatsApp contact store)
    last_message_time INTEGER, -- Last message timestamp
    unread_count INTEGER DEFAULT 0,
    is_group BOOLEAN DEFAULT FALSE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Push names table (WhatsApp display names)
CREATE TABLE IF NOT EXISTS push_names (
    jid TEXT PRIMARY KEY, -- User JID (canonical format)
    push_name TEXT NOT NULL, -- WhatsApp display name
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')) -- Last update timestamp
);

-- Group participants table
CREATE TABLE IF NOT EXISTS group_participants (
    group_jid TEXT NOT NULL,
    participant_jid TEXT NOT NULL, -- Canonical participant JID

    is_admin BOOLEAN DEFAULT FALSE,
    joined_at INTEGER,

    PRIMARY KEY (group_jid, participant_jid),
    FOREIGN KEY (group_jid) REFERENCES chats(jid) ON DELETE CASCADE
);

-- View for querying messages with sender names (current names from push_names and chats)
CREATE VIEW IF NOT EXISTS messages_with_names AS
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
