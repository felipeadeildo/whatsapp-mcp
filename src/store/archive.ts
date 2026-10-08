/**
 * The account's message archive: chats, contacts, messages and reactions in the
 * Durable Object's SQLite, with full-text search over message text.
 *
 * WhatsApp only pushes history once (at pairing) and then live traffic, so this
 * archive is what the tools read; the socket is only asked for things that are
 * not here (group metadata, profile, on-demand older history).
 *
 * Times are Unix seconds. JIDs are stored normalized (no device suffix).
 */
import type { MediaInfo, MessageKind, StoredMessage } from "../whatsapp/message"
import { migrate } from "./migrations"

const MIGRATIONS = [
  `CREATE TABLE chats (
    jid TEXT PRIMARY KEY,
    name TEXT,
    is_group INTEGER NOT NULL,
    unread_count INTEGER NOT NULL DEFAULT 0,
    last_message_at INTEGER,
    archived INTEGER NOT NULL DEFAULT 0,
    pinned INTEGER NOT NULL DEFAULT 0,
    muted_until INTEGER
  )`,
  `CREATE INDEX chats_by_activity ON chats (last_message_at DESC)`,
  `CREATE TABLE contacts (
    jid TEXT PRIMARY KEY,
    lid TEXT,
    phone TEXT,
    name TEXT,
    push_name TEXT,
    verified_name TEXT
  )`,
  `CREATE INDEX contacts_by_lid ON contacts (lid)`,
  `CREATE TABLE messages (
    chat_jid TEXT NOT NULL,
    id TEXT NOT NULL,
    from_me INTEGER NOT NULL,
    sender_jid TEXT,
    sender_name TEXT,
    sent_at INTEGER NOT NULL,
    kind TEXT NOT NULL,
    text TEXT,
    quoted_id TEXT,
    media TEXT,
    edited INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0,
    UNIQUE (chat_jid, id)
  )`,
  `CREATE INDEX messages_by_chat ON messages (chat_jid, sent_at DESC)`,
  `CREATE INDEX messages_by_sender ON messages (sender_jid, sent_at DESC)`,
  `CREATE VIRTUAL TABLE messages_fts USING fts5 (
    text, content = 'messages', content_rowid = 'rowid',
    tokenize = 'unicode61 remove_diacritics 2'
  )`,
  `CREATE TRIGGER messages_fts_insert AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts (rowid, text) VALUES (new.rowid, new.text);
  END`,
  `CREATE TRIGGER messages_fts_delete AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts (messages_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
  END`,
  `CREATE TRIGGER messages_fts_update AFTER UPDATE OF text ON messages BEGIN
    INSERT INTO messages_fts (messages_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
    INSERT INTO messages_fts (rowid, text) VALUES (new.rowid, new.text);
  END`,
  `CREATE TABLE reactions (
    chat_jid TEXT NOT NULL,
    message_id TEXT NOT NULL,
    sender_jid TEXT NOT NULL,
    emoji TEXT NOT NULL,
    reacted_at INTEGER NOT NULL,
    PRIMARY KEY (chat_jid, message_id, sender_jid)
  ) WITHOUT ROWID`,
  `CREATE TABLE sent (
    request_key TEXT PRIMARY KEY,
    message_id TEXT NOT NULL,
    sent_at INTEGER NOT NULL
  ) WITHOUT ROWID`,
]

/** The reaction sender recorded for our own reactions, whose key carries no JID. */
const ME = "me"

export interface ChatInput {
  readonly jid: string
  readonly name: string | null
  readonly unreadCount: number | null
  readonly lastMessageAt: number | null
  readonly archived: boolean | null
  readonly pinned: boolean | null
  readonly mutedUntil: number | null
}

export interface ContactInput {
  readonly jid: string
  readonly lid: string | null
  readonly phone: string | null
  readonly name: string | null
  readonly pushName: string | null
  readonly verifiedName: string | null
}

export interface ChatSummary {
  readonly jid: string
  readonly name: string | null
  readonly isGroup: boolean
  readonly unreadCount: number
  readonly lastMessageAt: number | null
  readonly archived: boolean
  readonly pinned: boolean
}

export interface ArchivedMessage {
  readonly chatJid: string
  readonly chatName: string | null
  readonly id: string
  readonly fromMe: boolean
  readonly senderJid: string | null
  readonly senderName: string | null
  readonly sentAt: number
  readonly kind: MessageKind
  readonly text: string | null
  readonly quotedId: string | null
  readonly media: MediaInfo | null
  readonly edited: boolean
  readonly deleted: boolean
  readonly reactions: readonly { readonly emoji: string; readonly senderJid: string }[]
}

export interface SearchHit extends ArchivedMessage {
  /** The matching text with hits wrapped in `[` `]`. */
  readonly snippet: string
}

export interface ContactSummary {
  readonly jid: string
  readonly lid: string | null
  readonly phone: string | null
  readonly name: string | null
  readonly pushName: string | null
  readonly verifiedName: string | null
}

export interface ArchiveStats {
  readonly chats: number
  readonly contacts: number
  readonly messages: number
  readonly oldestMessageAt: number | null
  readonly newestMessageAt: number | null
}

export interface MessageQuery {
  readonly chatJid: string
  readonly limit: number
  readonly before?: number | undefined
  readonly after?: number | undefined
  readonly senderJid?: string | undefined
}

export interface SearchQuery {
  readonly query: string
  readonly limit: number
  readonly chatJid?: string | undefined
  readonly senderJid?: string | undefined
  readonly before?: number | undefined
  readonly after?: number | undefined
}

export interface ChatListQuery {
  readonly limit: number
  readonly offset: number
  readonly unreadOnly: boolean
  readonly type: "all" | "groups" | "direct"
  readonly includeArchived: boolean
}

type MessageRow = {
  chat_jid: string
  chat_name: string | null
  id: string
  from_me: number
  sender_jid: string | null
  sender_name: string | null
  sent_at: number
  kind: string
  text: string | null
  quoted_id: string | null
  media: string | null
  edited: number
  deleted: number
}

const MESSAGE_KINDS: ReadonlySet<string> = new Set<MessageKind>([
  "text",
  "image",
  "video",
  "audio",
  "voice",
  "document",
  "sticker",
  "location",
  "contact",
  "poll",
  "other",
])

function isMessageKind(kind: string): kind is MessageKind {
  return MESSAGE_KINDS.has(kind)
}

function parseMedia(json: string | null): MediaInfo | null {
  if (!json) return null
  // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- written by `saveMessages` from a MediaInfo, never by anyone else
  return JSON.parse(json) as MediaInfo
}

/**
 * Display names, best first: the saved contact name, the group subject or chat
 * name, the WhatsApp profile name, the verified business name. Contacts are
 * matched by phone JID or by LID, since a chat can be addressed by either.
 */
const NAME_JOINS = (alias: string, jid: string) => `
  LEFT JOIN contacts ${alias}_pn ON ${alias}_pn.jid = ${jid}
  LEFT JOIN contacts ${alias}_lid ON ${alias}_lid.lid = ${jid}`
const CONTACT_NAME = (alias: string) => `COALESCE(
  ${alias}_pn.name, ${alias}_lid.name,
  ${alias}_pn.push_name, ${alias}_lid.push_name,
  ${alias}_pn.verified_name, ${alias}_lid.verified_name)`

/** Message columns with resolved chat and sender names; `from` must alias messages as `m`. */
const messageSelect = (from = "messages m", extraColumns = "") => `
  SELECT m.chat_jid, COALESCE(chat_pn.name, chat_lid.name, c.name, ${CONTACT_NAME("chat")}) AS chat_name,
    m.id, m.from_me, m.sender_jid, COALESCE(${CONTACT_NAME("sender")}, m.sender_name) AS sender_name,
    m.sent_at, m.kind, m.text, m.quoted_id, m.media, m.edited, m.deleted ${extraColumns}
  FROM ${from}
  LEFT JOIN chats c ON c.jid = m.chat_jid
  ${NAME_JOINS("chat", "m.chat_jid")}
  ${NAME_JOINS("sender", "m.sender_jid")}`
const MESSAGE_SELECT = messageSelect()

/**
 * Turns free text into an FTS5 query that cannot be a syntax error: every word
 * becomes a quoted prefix term, and all terms must match.
 */
export function toFtsQuery(query: string): string | null {
  const terms = query
    .split(/\s+/)
    .map((term) => term.replaceAll('"', ""))
    .filter((term) => term.length > 0)
  if (terms.length === 0) return null
  return terms.map((term) => `"${term}"*`).join(" ")
}

export class Archive {
  constructor(private readonly storage: DurableObjectStorage) {
    migrate(storage, "archive", MIGRATIONS)
  }

  private get sql(): SqlStorage {
    return this.storage.sql
  }

  saveMessages(messages: readonly StoredMessage[]): void {
    if (messages.length === 0) return
    this.storage.transactionSync(() => {
      const lastByChat = new Map<string, number>()
      for (const message of messages) {
        this.sql.exec(
          `INSERT INTO messages (chat_jid, id, from_me, sender_jid, sender_name, sent_at, kind, text, quoted_id, media)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
           ON CONFLICT (chat_jid, id) DO UPDATE SET
             sender_name = COALESCE(excluded.sender_name, sender_name),
             kind = excluded.kind,
             text = CASE WHEN edited OR deleted THEN text ELSE excluded.text END,
             quoted_id = excluded.quoted_id,
             media = excluded.media`,
          message.chatJid,
          message.id,
          message.fromMe ? 1 : 0,
          message.senderJid,
          message.senderName,
          message.sentAt,
          message.kind,
          message.text,
          message.quotedId,
          message.media ? JSON.stringify(message.media) : null,
        )
        if (message.senderJid && message.senderName) {
          this.sql.exec(
            `INSERT INTO contacts (jid, push_name) VALUES (?, ?)
             ON CONFLICT (jid) DO UPDATE SET push_name = excluded.push_name`,
            message.senderJid,
            message.senderName,
          )
        }
        const last = lastByChat.get(message.chatJid) ?? 0
        if (message.sentAt > last) lastByChat.set(message.chatJid, message.sentAt)
      }
      for (const [jid, sentAt] of lastByChat) {
        this.sql.exec(
          `INSERT INTO chats (jid, is_group, last_message_at) VALUES (?, ?, ?)
           ON CONFLICT (jid) DO UPDATE SET
             last_message_at = MAX(COALESCE(last_message_at, 0), excluded.last_message_at)`,
          jid,
          jid.endsWith("@g.us") ? 1 : 0,
          sentAt,
        )
      }
    })
  }

  applyEdit(chatJid: string, id: string, text: string): void {
    this.sql.exec(
      "UPDATE messages SET text = ?, edited = 1 WHERE chat_jid = ? AND id = ?",
      text,
      chatJid,
      id,
    )
  }

  applyRevoke(chatJid: string, id: string): void {
    this.sql.exec(
      "UPDATE messages SET text = NULL, media = NULL, deleted = 1 WHERE chat_jid = ? AND id = ?",
      chatJid,
      id,
    )
  }

  saveReaction(
    chatJid: string,
    messageId: string,
    senderJid: string | null,
    emoji: string,
    at: number,
  ): void {
    const sender = senderJid ?? ME
    if (emoji === "") {
      this.sql.exec(
        "DELETE FROM reactions WHERE chat_jid = ? AND message_id = ? AND sender_jid = ?",
        chatJid,
        messageId,
        sender,
      )
      return
    }
    this.sql.exec(
      `INSERT OR REPLACE INTO reactions (chat_jid, message_id, sender_jid, emoji, reacted_at)
       VALUES (?, ?, ?, ?, ?)`,
      chatJid,
      messageId,
      sender,
      emoji,
      Math.floor(at / 1000),
    )
  }

  saveChats(chats: readonly ChatInput[]): void {
    if (chats.length === 0) return
    this.storage.transactionSync(() => {
      for (const chat of chats) {
        this.sql.exec(
          `INSERT INTO chats (jid, name, is_group, unread_count, last_message_at, archived, pinned, muted_until)
           VALUES (?1, ?2, ?3, COALESCE(?4, 0), ?5, COALESCE(?6, 0), COALESCE(?7, 0), ?8)
           ON CONFLICT (jid) DO UPDATE SET
             name = COALESCE(?2, name),
             unread_count = COALESCE(?4, unread_count),
             last_message_at = MAX(COALESCE(last_message_at, 0), COALESCE(?5, 0)),
             archived = COALESCE(?6, archived),
             pinned = COALESCE(?7, pinned),
             muted_until = COALESCE(?8, muted_until)`,
          chat.jid,
          chat.name,
          chat.jid.endsWith("@g.us") ? 1 : 0,
          chat.unreadCount,
          chat.lastMessageAt,
          chat.archived === null ? null : Number(chat.archived),
          chat.pinned === null ? null : Number(chat.pinned),
          chat.mutedUntil,
        )
      }
    })
  }

  deleteChats(jids: readonly string[]): void {
    this.storage.transactionSync(() => {
      for (const jid of jids) {
        this.sql.exec("DELETE FROM messages WHERE chat_jid = ?", jid)
        this.sql.exec("DELETE FROM reactions WHERE chat_jid = ?", jid)
        this.sql.exec("DELETE FROM chats WHERE jid = ?", jid)
      }
    })
  }

  saveContacts(contacts: readonly ContactInput[]): void {
    if (contacts.length === 0) return
    this.storage.transactionSync(() => {
      for (const contact of contacts) {
        this.sql.exec(
          `INSERT INTO contacts (jid, lid, phone, name, push_name, verified_name) VALUES (?, ?, ?, ?, ?, ?)
           ON CONFLICT (jid) DO UPDATE SET
             lid = COALESCE(excluded.lid, lid),
             phone = COALESCE(excluded.phone, phone),
             name = COALESCE(excluded.name, name),
             push_name = COALESCE(excluded.push_name, push_name),
             verified_name = COALESCE(excluded.verified_name, verified_name)`,
          contact.jid,
          contact.lid,
          contact.phone,
          contact.name,
          contact.pushName,
          contact.verifiedName,
        )
      }
    })
  }

  /** Records a send so a retried request with the same key returns the same message. */
  rememberSend(requestKey: string, messageId: string): void {
    this.sql.exec(
      "INSERT OR REPLACE INTO sent (request_key, message_id, sent_at) VALUES (?, ?, ?)",
      requestKey,
      messageId,
      Math.floor(Date.now() / 1000),
    )
  }

  /** The message sent under `requestKey` within the last `withinSeconds`, if any. */
  findSend(requestKey: string, withinSeconds: number): string | null {
    const first = this.sql
      .exec<{ message_id: string }>(
        "SELECT message_id FROM sent WHERE request_key = ? AND sent_at >= ?",
        requestKey,
        Math.floor(Date.now() / 1000) - withinSeconds,
      )
      .next()
    return first.done ? null : first.value.message_id
  }

  listChats(query: ChatListQuery): ChatSummary[] {
    const filters = ["1 = 1"]
    if (query.unreadOnly) filters.push("c.unread_count > 0")
    if (query.type === "groups") filters.push("c.is_group = 1")
    if (query.type === "direct") filters.push("c.is_group = 0")
    if (!query.includeArchived) filters.push("c.archived = 0")
    return this.chatRows(
      `${filters.join(" AND ")} ORDER BY c.pinned DESC, c.last_message_at DESC NULLS LAST LIMIT ? OFFSET ?`,
      query.limit,
      query.offset,
    )
  }

  /** Chats whose name, contact name, JID or number contains `text` (case-insensitive for ASCII). */
  findChats(text: string, limit: number): ChatSummary[] {
    const pattern = `%${text.replace(/[%_\\]/g, "\\$&")}%`
    return this.chatRows(
      `(c.name LIKE ?1 ESCAPE '\\' OR ${CONTACT_NAME("chat")} LIKE ?1 ESCAPE '\\' OR c.jid LIKE ?1 ESCAPE '\\'
        OR chat_pn.phone LIKE ?1 ESCAPE '\\')
       ORDER BY c.last_message_at DESC NULLS LAST LIMIT ?2`,
      pattern,
      limit,
    )
  }

  getChat(jid: string): ChatSummary | null {
    return this.chatRows("c.jid = ?", jid)[0] ?? null
  }

  private chatRows(where: string, ...params: SqlStorageValue[]): ChatSummary[] {
    const rows = this.sql.exec<{
      jid: string
      name: string | null
      is_group: number
      unread_count: number
      last_message_at: number | null
      archived: number
      pinned: number
    }>(
      `SELECT c.jid, COALESCE(c.name, ${CONTACT_NAME("chat")}) AS name, c.is_group, c.unread_count,
         c.last_message_at, c.archived, c.pinned
       FROM chats c ${NAME_JOINS("chat", "c.jid")}
       WHERE ${where}`,
      ...params,
    )
    return Array.from(rows, (row) => ({
      jid: row.jid,
      name: row.name,
      isGroup: row.is_group === 1,
      unreadCount: row.unread_count,
      lastMessageAt: row.last_message_at,
      archived: row.archived === 1,
      pinned: row.pinned === 1,
    }))
  }

  /** Newest `limit` messages of a chat matching the filters, returned oldest first. */
  getMessages(query: MessageQuery): ArchivedMessage[] {
    const filters = ["m.chat_jid = ?"]
    const params: SqlStorageValue[] = [query.chatJid]
    if (query.before !== undefined) {
      filters.push("m.sent_at < ?")
      params.push(query.before)
    }
    if (query.after !== undefined) {
      filters.push("m.sent_at > ?")
      params.push(query.after)
    }
    if (query.senderJid !== undefined) {
      filters.push("m.sender_jid = ?")
      params.push(query.senderJid)
    }
    const rows = this.messageRows(
      `${MESSAGE_SELECT} WHERE ${filters.join(" AND ")} ORDER BY m.sent_at DESC LIMIT ?`,
      ...params,
      query.limit,
    )
    return this.withReactions(rows).toReversed()
  }

  /** The message plus up to `around` messages on each side, oldest first. */
  getMessageContext(chatJid: string, id: string, around: number): ArchivedMessage[] {
    const anchor = this.sql
      .exec<{ sent_at: number }>(
        "SELECT sent_at FROM messages WHERE chat_jid = ? AND id = ?",
        chatJid,
        id,
      )
      .next()
    if (anchor.done) return []
    const at = anchor.value.sent_at
    const before = this.messageRows(
      `${MESSAGE_SELECT} WHERE m.chat_jid = ? AND m.sent_at <= ? AND m.id != ? ORDER BY m.sent_at DESC LIMIT ?`,
      chatJid,
      at,
      id,
      around,
    ).toReversed()
    const self = this.messageRows(
      `${MESSAGE_SELECT} WHERE m.chat_jid = ? AND m.id = ?`,
      chatJid,
      id,
    )
    const after = this.messageRows(
      `${MESSAGE_SELECT} WHERE m.chat_jid = ? AND m.sent_at >= ? AND m.id != ? ORDER BY m.sent_at ASC LIMIT ?`,
      chatJid,
      at,
      id,
      around,
    )
    return this.withReactions([...before, ...self, ...after])
  }

  getMessage(chatJid: string, id: string): ArchivedMessage | null {
    const rows = this.messageRows(
      `${MESSAGE_SELECT} WHERE m.chat_jid = ? AND m.id = ?`,
      chatJid,
      id,
    )
    return this.withReactions(rows)[0] ?? null
  }

  searchMessages(query: SearchQuery): SearchHit[] {
    const match = toFtsQuery(query.query)
    if (!match) return []
    const filters = ["messages_fts MATCH ?"]
    const params: SqlStorageValue[] = [match]
    if (query.chatJid !== undefined) {
      filters.push("m.chat_jid = ?")
      params.push(query.chatJid)
    }
    if (query.senderJid !== undefined) {
      filters.push("m.sender_jid = ?")
      params.push(query.senderJid)
    }
    if (query.before !== undefined) {
      filters.push("m.sent_at < ?")
      params.push(query.before)
    }
    if (query.after !== undefined) {
      filters.push("m.sent_at > ?")
      params.push(query.after)
    }
    const rows = this.sql.exec<MessageRow & { snippet: string }>(
      `${messageSelect(
        "messages_fts JOIN messages m ON m.rowid = messages_fts.rowid",
        ", snippet(messages_fts, 0, '[', ']', '…', 16) AS snippet",
      )}
       WHERE ${filters.join(" AND ")} ORDER BY rank LIMIT ?`,
      ...params,
      query.limit,
    )
    const hits = Array.from(rows)
    const messages = this.withReactions(hits.map((row) => this.toMessage(row)))
    return messages.map((message, index) => ({ ...message, snippet: hits[index]?.snippet ?? "" }))
  }

  findContacts(text: string, limit: number): ContactSummary[] {
    const pattern = `%${text.replace(/[%_\\]/g, "\\$&")}%`
    const rows = this.sql.exec<{
      jid: string
      lid: string | null
      phone: string | null
      name: string | null
      push_name: string | null
      verified_name: string | null
    }>(
      `SELECT jid, lid, phone, name, push_name, verified_name FROM contacts
       WHERE name LIKE ?1 ESCAPE '\\' OR push_name LIKE ?1 ESCAPE '\\' OR verified_name LIKE ?1 ESCAPE '\\'
         OR jid LIKE ?1 ESCAPE '\\' OR phone LIKE ?1 ESCAPE '\\'
       ORDER BY name IS NULL, name, push_name LIMIT ?2`,
      pattern,
      limit,
    )
    return Array.from(rows, (row) => ({
      jid: row.jid,
      lid: row.lid,
      phone: row.phone,
      name: row.name,
      pushName: row.push_name,
      verifiedName: row.verified_name,
    }))
  }

  stats(): ArchiveStats {
    const count = (table: string): number =>
      this.sql.exec<{ n: number }>(`SELECT count(*) AS n FROM ${table}`).one().n
    const range = this.sql
      .exec<{ oldest: number | null; newest: number | null }>(
        "SELECT min(sent_at) AS oldest, max(sent_at) AS newest FROM messages",
      )
      .one()
    return {
      chats: count("chats"),
      contacts: count("contacts"),
      messages: count("messages"),
      oldestMessageAt: range.oldest,
      newestMessageAt: range.newest,
    }
  }

  /** The oldest archived message of a chat, the anchor for fetching older history. */
  oldestMessage(chatJid: string): { id: string; fromMe: boolean; sentAt: number } | null {
    const first = this.sql
      .exec<{ id: string; from_me: number; sent_at: number }>(
        "SELECT id, from_me, sent_at FROM messages WHERE chat_jid = ? ORDER BY sent_at ASC LIMIT 1",
        chatJid,
      )
      .next()
    if (first.done) return null
    return { id: first.value.id, fromMe: first.value.from_me === 1, sentAt: first.value.sent_at }
  }

  /** Wipes everything: used when the device is unlinked. */
  clear(): void {
    this.storage.transactionSync(() => {
      for (const table of ["messages", "reactions", "chats", "contacts", "sent"]) {
        this.sql.exec(`DELETE FROM ${table}`)
      }
    })
  }

  private messageRows(query: string, ...params: SqlStorageValue[]): ArchivedMessage[] {
    return Array.from(this.sql.exec<MessageRow>(query, ...params), (row) => this.toMessage(row))
  }

  private toMessage(row: MessageRow): ArchivedMessage {
    return {
      chatJid: row.chat_jid,
      chatName: row.chat_name,
      id: row.id,
      fromMe: row.from_me === 1,
      senderJid: row.sender_jid,
      senderName: row.sender_name,
      sentAt: row.sent_at,
      kind: isMessageKind(row.kind) ? row.kind : "other",
      text: row.text,
      quotedId: row.quoted_id,
      media: parseMedia(row.media),
      edited: row.edited === 1,
      deleted: row.deleted === 1,
      reactions: [],
    }
  }

  private withReactions(messages: ArchivedMessage[]): ArchivedMessage[] {
    if (messages.length === 0) return messages
    const byMessage = new Map<string, { emoji: string; senderJid: string }[]>()
    for (const message of messages) {
      const rows = this.sql.exec<{ emoji: string; sender_jid: string }>(
        "SELECT emoji, sender_jid FROM reactions WHERE chat_jid = ? AND message_id = ?",
        message.chatJid,
        message.id,
      )
      const reactions = Array.from(rows, (row) => ({ emoji: row.emoji, senderJid: row.sender_jid }))
      if (reactions.length > 0) byMessage.set(`${message.chatJid}/${message.id}`, reactions)
    }
    return messages.map((message) => {
      const reactions = byMessage.get(`${message.chatJid}/${message.id}`)
      return reactions ? { ...message, reactions } : message
    })
  }
}
