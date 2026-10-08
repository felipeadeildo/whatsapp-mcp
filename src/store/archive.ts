/**
 * The account's message archive: people, chats, messages and reactions in the
 * Durable Object's SQLite, with full-text search over message text.
 *
 * WhatsApp only pushes history once (at pairing) and then live traffic, so this
 * archive is what the tools read; the socket is only asked for things that are
 * not here (group metadata, profile, on-demand older history).
 *
 * Identity follows WhatsApp's own model (and mautrix-whatsapp's handling of
 * it): one person has a phone-number JID (`@s.whatsapp.net`) and an anonymous
 * LID (`@lid`), and messages can arrive under either. Every person and every
 * direct chat is stored under one canonical JID, the LID when known, else the
 * phone JID. Learning that a phone JID and a LID belong together merges the
 * two, including chats and messages already stored under the phone JID.
 *
 * Each message keeps its raw protobuf next to the derived columns, so a better
 * normalizer can re-derive them later without a new pairing, and media keys
 * stay available for downloads.
 *
 * Times are Unix seconds.
 */
import type { MediaInfo, MessageKind } from "../whatsapp/message"
import { migrate } from "./migrations"
import type { SqlDatabase } from "./sql"

const MIGRATIONS = [
  `CREATE TABLE people (
    id TEXT PRIMARY KEY,
    lid TEXT,
    pn TEXT,
    name TEXT,
    push_name TEXT,
    business_name TEXT,
    redacted_phone TEXT,
    username TEXT
  )`,
  `CREATE UNIQUE INDEX people_by_lid ON people (lid) WHERE lid IS NOT NULL`,
  `CREATE UNIQUE INDEX people_by_pn ON people (pn) WHERE pn IS NOT NULL`,
  `CREATE TABLE chats (
    jid TEXT PRIMARY KEY,
    name TEXT,
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
    sender_name TEXT,
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
  `CREATE TABLE sent (
    request_key TEXT PRIMARY KEY,
    message_id TEXT NOT NULL,
    sent_at INTEGER NOT NULL
  ) WITHOUT ROWID`,
  `CREATE INDEX reactions_by_sender ON reactions (sender_jid)`,
]

/** The reaction sender recorded for this account's own reactions. */
const ME = "me"

const PN_SUFFIX = "@s.whatsapp.net"
const LID_SUFFIX = "@lid"

/** What one event tells us about a person; absent fields are left as they are. */
export interface PersonInput {
  readonly jid: string
  readonly lid?: string | null
  readonly pn?: string | null
  readonly name?: string | null
  readonly pushName?: string | null
  readonly businessName?: string | null
  readonly redactedPhone?: string | null
  readonly username?: string | null
}

/** What an event says about a chat; absent fields are left as they are. */
export interface ChatInput {
  readonly jid: string
  readonly name?: string | null
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
  readonly senderName: string | null
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

export interface Person {
  readonly jid: string
  readonly name: string | null
  readonly phone: string | null
  readonly lid: string | null
  /** The name they chose on WhatsApp, when different from `name`. */
  readonly whatsappName: string | null
  readonly businessName: string | null
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

type PersonRow = {
  id: string
  lid: string | null
  pn: string | null
  name: string | null
  push_name: string | null
  business_name: string | null
  redacted_phone: string | null
  username: string | null
}

const PERSON_FIELDS = [
  "id",
  "lid",
  "pn",
  "name",
  "push_name",
  "business_name",
  "redacted_phone",
  "username",
] as const satisfies readonly (keyof PersonRow)[]

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
  mentions: string | null
  forwarded: number
  media: string | null
  edited: number
  deleted: number
}

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

/** SQL for the phone of a phone-JID expression. */
const sqlPhone = (pn: string) =>
  `CASE WHEN ${pn} LIKE '%${PN_SUFFIX}' THEN '+' || substr(${pn}, 1, instr(${pn}, '@') - 1) END`

/**
 * SQL for a person's display name, best first: the address book name, the name
 * they chose on WhatsApp, their verified business name, their phone number,
 * their masked phone. `jid` covers people never stored (a bare phone JID).
 */
const sqlPersonName = (p: string, jid: string) =>
  `COALESCE(${p}.name, ${p}.push_name, ${p}.business_name, ${sqlPhone(`${p}.pn`)}, ${sqlPhone(jid)}, ${p}.redacted_phone)`

const sqlChatName = (c: string, p: string) => `COALESCE(${c}.name, ${sqlPersonName(p, `${c}.jid`)})`

/** Message columns with resolved chat and sender names; `from` must alias messages as `m`. */
const messageSelect = (from = "messages m", extraColumns = "") => `
  SELECT m.chat_jid, ${sqlChatName("c", "cp")} AS chat_name,
    m.id, m.from_me, m.sender_jid, COALESCE(${sqlPersonName("sp", "m.sender_jid")}, m.sender_name) AS sender_name,
    m.sent_at, m.kind, m.text, m.quoted_id, m.mentions, m.forwarded, m.media, m.edited, m.deleted ${extraColumns}
  FROM ${from}
  LEFT JOIN chats c ON c.jid = m.chat_jid
  LEFT JOIN people cp ON cp.id = m.chat_jid
  LEFT JOIN people sp ON sp.id = m.sender_jid`
const MESSAGE_SELECT = messageSelect()

const CHAT_SELECT = `
  SELECT c.jid, ${sqlChatName("c", "p")} AS name, COALESCE(${sqlPhone("p.pn")}, ${sqlPhone("c.jid")}) AS phone,
    c.unread_count, c.last_message_at, c.archived, c.pinned
  FROM chats c LEFT JOIN people p ON p.id = c.jid`

function likePattern(text: string): string {
  return `%${text.replace(/[%_\\]/g, "\\$&")}%`
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

  // People

  /**
   * The JID a person or chat is stored under: a phone JID becomes the person's
   * LID once that pairing is known; anything else is returned unchanged.
   */
  canonical(jid: string): string {
    if (!jid.endsWith(PN_SUFFIX)) return jid
    const row = this.sql.exec<{ id: string }>("SELECT id FROM people WHERE pn = ?", jid).next()
    return row.done ? jid : row.value.id
  }

  /** Records what an event says about a person, merging their phone and LID identities. */
  learnPerson(input: PersonInput): void {
    const inputLid = input.lid ?? (input.jid.endsWith(LID_SUFFIX) ? input.jid : null)
    const inputPn = input.pn ?? (input.jid.endsWith(PN_SUFFIX) ? input.jid : null)

    // On conflicting names the phone identity wins: the address book is keyed
    // by phone number, and mautrix-whatsapp settles push-name conflicts the same way.
    const byPn = inputPn ? this.findPerson("id = ?1 OR pn = ?1", inputPn) : null
    const byLid = inputLid ? this.findPerson("id = ?1 OR lid = ?1", inputLid) : null
    const byId = byPn || byLid ? null : this.findPerson("id = ?1", input.jid)
    const existing = [byPn, byLid, byId].filter(
      (row, index, rows): row is PersonRow =>
        row !== null && rows.findIndex((other) => other?.id === row.id) === index,
    )
    const pick = (field: Exclude<keyof PersonRow, "id">) =>
      existing.map((row) => row[field]).find((value) => value !== null) ?? null

    // The canonical id is decided after merging: an update that only names the
    // phone JID must not pull a person already known by LID back to the phone.
    const lid = inputLid ?? pick("lid")
    const pn = inputPn ?? pick("pn")
    const id = lid ?? pn ?? input.jid
    const merged: PersonRow = {
      id,
      lid,
      pn,
      name: input.name ?? pick("name"),
      push_name: input.pushName ?? pick("push_name"),
      business_name: input.businessName ?? pick("business_name"),
      redacted_phone: input.redactedPhone ?? pick("redacted_phone"),
      username: input.username ?? pick("username"),
    }
    const merging = existing.filter((row) => row.id !== id)
    const [only] = existing
    const unchanged =
      existing.length === 1 &&
      only?.id === id &&
      PERSON_FIELDS.every((field) => only[field] === merged[field])
    if (unchanged) return

    this.storage.transactionSync(() => {
      for (const row of merging) this.sql.exec("DELETE FROM people WHERE id = ?", row.id)
      this.sql.exec(
        `INSERT OR REPLACE INTO people (id, lid, pn, name, push_name, business_name, redacted_phone, username)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
        merged.id,
        merged.lid,
        merged.pn,
        merged.name,
        merged.push_name,
        merged.business_name,
        merged.redacted_phone,
        merged.username,
      )
      // Chats and messages may sit under any JID that now resolves to `id`,
      // including a phone JID no person row was ever stored under. A name
      // update for an identity already settled has nothing to move.
      const settled = existing.some(
        (row) => row.id === id && row.pn === merged.pn && row.lid === merged.lid,
      )
      const aliases = new Set(merging.map((row) => row.id))
      if (!settled) for (const alias of [merged.pn, merged.lid]) if (alias) aliases.add(alias)
      aliases.delete(id)
      for (const alias of aliases) this.rekey(alias, id)
    })
  }

  private findPerson(where: string, jid: string): PersonRow | null {
    const row = this.sql.exec<PersonRow>(`SELECT * FROM people WHERE ${where}`, jid).next()
    return row.done ? null : row.value
  }

  /** Moves a chat, its messages and reactions, and authorship from one JID to another. */
  private rekey(from: string, to: string): void {
    const exec = (query: string) => this.sql.exec(query, to, from)
    exec("UPDATE OR IGNORE messages SET chat_jid = ? WHERE chat_jid = ?")
    exec("UPDATE messages SET sender_jid = ? WHERE sender_jid = ?")
    exec("UPDATE OR IGNORE reactions SET chat_jid = ? WHERE chat_jid = ?")
    exec("UPDATE OR IGNORE reactions SET sender_jid = ? WHERE sender_jid = ?")
    this.sql.exec(
      `INSERT INTO chats (jid, name, unread_count, last_message_at, archived, pinned, muted_until)
       SELECT ?, name, unread_count, last_message_at, archived, pinned, muted_until FROM chats WHERE jid = ?
       ON CONFLICT (jid) DO UPDATE SET
         name = COALESCE(chats.name, excluded.name),
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
    this.sql.exec("DELETE FROM chats WHERE jid = ?", from)
  }

  findPeople(text: string, limit: number): Person[] {
    const rows = this.sql.exec<PersonRow>(
      `SELECT * FROM people
       WHERE name LIKE ?1 ESCAPE '\\' OR push_name LIKE ?1 ESCAPE '\\' OR business_name LIKE ?1 ESCAPE '\\'
         OR pn LIKE ?1 ESCAPE '\\' OR id LIKE ?1 ESCAPE '\\' OR username LIKE ?1 ESCAPE '\\'
       ORDER BY name IS NULL, name, push_name LIMIT ?2`,
      likePattern(text),
      limit,
    )
    return Array.from(rows, (row) => this.toPerson(row))
  }

  getPerson(jid: string): Person | null {
    const row = this.findPerson("id = ?1", this.canonical(jid))
    return row ? this.toPerson(row) : null
  }

  private toPerson(row: PersonRow): Person {
    const phone = phoneOf(row.pn) ?? phoneOf(row.id)
    const name = row.name ?? row.push_name ?? row.business_name ?? phone ?? row.redacted_phone
    return {
      jid: row.id,
      name,
      phone,
      lid: row.lid,
      whatsappName: row.push_name && row.push_name !== name ? row.push_name : null,
      businessName: row.business_name,
    }
  }

  /** Best display name for any JID: a chat's name, or a person's. */
  private nameOf(jid: string): string | null {
    const id = this.canonical(jid)
    const row = this.sql
      .exec<{ name: string | null }>(
        `SELECT COALESCE(c.name, ${sqlPersonName("p", "?1")}) AS name
         FROM (SELECT ?1 AS jid) j LEFT JOIN chats c ON c.jid = j.jid LEFT JOIN people p ON p.id = j.jid`,
        id,
      )
      .one()
    return row.name
  }

  // Writes

  saveMessages(messages: readonly MessageInput[]): void {
    if (messages.length === 0) return
    this.storage.transactionSync(() => {
      const lastByChat = new Map<string, number>()
      for (const message of messages) {
        this.sql.exec(
          `INSERT INTO messages (chat_jid, id, from_me, sender_jid, sender_name, sent_at, kind, text, quoted_id,
             mentions, forwarded, media, raw)
           VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
           ON CONFLICT (chat_jid, id) DO UPDATE SET
             sender_jid = excluded.sender_jid,
             sender_name = COALESCE(excluded.sender_name, sender_name),
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
          message.senderName,
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
          `INSERT INTO chats (jid, name, unread_count, last_message_at, archived, pinned, muted_until)
           VALUES (?1, ?2, COALESCE(?3, 0), ?4, COALESCE(?5, 0), COALESCE(?6, 0), ?7)
           ON CONFLICT (jid) DO UPDATE SET
             name = COALESCE(?2, name),
             unread_count = COALESCE(?3, unread_count),
             last_message_at = MAX(COALESCE(last_message_at, 0), COALESCE(?4, 0)),
             archived = COALESCE(?5, archived),
             pinned = COALESCE(?6, pinned),
             muted_until = COALESCE(?7, muted_until)`,
          chat.jid,
          chat.name ?? null,
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
    if (query.type === "direct")
      filters.push(`(c.jid LIKE '%${LID_SUFFIX}' OR c.jid LIKE '%${PN_SUFFIX}')`)
    if (!query.includeArchived) filters.push("c.archived = 0")
    return this.chatRows(
      `${CHAT_SELECT} WHERE ${filters.join(" AND ")}
       ORDER BY c.pinned DESC, c.last_message_at DESC NULLS LAST LIMIT ? OFFSET ?`,
      query.limit,
      query.offset,
    )
  }

  /** Chats whose name, JID or phone contains `text` (case-insensitive for ASCII). */
  findChats(text: string, limit: number): ChatSummary[] {
    return this.chatRows(
      `SELECT * FROM (${CHAT_SELECT})
       WHERE name LIKE ?1 ESCAPE '\\' OR jid LIKE ?1 ESCAPE '\\' OR phone LIKE ?1 ESCAPE '\\'
       ORDER BY last_message_at DESC NULLS LAST LIMIT ?2`,
      likePattern(text),
      limit,
    )
  }

  getChat(jid: string): ChatSummary | null {
    return this.chatRows(`${CHAT_SELECT} WHERE c.jid = ?`, jid)[0] ?? null
  }

  private chatRows(query: string, ...params: SqlStorageValue[]): ChatSummary[] {
    const rows = this.sql.exec<{
      jid: string
      name: string | null
      phone: string | null
      unread_count: number
      last_message_at: number | null
      archived: number
      pinned: number
    }>(query, ...params)
    return Array.from(rows, (row) => ({
      jid: row.jid,
      name: row.name,
      phone: row.phone,
      type: chatTypeOf(row.jid),
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

  stats(): ArchiveStats {
    const count = (table: string): number =>
      this.sql.exec<{ n: number }>(`SELECT count(*) AS n FROM ${table}`).one().n
    const range = this.sql
      .exec<{ oldest: number | null; newest: number | null }>(
        "SELECT min(sent_at) AS oldest, max(sent_at) AS newest FROM messages",
      )
      .one()
    return {
      people: count("people"),
      chats: count("chats"),
      messages: count("messages"),
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
    return Array.from(
      this.sql.exec<{ id: string }>(
        "SELECT id FROM messages WHERE chat_jid = ? AND from_me = 0 ORDER BY sent_at DESC LIMIT ?",
        chatJid,
        limit,
      ),
    )
  }

  /** Wipes everything: used when the device is unlinked. */
  clear(): void {
    this.storage.transactionSync(() => {
      for (const table of ["messages", "reactions", "chats", "people", "sent"]) {
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
      mentions: parseMentions(row.mentions).map((jid) => ({
        user: userOf(jid),
        jid: this.canonical(jid),
        name: this.nameOf(jid),
      })),
      forwarded: row.forwarded === 1,
      media: parseMedia(row.media),
      edited: row.edited === 1,
      deleted: row.deleted === 1,
      reactions: [],
    }
  }

  private withReactions(messages: ArchivedMessage[]): ArchivedMessage[] {
    return messages.map((message) => {
      const rows = this.sql.exec<{ emoji: string; sender_jid: string }>(
        "SELECT emoji, sender_jid FROM reactions WHERE chat_jid = ? AND message_id = ? ORDER BY reacted_at",
        message.chatJid,
        message.id,
      )
      const reactions = Array.from(rows, (row) => ({ emoji: row.emoji, senderJid: row.sender_jid }))
      return reactions.length > 0 ? { ...message, reactions } : message
    })
  }
}
