-- Main messages table
CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY, -- Unique WhatsApp message ID

    -- JIDs in PN and LID formats (actual data)
    chat_jid_pn TEXT, -- Chat JID in PN format (@s.whatsapp.net)
    chat_jid_lid TEXT, -- Chat JID in LID format (@lid)
    sender_jid_pn TEXT, -- Sender JID in PN format
    sender_jid_lid TEXT, -- Sender JID in LID format

    -- Canonical JIDs generated automatically (for queries and FKs)
    chat_jid TEXT GENERATED ALWAYS AS (COALESCE(chat_jid_pn, chat_jid_lid)) STORED,
    sender_jid TEXT GENERATED ALWAYS AS (COALESCE(sender_jid_pn, sender_jid_lid)) STORED,

    -- Message data
    -- Note: sender names are retrieved via JOIN with push_names and chats tables
    -- This ensures we always show current names and eliminates data duplication
    text TEXT, -- Text content (null for media)
    timestamp INTEGER NOT NULL, -- Unix timestamp
    is_from_me BOOLEAN NOT NULL, -- true if I sent it
    message_type TEXT NOT NULL, -- 'text', 'image', 'video', etc
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    -- Relationship with chats
    FOREIGN KEY (chat_jid) REFERENCES chats(jid) ON DELETE CASCADE
);

-- Indexes for fast search
CREATE INDEX IF NOT EXISTS idx_chat_timestamp ON messages (chat_jid, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_text_search ON messages (text) WHERE text IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sender ON messages (sender_jid);

-- Indexes for alternative JIDs (only non-NULL values)
CREATE INDEX IF NOT EXISTS idx_messages_sender_pn ON messages(sender_jid_pn) WHERE sender_jid_pn IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_messages_sender_lid ON messages(sender_jid_lid) WHERE sender_jid_lid IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_messages_chat_pn ON messages(chat_jid_pn) WHERE chat_jid_pn IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_messages_chat_lid ON messages(chat_jid_lid) WHERE chat_jid_lid IS NOT NULL;

-- Chats table (conversations)
CREATE TABLE IF NOT EXISTS chats (
    -- JIDs in PN and LID formats (actual data)
    jid_pn TEXT, -- JID in PN format (@s.whatsapp.net)
    jid_lid TEXT, -- JID in LID format (@lid)

    -- Canonical JID (PRIMARY KEY, auto-updated by trigger)
    jid TEXT PRIMARY KEY,

    push_name TEXT, -- Sender's WhatsApp display name (from PushName in messages)
    contact_name TEXT, -- Saved contact name (from WhatsApp contact store)
    last_message_time INTEGER, -- Last message timestamp
    unread_count INTEGER DEFAULT 0,
    is_group BOOLEAN DEFAULT FALSE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,

    -- Ensure at least one JID is provided
    CHECK (jid_pn IS NOT NULL OR jid_lid IS NOT NULL)
);

-- Trigger to auto-update canonical JID in chats
CREATE TRIGGER IF NOT EXISTS chats_jid_update
AFTER INSERT ON chats
WHEN NEW.jid IS NULL
BEGIN
    UPDATE chats SET jid = COALESCE(NEW.jid_pn, NEW.jid_lid) WHERE rowid = NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS chats_jid_update_on_update
AFTER UPDATE OF jid_pn, jid_lid ON chats
BEGIN
    UPDATE chats SET jid = COALESCE(NEW.jid_pn, NEW.jid_lid) WHERE rowid = NEW.rowid;
END;

-- Indexes for chats
CREATE INDEX IF NOT EXISTS idx_chats_jid_pn ON chats(jid_pn) WHERE jid_pn IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_chats_jid_lid ON chats(jid_lid) WHERE jid_lid IS NOT NULL;

-- Group participants table (optional)
CREATE TABLE IF NOT EXISTS group_participants (
    group_jid TEXT NOT NULL,

    -- Participant JIDs in PN and LID formats
    participant_jid_pn TEXT, -- Participant JID in PN format
    participant_jid_lid TEXT, -- Participant JID in LID format

    -- Canonical JID (auto-updated by trigger)
    participant_jid TEXT,

    is_admin BOOLEAN DEFAULT FALSE,
    joined_at INTEGER,

    PRIMARY KEY (group_jid, participant_jid),
    FOREIGN KEY (group_jid) REFERENCES chats(jid) ON DELETE CASCADE,

    -- Ensure at least one JID is provided
    CHECK (participant_jid_pn IS NOT NULL OR participant_jid_lid IS NOT NULL)
);

-- Trigger to auto-update canonical participant JID
CREATE TRIGGER IF NOT EXISTS group_participants_jid_update
AFTER INSERT ON group_participants
WHEN NEW.participant_jid IS NULL
BEGIN
    UPDATE group_participants
    SET participant_jid = COALESCE(NEW.participant_jid_pn, NEW.participant_jid_lid)
    WHERE rowid = NEW.rowid;
END;

CREATE TRIGGER IF NOT EXISTS group_participants_jid_update_on_update
AFTER UPDATE OF participant_jid_pn, participant_jid_lid ON group_participants
BEGIN
    UPDATE group_participants
    SET participant_jid = COALESCE(NEW.participant_jid_pn, NEW.participant_jid_lid)
    WHERE rowid = NEW.rowid;
END;

-- Indexes for group participants
CREATE INDEX IF NOT EXISTS idx_group_participants_pn ON group_participants(participant_jid_pn) WHERE participant_jid_pn IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_group_participants_lid ON group_participants(participant_jid_lid) WHERE participant_jid_lid IS NOT NULL;

-- View for querying messages with sender names (current names from push_names and chats)
CREATE VIEW IF NOT EXISTS messages_with_names AS
SELECT
    m.id,
    m.chat_jid_pn,
    m.chat_jid_lid,
    m.chat_jid,
    m.sender_jid_pn,
    m.sender_jid_lid,
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
