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
    m.created_at
FROM messages m
LEFT JOIN push_names p ON m.sender_jid = p.jid
LEFT JOIN chats c_sender ON m.sender_jid = c_sender.jid
LEFT JOIN chats c_chat ON m.chat_jid = c_chat.jid;
