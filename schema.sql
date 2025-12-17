-- Tabela principal de mensagens
CREATE TABLE
  IF NOT EXISTS messages (
    id TEXT PRIMARY KEY, -- Message ID único do WhatsApp
    chat_jid TEXT NOT NULL, -- JID do chat (número@s.whatsapp.net ou grupo@g.us)
    sender_jid TEXT NOT NULL, -- JID de quem enviou
    text TEXT, -- Conteúdo texto (null se for mídia)
    timestamp INTEGER NOT NULL, -- Unix timestamp
    is_from_me BOOLEAN NOT NULL, -- true se eu enviei
    message_type TEXT NOT NULL, -- 'text', 'image', 'video', etc
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
  );

-- Índices para busca rápida
CREATE INDEX IF NOT EXISTS idx_chat_timestamp ON messages (chat_jid, timestamp DESC);

CREATE INDEX IF NOT EXISTS idx_text_search ON messages (text)
WHERE
  text IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_sender ON messages (sender_jid);

-- Tabela de chats (conversas)
CREATE TABLE
  IF NOT EXISTS chats (
    jid TEXT PRIMARY KEY, -- JID do chat
    name TEXT, -- Nome do contato/grupo
    last_message_time INTEGER, -- Timestamp última mensagem
    unread_count INTEGER DEFAULT 0,
    is_group BOOLEAN DEFAULT FALSE,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
  );

-- Tabela de participantes de grupo (opcional)
CREATE TABLE
  IF NOT EXISTS group_participants (
    group_jid TEXT NOT NULL,
    participant_jid TEXT NOT NULL,
    is_admin BOOLEAN DEFAULT FALSE,
    joined_at INTEGER,
    PRIMARY KEY (group_jid, participant_jid)
  );