/**
 * The account's message archive: chats, messages, reactions and every name
 * seen for a person, group or channel, in the Durable Object's SQLite, with
 * full-text search over message text and over names.
 *
 * WhatsApp only pushes history once (at pairing) and then live traffic, so this
 * archive is what the tools read; the socket is only asked for things that are
 * not here (group metadata, profile, on-demand older history).
 *
 * Identity follows WhatsApp's own model (and mautrix-whatsapp's handling of
 * it): one person has a phone-number JID (`@s.whatsapp.net`) and an anonymous
 * LID (`@lid`), and messages can arrive under either. Everything about a person
 * is stored under one canonical JID, the LID when known, else the phone JID.
 * Learning that a phone JID and a LID belong together moves what was stored
 * under the phone JID (chats, messages, reactions, names) to the LID.
 *
 * Names are kept per source instead of overwriting each other (see `names.ts`):
 * display picks the best one, search matches all of them.
 *
 * Each message keeps its raw protobuf next to the derived columns: its key is
 * what edits, reactions and receipts must reference, and its media keys are
 * what downloads need.
 *
 * Times are Unix seconds.
 */
import type { MediaInfo, MessageKind } from "../whatsapp/message"
import { migrate } from "./migrations"
import {
  type Identity,
  isNameSource,
  type NameRecord,
  type NameSource,
  resolveIdentity,
} from "./names"
import type { SqlDatabase } from "./sql"

const MIGRATIONS = [
  `CREATE TABLE phone_lids (
    pn TEXT PRIMARY KEY,
    lid TEXT NOT NULL
  ) WITHOUT ROWID`,
  `CREATE INDEX phone_lids_by_lid ON phone_lids (lid)`,
  `CREATE TABLE names (
    jid TEXT NOT NULL,
    source TEXT NOT NULL,
    name TEXT NOT NULL,
    seen_at INTEGER NOT NULL,
    UNIQUE (jid, source, name)
  )`,
  `CREATE VIRTUAL TABLE names_fts USING fts5 (
    name, content = 'names', content_rowid = 'rowid',
    tokenize = 'unicode61 remove_diacritics 2'
  )`,
  `CREATE TRIGGER names_fts_insert AFTER INSERT ON names BEGIN
    INSERT INTO names_fts (rowid, name) VALUES (new.rowid, new.name);
  END`,
  `CREATE TRIGGER names_fts_delete AFTER DELETE ON names BEGIN
    INSERT INTO names_fts (names_fts, rowid, name) VALUES ('delete', old.rowid, old.name);
  END`,
  `CREATE TABLE chats (
    jid TEXT PRIMARY KEY,
    unread_count INTEGER NOT NULL DEFAULT 0,
    last_message_at INTEGER,
    archived INTEGER NOT NULL DEFAULT 0,
    pinned INTEGER NOT NULL DEFAULT 0,
    muted_until INTEGER
  )`,
  `CREATE INDEX chats_by_activity ON chats (last_message_at DESC)`,
  `CREATE TABLE messages (
    chat_jid TEXT NOT NULL,
    id TEXT NOT NULL,
    from_me INTEGER NOT NULL,
    sender_jid TEXT,
    sent_at INTEGER NOT NULL,
    kind TEXT NOT NULL,
    text TEXT,
    quoted_id TEXT,
    mentions TEXT,
    forwarded INTEGER NOT NULL DEFAULT 0,
    media TEXT,
    edited INTEGER NOT NULL DEFAULT 0,
    deleted INTEGER NOT NULL DEFAULT 0,
    raw BLOB,
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
  `CREATE INDEX reactions_by_sender ON reactions (sender_jid)`,
  `CREATE TABLE sent (
    request_key TEXT PRIMARY KEY,
    message_id TEXT NOT NULL,
    sent_at INTEGER NOT NULL
  ) WITHOUT ROWID`,
]

/** The reaction sender recorded for this account's own reactions. */
const ME = "me"

const PN_SUFFIX = "@s.whatsapp.net"
const LID_SUFFIX = "@lid"
/** SQLite's limit on bound parameters is 100; leave room for the rest of a query. */
const IN_CHUNK = 90

export interface NameInput {
  readonly source: NameSource
  readonly name: string
  /** Unix seconds; defaults to now. */
  readonly seenAt?: number | undefined
}

/** What an event says about a chat; absent fields are left as they are. */
export interface ChatInput {
  readonly jid: string
  readonly unreadCount?: number | null
  readonly lastMessageAt?: number | null
  readonly archived?: boolean | null
  readonly pinned?: boolean | null
  readonly mutedUntil?: number | null
}

/** A message ready to store: chat and sender already canonical. */
export interface MessageInput {
  readonly chatJid: string
  readonly id: string
  readonly fromMe: boolean
  readonly senderJid: string | null
  readonly sentAt: number
  readonly kind: MessageKind
  readonly text: string | null
  readonly quotedId: string | null
  readonly mentions: readonly string[]
  readonly forwarded: boolean
  readonly media: MediaInfo | null
  readonly raw: Uint8Array | null
}

export type ChatType = "direct" | "group" | "channel" | "broadcast"

export function chatTypeOf(jid: string): ChatType {
  if (jid.endsWith("@g.us")) return "group"
  if (jid.endsWith("@newsletter")) return "channel"
  if (jid.endsWith("@broadcast")) return "broadcast"
  return "direct"
}

export interface ChatSummary {
  readonly jid: string
  readonly name: string | null
  readonly phone: string | null
  readonly type: ChatType
  readonly unreadCount: number
  readonly lastMessageAt: number | null
  readonly archived: boolean
  readonly pinned: boolean
}

/** A search result and the name that made it match, when that is not its display name. */
export type Match<T> = T & { readonly matched: string | null }

export interface Mention {
  /** As written in the text, after the `@`. */
  readonly user: string
  readonly jid: string
  readonly name: string | null
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
  readonly mentions: readonly Mention[]
  readonly forwarded: boolean
  readonly media: MediaInfo | null
  readonly edited: boolean
  readonly deleted: boolean
  readonly reactions: readonly { readonly emoji: string; readonly senderJid: string }[]
}

export interface SearchHit extends ArchivedMessage {
  /** The matching text with hits wrapped in `[` `]`. */
  readonly snippet: string
}

export interface ArchiveStats {
  readonly people: number
  readonly chats: number
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

type ChatRow = {
  jid: string
  unread_count: number
  last_message_at: number | null
  archived: number
  pinned: number
}

type MessageRow = {
  chat_jid: string
  id: string
  from_me: number
  sender_jid: string | null
  sent_at: number
  kind: string
  text: string | null
  quoted_id: string | null
  mentions: string | null
  forwarded: number
  media: string | null
  edited: number
  deleted: number
}

const MESSAGE_COLUMNS = `m.chat_jid, m.id, m.from_me, m.sender_jid, m.sent_at, m.kind, m.text, m.quoted_id,
  m.mentions, m.forwarded, m.media, m.edited, m.deleted`

const CHAT_COLUMNS = "c.jid, c.unread_count, c.last_message_at, c.archived, c.pinned"

const MESSAGE_KINDS: ReadonlySet<string> = new Set<MessageKind>([
  "text",
  "image",
  "video",
  "video_note",
  "audio",
  "voice",
  "document",
  "sticker",
  "album",
  "location",
  "live_location",
  "contact",
  "poll",
  "event",
  "group_invite",
  "pin",
  "unavailable",
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

function parseMentions(json: string | null): string[] {
  if (!json) return []
  // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- written by `saveMessages` from a string[], never by anyone else
  return JSON.parse(json) as string[]
}

function userOf(jid: string): string {
  return jid.slice(0, jid.indexOf("@")).split(":")[0] ?? jid
}

/** `+<digits>` for a phone JID. */
export function phoneOf(pn: string | null): string | null {
  return pn?.endsWith(PN_SUFFIX) ? `+${userOf(pn)}` : null
}

function chunks<T>(items: readonly T[], size: number): T[][] {
  const out: T[][] = []
  for (let index = 0; index < items.length; index += size)
    out.push(items.slice(index, index + size))
  return out
}

function placeholders(count: number): string {
  return Array.from({ length: count }, () => "?").join(", ")
}

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

/** History marks contacts it cannot name with a masked phone ("+55∙∙∙∙∙∙∙∙08"); that is not a name. */
export function isRedactedPhone(name: string): boolean {
  return name.includes("∙")
}

export class Archive {
  constructor(private readonly storage: SqlDatabase) {
    migrate(storage, "archive", MIGRATIONS)
  }

  private get sql(): SqlDatabase["sql"] {
    return this.storage.sql
  }

  // Identity

  /**
   * The JID a person or chat is stored under: a phone JID becomes the person's
   * LID once that pairing is known; anything else is returned unchanged.
   */
  canonical(jid: string): string {
    if (!jid.endsWith(PN_SUFFIX)) return jid
    const row = this.sql
      .exec<{ lid: string }>("SELECT lid FROM phone_lids WHERE pn = ?", jid)
      .next()
    return row.done ? jid : row.value.lid
  }

  /** `null` while only the LID is known, and for anything that is not a person. */
  phoneJidOf(jid: string): string | null {
    if (jid.endsWith(PN_SUFFIX)) return jid
    if (!jid.endsWith(LID_SUFFIX)) return null
    const row = this.sql
      .exec<{ pn: string }>("SELECT pn FROM phone_lids WHERE lid = ? LIMIT 1", jid)
      .next()
    return row.done ? null : row.value.pn
  }

  /** Records that a phone JID and a LID are one person, moving what was stored under the phone JID. */
  learnPair(pn: string, lid: string): void {
    const known = this.sql
      .exec<{ lid: string }>("SELECT lid FROM phone_lids WHERE pn = ?", pn)
      .next()
    if (!known.done && known.value.lid === lid) return
    this.storage.transactionSync(() => {
      this.sql.exec("INSERT OR REPLACE INTO phone_lids (pn, lid) VALUES (?, ?)", pn, lid)
      this.rekey(pn, lid)
    })
  }

  /** Records names seen for a person, group or channel. A name seen again refreshes its date. */
  learnNames(jid: string, names: readonly NameInput[]): void {
    const id = this.canonical(jid)
    const now = Math.floor(Date.now() / 1000)
    for (const { source, name, seenAt } of names) {
      const trimmed = name.trim()
      if (!trimmed) continue
      this.sql.exec(
        `INSERT INTO names (jid, source, name, seen_at) VALUES (?, ?, ?, ?)
         ON CONFLICT (jid, source, name) DO UPDATE SET seen_at = MAX(seen_at, excluded.seen_at)`,
        id,
        source,
        trimmed,
        seenAt ?? now,
      )
    }
  }

  forgetNames(jid: string, source: NameSource): void {
    this.sql.exec("DELETE FROM names WHERE jid = ? AND source = ?", this.canonical(jid), source)
  }

  /** Moves everything stored under one JID to another; what already exists there wins. */
  private rekey(from: string, to: string): void {
    const exec = (query: string) => this.sql.exec(query, to, from)
    exec("UPDATE OR IGNORE messages SET chat_jid = ? WHERE chat_jid = ?")
    exec("UPDATE messages SET sender_jid = ? WHERE sender_jid = ?")
    exec("UPDATE OR IGNORE reactions SET chat_jid = ? WHERE chat_jid = ?")
    exec("UPDATE OR IGNORE reactions SET sender_jid = ? WHERE sender_jid = ?")
    exec("UPDATE OR IGNORE names SET jid = ? WHERE jid = ?")
    this.sql.exec(
      `INSERT INTO chats (jid, unread_count, last_message_at, archived, pinned, muted_until)
       SELECT ?, unread_count, last_message_at, archived, pinned, muted_until FROM chats WHERE jid = ?
       ON CONFLICT (jid) DO UPDATE SET
         unread_count = MAX(chats.unread_count, excluded.unread_count),
         last_message_at = MAX(COALESCE(chats.last_message_at, 0), COALESCE(excluded.last_message_at, 0)),
         archived = MIN(chats.archived, excluded.archived),
         pinned = MAX(chats.pinned, excluded.pinned),
         muted_until = MAX(COALESCE(chats.muted_until, 0), COALESCE(excluded.muted_until, 0))`,
      to,
      from,
    )
    // What could not move already exists under the new JID.
    this.sql.exec("DELETE FROM messages WHERE chat_jid = ?", from)
    this.sql.exec("DELETE FROM reactions WHERE chat_jid = ? OR sender_jid = ?", from, from)
    this.sql.exec("DELETE FROM names WHERE jid = ?", from)
    this.sql.exec("DELETE FROM chats WHERE jid = ?", from)
  }

  /** Display name, phone and other names of each JID, in one pass over the names table. */
  private identities(jids: Iterable<string>): Map<string, Identity> {
    const unique = [...new Set(jids)]
    const names = new Map<string, NameRecord[]>()
    const phones = new Map<string, string>()
    for (const chunk of chunks(unique, IN_CHUNK)) {
      const marks = placeholders(chunk.length)
      const nameRows = this.sql.exec<{
        jid: string
        source: string
        name: string
        seen_at: number
      }>(`SELECT jid, source, name, seen_at FROM names WHERE jid IN (${marks})`, ...chunk)
      for (const row of nameRows) {
        if (!isNameSource(row.source)) continue
        const list = names.get(row.jid) ?? []
        list.push({ source: row.source, name: row.name, seenAt: row.seen_at })
        names.set(row.jid, list)
      }
      const phoneRows = this.sql.exec<{ lid: string; pn: string }>(
        `SELECT lid, pn FROM phone_lids WHERE lid IN (${marks})`,
        ...chunk,
      )
      for (const row of phoneRows) phones.set(row.lid, row.pn)
    }
    return new Map(
      unique.map((jid) => [
        jid,
        resolveIdentity(jid, names.get(jid) ?? [], phoneOf(phones.get(jid) ?? jid)),
      ]),
    )
  }

  getPerson(jid: string): Identity {
    const id = this.canonical(jid)
    const identity = this.identities([id]).get(id)
    return identity ?? { jid: id, name: null, phone: phoneOf(id), aliases: [] }
  }

  /**
   * JIDs whose names (any of them, accents and case ignored) or phone match the
   * text, with the name that matched. `scope` narrows the candidates in SQL.
   */
  private matchNames(text: string, scope: string, limit: number): Map<string, string | null> {
    const fts = toFtsQuery(text)
    const digits = text.replace(/\D/g, "")
    const phone = digits.length >= 3 ? `%${digits}%` : null
    const rows = this.sql.exec<{ jid: string; matched: string | null }>(
      `WITH hits AS (
         SELECT n.jid, n.name AS matched FROM names_fts JOIN names n ON n.rowid = names_fts.rowid
         WHERE ?1 IS NOT NULL AND names_fts MATCH ?1
         UNION ALL SELECT lid, NULL FROM phone_lids WHERE ?2 IS NOT NULL AND pn LIKE ?2
         UNION ALL SELECT jid, NULL FROM chats WHERE ?2 IS NOT NULL AND jid LIKE ?2
       )
       SELECT h.jid, max(h.matched) AS matched FROM hits h ${scope}
       GROUP BY h.jid LIMIT ?3`,
      fts,
      phone,
      limit,
    )
    return new Map(Array.from(rows, (row) => [row.jid, row.matched]))
  }

  /** People whose names or phone match the text, including people with no chat. */
  findPeople(text: string, limit: number): Match<Identity>[] {
    const hits = this.matchNames(
      text,
      `WHERE h.jid LIKE '%${LID_SUFFIX}' OR h.jid LIKE '%${PN_SUFFIX}'`,
      limit,
    )
    const identities = this.identities(hits.keys())
    return [...hits].flatMap(([jid, matched]) => {
      const identity = identities.get(jid)
      return identity ? [{ ...identity, matched: matched === identity.name ? null : matched }] : []
    })
  }

  // Writes

  saveMessages(messages: readonly MessageInput[]): void {
    if (messages.length === 0) return
    this.storage.transactionSync(() => {
      const lastByChat = new Map<string, number>()
      for (const message of messages) {
        this.sql.exec(
          `INSERT INTO messages (chat_jid, id, from_me, sender_jid, sent_at, kind, text, quoted_id,
             mentions, forwarded, media, raw)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
           ON CONFLICT (chat_jid, id) DO UPDATE SET
             sender_jid = excluded.sender_jid,
             kind = excluded.kind,
             text = CASE WHEN edited OR deleted THEN text ELSE excluded.text END,
             quoted_id = excluded.quoted_id,
             mentions = excluded.mentions,
             forwarded = excluded.forwarded,
             media = excluded.media,
             raw = COALESCE(excluded.raw, raw)`,
          message.chatJid,
          message.id,
          message.fromMe ? 1 : 0,
          message.senderJid,
          message.sentAt,
          message.kind,
          message.text,
          message.quotedId,
          message.mentions.length > 0 ? JSON.stringify(message.mentions) : null,
          message.forwarded ? 1 : 0,
          message.media ? JSON.stringify(message.media) : null,
          message.raw ? message.raw.slice().buffer : null,
        )
        const last = lastByChat.get(message.chatJid) ?? 0
        if (message.sentAt > last) lastByChat.set(message.chatJid, message.sentAt)
      }
      for (const [jid, sentAt] of lastByChat) {
        this.sql.exec(
          `INSERT INTO chats (jid, last_message_at) VALUES (?, ?)
           ON CONFLICT (jid) DO UPDATE SET
             last_message_at = MAX(COALESCE(last_message_at, 0), excluded.last_message_at)`,
          jid,
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
    atMs: number,
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
      `INSERT INTO reactions (chat_jid, message_id, sender_jid, emoji, reacted_at) VALUES (?, ?, ?, ?, ?)
       ON CONFLICT (chat_jid, message_id, sender_jid) DO UPDATE SET emoji = excluded.emoji, reacted_at = excluded.reacted_at
       WHERE excluded.reacted_at >= reactions.reacted_at`,
      chatJid,
      messageId,
      sender,
      emoji,
      Math.floor(atMs / 1000),
    )
  }

  saveChats(chats: readonly ChatInput[]): void {
    if (chats.length === 0) return
    this.storage.transactionSync(() => {
      for (const chat of chats) {
        this.sql.exec(
          `INSERT INTO chats (jid, unread_count, last_message_at, archived, pinned, muted_until)
           VALUES (?1, COALESCE(?2, 0), ?3, COALESCE(?4, 0), COALESCE(?5, 0), ?6)
           ON CONFLICT (jid) DO UPDATE SET
             unread_count = COALESCE(?2, unread_count),
             last_message_at = MAX(COALESCE(last_message_at, 0), COALESCE(?3, 0)),
             archived = COALESCE(?4, archived),
             pinned = COALESCE(?5, pinned),
             muted_until = COALESCE(?6, muted_until)`,
          chat.jid,
          chat.unreadCount ?? null,
          chat.lastMessageAt ?? null,
          chat.archived === null || chat.archived === undefined ? null : Number(chat.archived),
          chat.pinned === null || chat.pinned === undefined ? null : Number(chat.pinned),
          chat.mutedUntil ?? null,
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

  // Reads

  listChats(query: ChatListQuery): ChatSummary[] {
    const filters = ["1 = 1"]
    if (query.unreadOnly) filters.push("c.unread_count > 0")
    if (query.type === "groups") filters.push("c.jid LIKE '%@g.us'")
    if (query.type === "direct") {
      filters.push(`(c.jid LIKE '%${LID_SUFFIX}' OR c.jid LIKE '%${PN_SUFFIX}')`)
    }
    if (!query.includeArchived) filters.push("c.archived = 0")
    return this.chatSummaries(
      this.sql
        .exec<ChatRow>(
          `SELECT ${CHAT_COLUMNS} FROM chats c WHERE ${filters.join(" AND ")}
           ORDER BY c.pinned DESC, c.last_message_at DESC NULLS LAST LIMIT ? OFFSET ?`,
          query.limit,
          query.offset,
        )
        .toArray(),
    )
  }

  /** Chats whose names (any of them) or phone match the text, most recently active first. */
  findChats(text: string, limit: number): Match<ChatSummary>[] {
    const hits = this.matchNames(text, "JOIN chats c ON c.jid = h.jid", 500)
    if (hits.size === 0) return []
    const jids = [...hits.keys()]
    const rows = chunks(jids, IN_CHUNK)
      .flatMap((chunk) =>
        this.sql
          .exec<ChatRow>(
            `SELECT ${CHAT_COLUMNS} FROM chats c WHERE c.jid IN (${placeholders(chunk.length)})`,
            ...chunk,
          )
          .toArray(),
      )
      .toSorted((a, b) => (b.last_message_at ?? 0) - (a.last_message_at ?? 0))
      .slice(0, limit)
    return this.chatSummaries(rows).map((chat) => {
      const matched = hits.get(chat.jid) ?? null
      return { ...chat, matched: matched === chat.name ? null : matched }
    })
  }

  getChat(jid: string): ChatSummary | null {
    const rows = this.sql
      .exec<ChatRow>(`SELECT ${CHAT_COLUMNS} FROM chats c WHERE c.jid = ?`, jid)
      .toArray()
    return this.chatSummaries(rows)[0] ?? null
  }

  private chatSummaries(rows: readonly ChatRow[]): ChatSummary[] {
    const identities = this.identities(rows.map((row) => row.jid))
    return rows.map((row) => {
      const identity = identities.get(row.jid)
      return {
        jid: row.jid,
        name: identity?.name ?? null,
        phone: identity?.phone ?? null,
        type: chatTypeOf(row.jid),
        unreadCount: row.unread_count,
        lastMessageAt: row.last_message_at,
        archived: row.archived === 1,
        pinned: row.pinned === 1,
      }
    })
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
    const rows = this.sql
      .exec<MessageRow>(
        `SELECT ${MESSAGE_COLUMNS} FROM messages m WHERE ${filters.join(" AND ")}
         ORDER BY m.sent_at DESC LIMIT ?`,
        ...params,
        query.limit,
      )
      .toArray()
    return this.toMessages(rows.toReversed())
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
    const select = (where: string, order: "ASC" | "DESC") =>
      this.sql
        .exec<MessageRow>(
          `SELECT ${MESSAGE_COLUMNS} FROM messages m WHERE m.chat_jid = ? AND ${where}
           ORDER BY m.sent_at ${order} LIMIT ?`,
          chatJid,
          at,
          id,
          around,
        )
        .toArray()
    const before = select("m.sent_at <= ? AND m.id != ?", "DESC").toReversed()
    const after = select("m.sent_at >= ? AND m.id != ?", "ASC")
    const self = this.sql
      .exec<MessageRow>(
        `SELECT ${MESSAGE_COLUMNS} FROM messages m WHERE m.chat_jid = ? AND m.id = ?`,
        chatJid,
        id,
      )
      .toArray()
    return this.toMessages([...before, ...self, ...after])
  }

  getMessage(chatJid: string, id: string): ArchivedMessage | null {
    const rows = this.sql
      .exec<MessageRow>(
        `SELECT ${MESSAGE_COLUMNS} FROM messages m WHERE m.chat_jid = ? AND m.id = ?`,
        chatJid,
        id,
      )
      .toArray()
    return this.toMessages(rows)[0] ?? null
  }

  /** The protobuf a message was stored from, for building its key or downloading its media. */
  getRaw(chatJid: string, id: string): Uint8Array | null {
    const row = this.sql
      .exec<{ raw: ArrayBuffer | null }>(
        "SELECT raw FROM messages WHERE chat_jid = ? AND id = ?",
        chatJid,
        id,
      )
      .next()
    return row.done || !row.value.raw ? null : new Uint8Array(row.value.raw)
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
    const rows = this.sql
      .exec<MessageRow & { snippet: string }>(
        `SELECT ${MESSAGE_COLUMNS}, snippet(messages_fts, 0, '[', ']', '…', 16) AS snippet
         FROM messages_fts JOIN messages m ON m.rowid = messages_fts.rowid
         WHERE ${filters.join(" AND ")} ORDER BY rank LIMIT ?`,
        ...params,
        query.limit,
      )
      .toArray()
    return this.toMessages(rows).map((message, index) => ({
      ...message,
      snippet: rows[index]?.snippet ?? "",
    }))
  }

  stats(): ArchiveStats {
    const count = (query: string): number => this.sql.exec<{ n: number }>(query).one().n
    const range = this.sql
      .exec<{ oldest: number | null; newest: number | null }>(
        "SELECT min(sent_at) AS oldest, max(sent_at) AS newest FROM messages",
      )
      .one()
    return {
      people: count(
        `SELECT count(*) AS n FROM (
           SELECT jid FROM names WHERE jid LIKE '%${LID_SUFFIX}' OR jid LIKE '%${PN_SUFFIX}'
           UNION SELECT lid FROM phone_lids)`,
      ),
      chats: count("SELECT count(*) AS n FROM chats"),
      messages: count("SELECT count(*) AS n FROM messages"),
      oldestMessageAt: range.oldest,
      newestMessageAt: range.newest,
    }
  }

  /** The oldest archived message of a chat, the anchor for fetching older history. */
  oldestMessage(chatJid: string): { id: string; sentAt: number } | null {
    const first = this.sql
      .exec<{ id: string; sent_at: number }>(
        "SELECT id, sent_at FROM messages WHERE chat_jid = ? ORDER BY sent_at ASC LIMIT 1",
        chatJid,
      )
      .next()
    return first.done ? null : { id: first.value.id, sentAt: first.value.sent_at }
  }

  /** Incoming messages of a chat, newest first, for marking them read. */
  latestIncoming(chatJid: string, limit: number): { id: string }[] {
    return this.sql
      .exec<{ id: string }>(
        "SELECT id FROM messages WHERE chat_jid = ? AND from_me = 0 ORDER BY sent_at DESC LIMIT ?",
        chatJid,
        limit,
      )
      .toArray()
  }

  /** Wipes everything: used when the device is unlinked. */
  clear(): void {
    this.storage.transactionSync(() => {
      for (const table of ["messages", "reactions", "chats", "names", "phone_lids", "sent"]) {
        this.sql.exec(`DELETE FROM ${table}`)
      }
    })
  }

  /** Rows to messages, with chat, sender and mention names and reactions resolved in bulk. */
  private toMessages(rows: readonly MessageRow[]): ArchivedMessage[] {
    const mentions = rows.map((row) => parseMentions(row.mentions))
    const identities = this.identities([
      ...rows.flatMap((row) => [row.chat_jid, ...(row.sender_jid ? [row.sender_jid] : [])]),
      ...mentions.flat().map((jid) => this.canonical(jid)),
    ])
    const nameOf = (jid: string | null) => (jid ? (identities.get(jid)?.name ?? null) : null)
    return rows.map((row, index) => ({
      chatJid: row.chat_jid,
      chatName: nameOf(row.chat_jid),
      id: row.id,
      fromMe: row.from_me === 1,
      senderJid: row.sender_jid,
      senderName: nameOf(row.sender_jid),
      sentAt: row.sent_at,
      kind: isMessageKind(row.kind) ? row.kind : "other",
      text: row.text,
      quotedId: row.quoted_id,
      mentions: (mentions[index] ?? []).map((jid) => {
        const canonical = this.canonical(jid)
        return { user: userOf(jid), jid: canonical, name: nameOf(canonical) }
      }),
      forwarded: row.forwarded === 1,
      media: parseMedia(row.media),
      edited: row.edited === 1,
      deleted: row.deleted === 1,
      reactions: this.reactionsOf(row.chat_jid, row.id),
    }))
  }

  private reactionsOf(chatJid: string, messageId: string): ArchivedMessage["reactions"] {
    const rows = this.sql.exec<{ emoji: string; sender_jid: string }>(
      "SELECT emoji, sender_jid FROM reactions WHERE chat_jid = ? AND message_id = ? ORDER BY reacted_at",
      chatJid,
      messageId,
    )
    return Array.from(rows, (row) => ({ emoji: row.emoji, senderJid: row.sender_jid }))
  }
}
