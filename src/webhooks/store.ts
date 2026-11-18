// Webhooks and their deliveries, in the account's SQLite. An event becomes one
// delivery per webhook that wants it, written in the same turn as the archive change
// behind it, so a deploy or an eviction loses nothing. Times are Unix milliseconds.
import { z } from "zod"

import { migrate } from "../store/migrations"
import type { SqlDatabase } from "../store/sql"
import { isEventType, serialize, type SubscribableType, type WebhookEvent } from "./events"

const MIGRATIONS = [
  `CREATE TABLE webhooks (
    id TEXT PRIMARY KEY,
    account TEXT NOT NULL,
    url TEXT NOT NULL,
    secret TEXT NOT NULL,
    previous_secret TEXT,
    previous_secret_until INTEGER,
    events TEXT NOT NULL,
    chats TEXT,
    chat_types TEXT NOT NULL,
    headers TEXT NOT NULL,
    description TEXT,
    transcribe_voice INTEGER NOT NULL,
    enabled INTEGER NOT NULL,
    disabled_reason TEXT,
    failing_since INTEGER,
    created_via TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
  )`,
  `CREATE TABLE webhook_deliveries (
    webhook_id TEXT NOT NULL,
    event_id TEXT NOT NULL,
    type TEXT NOT NULL,
    payload TEXT NOT NULL,
    transcribe TEXT,
    created_at INTEGER NOT NULL,
    state TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at INTEGER,
    last_attempt_at INTEGER,
    last_status INTEGER,
    last_error TEXT,
    delivered_at INTEGER,
    PRIMARY KEY (webhook_id, event_id)
  )`,
  `CREATE INDEX webhook_deliveries_due ON webhook_deliveries (next_attempt_at) WHERE state = 'pending'`,
  `CREATE INDEX webhook_deliveries_by_webhook ON webhook_deliveries (webhook_id, created_at DESC)`,
]

/** A dead endpoint stops growing the queue past about this; the oldest are dropped. */
export const MAX_PENDING = 5_000
const KEEP_RECENT = 50
const KEEP_DELIVERED_MS = 24 * 60 * 60 * 1000
const KEEP_FAILED_MS = 7 * 24 * 60 * 60 * 1000

export type ChatKind = "direct" | "group"
const CREATED_VIA = ["api", "mcp", "page"] as const
export type CreatedVia = (typeof CREATED_VIA)[number]
export type DisabledReason = "gone" | "failing"
export type DeliveryState = "pending" | "delivered" | "failed" | "dropped"

export interface WebhookFields {
  readonly url: string
  readonly events: readonly (SubscribableType | "*")[]
  /** Canonical JIDs; null for every chat. */
  readonly chats: readonly string[] | null
  readonly chatTypes: readonly ChatKind[]
  readonly headers: Readonly<Record<string, string>>
  readonly description: string | null
  readonly transcribeVoice: boolean
}

export type WebhookPatch = { -readonly [K in keyof WebhookFields]?: WebhookFields[K] } & {
  enabled?: boolean
}

export interface Webhook extends Omit<WebhookFields, "headers"> {
  readonly id: string
  readonly account: string
  /** Header values never leave the store, only their names. */
  readonly headerNames: readonly string[]
  readonly enabled: boolean
  readonly disabledReason: DisabledReason | null
  readonly failingSince: number | null
  readonly createdVia: CreatedVia
  readonly createdAt: number
  readonly updatedAt: number
  readonly pending: number
  readonly lastDelivery: Delivery | null
}

export interface Delivery {
  readonly eventId: string
  readonly type: string
  readonly state: DeliveryState
  readonly attempts: number
  readonly createdAt: number
  readonly nextAttemptAt: number | null
  readonly lastAttemptAt: number | null
  readonly lastStatus: number | null
  readonly lastError: string | null
  readonly deliveredAt: number | null
}

/** What sending one delivery needs. */
export interface Target {
  readonly webhookId: string
  readonly url: string
  readonly headers: Readonly<Record<string, string>>
  readonly secrets: readonly string[]
}

export interface DueDelivery extends Target {
  readonly eventId: string
  readonly payload: string
  readonly attempts: number
  readonly transcribe: { readonly chatJid: string; readonly id: string } | null
}

type WebhookRow = {
  id: string
  account: string
  url: string
  secret: string
  previous_secret: string | null
  previous_secret_until: number | null
  events: string
  chats: string | null
  chat_types: string
  headers: string
  description: string | null
  transcribe_voice: number
  enabled: number
  disabled_reason: string | null
  failing_since: number | null
  created_via: string
  created_at: number
  updated_at: number
}

type DeliveryRow = {
  webhook_id: string
  event_id: string
  type: string
  payload: string
  transcribe: string | null
  created_at: number
  state: string
  attempts: number
  next_attempt_at: number | null
  last_attempt_at: number | null
  last_status: number | null
  last_error: string | null
  delivered_at: number | null
}

// The JSON columns are written only by this store; parsing still checks their shape.
const Strings = z.array(z.string())
const Headers = z.record(z.string(), z.string())
const VoiceRef = z.object({ chatJid: z.string(), id: z.string() })
const DELIVERY_STATES: readonly string[] = ["pending", "delivered", "failed", "dropped"]

function strings(json: string): string[] {
  return Strings.parse(JSON.parse(json))
}

function headersOf(json: string): Record<string, string> {
  return Headers.parse(JSON.parse(json))
}

function eventsOf(json: string): (SubscribableType | "*")[] {
  return strings(json).filter(
    (type): type is SubscribableType | "*" => type === "*" || isEventType(type),
  )
}

function chatKindsOf(json: string): ChatKind[] {
  return strings(json).filter((kind): kind is ChatKind => kind === "direct" || kind === "group")
}

function isDeliveryState(state: string): state is DeliveryState {
  return DELIVERY_STATES.includes(state)
}

function toDelivery(row: DeliveryRow): Delivery {
  return {
    eventId: row.event_id,
    type: row.type,
    state: isDeliveryState(row.state) ? row.state : "failed",
    attempts: row.attempts,
    createdAt: row.created_at,
    nextAttemptAt: row.next_attempt_at,
    lastAttemptAt: row.last_attempt_at,
    lastStatus: row.last_status,
    lastError: row.last_error,
    deliveredAt: row.delivered_at,
  }
}

type SecretColumns = Pick<WebhookRow, "secret" | "previous_secret" | "previous_secret_until">

function secretsOf(row: SecretColumns, now: number): string[] {
  const previous =
    row.previous_secret && (row.previous_secret_until ?? 0) > now ? [row.previous_secret] : []
  return [row.secret, ...previous]
}

// A chat saved by phone becomes its LID once the pair is learned, so both sides are
// compared in their canonical form.
function wants(row: WebhookRow, event: WebhookEvent, canonical: (jid: string) => string): boolean {
  if (event.type === "webhook.test") return false
  const events = strings(row.events)
  if (!events.includes("*") && !events.includes(event.type)) return false
  if (!event.chat) return true
  const types = strings(row.chat_types)
  if (!types.includes(event.chat.type)) return false
  const chats = row.chats === null ? null : strings(row.chats)
  return chats === null || chats.some((jid) => canonical(jid) === event.chat?.jid)
}

export class WebhookStore {
  constructor(private readonly storage: SqlDatabase) {
    migrate(storage, "webhooks", MIGRATIONS)
  }

  private get sql(): SqlDatabase["sql"] {
    return this.storage.sql
  }

  count(): number {
    return this.sql.exec<{ n: number }>("SELECT count(*) AS n FROM webhooks").one().n
  }

  /** False spares building events nobody would receive. */
  hasActive(): boolean {
    return !this.sql.exec("SELECT 1 FROM webhooks WHERE enabled = 1 LIMIT 1").next().done
  }

  create(
    id: string,
    fields: WebhookFields & { readonly account: string; readonly createdVia: CreatedVia },
    secret: string,
    now: number,
  ): Webhook {
    this.sql.exec(
      `INSERT INTO webhooks (id, account, url, secret, events, chats, chat_types, headers,
         description, transcribe_voice, enabled, created_via, created_at, updated_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
      id,
      fields.account,
      fields.url,
      secret,
      JSON.stringify(fields.events),
      fields.chats === null ? null : JSON.stringify(fields.chats),
      JSON.stringify(fields.chatTypes),
      JSON.stringify(fields.headers),
      fields.description,
      fields.transcribeVoice ? 1 : 0,
      fields.createdVia,
      now,
      now,
    )
    const created = this.get(id)
    if (!created) throw new Error(`webhook ${id} was not saved`)
    return created
  }

  /** Re-enabling clears the reason and the failure streak, and resends what is pending. */
  update(id: string, patch: WebhookPatch, now: number): Webhook | null {
    const row = this.row(id)
    if (!row) return null
    const sets: string[] = ["updated_at = ?"]
    const values: SqlStorageValue[] = [now]
    const set = (column: string, value: SqlStorageValue) => {
      sets.push(`${column} = ?`)
      values.push(value)
    }
    if (patch.url !== undefined) set("url", patch.url)
    if (patch.events !== undefined) set("events", JSON.stringify(patch.events))
    if (patch.chats !== undefined)
      set("chats", patch.chats === null ? null : JSON.stringify(patch.chats))
    if (patch.chatTypes !== undefined) set("chat_types", JSON.stringify(patch.chatTypes))
    if (patch.headers !== undefined) set("headers", JSON.stringify(patch.headers))
    if (patch.description !== undefined) set("description", patch.description)
    if (patch.transcribeVoice !== undefined) set("transcribe_voice", patch.transcribeVoice ? 1 : 0)
    if (patch.enabled !== undefined) {
      set("enabled", patch.enabled ? 1 : 0)
      if (patch.enabled && row.enabled === 0) {
        set("disabled_reason", null)
        set("failing_since", null)
        // Deliveries due long ago go out on the next drain.
        this.sql.exec(
          "UPDATE webhook_deliveries SET next_attempt_at = ? WHERE webhook_id = ? AND state = 'pending'",
          now,
          id,
        )
      }
    }
    this.sql.exec(`UPDATE webhooks SET ${sets.join(", ")} WHERE id = ?`, ...values, id)
    return this.get(id)
  }

  /** The old secret keeps signing next to the new one until `previousUntil`. */
  rotateSecret(id: string, secret: string, previousUntil: number, now: number): boolean {
    return (
      this.sql.exec(
        `UPDATE webhooks SET previous_secret = secret, previous_secret_until = ?, secret = ?,
           updated_at = ? WHERE id = ?`,
        previousUntil,
        secret,
        now,
        id,
      ).rowsWritten > 0
    )
  }

  delete(id: string): boolean {
    return this.storage.transactionSync(() => {
      this.sql.exec("DELETE FROM webhook_deliveries WHERE webhook_id = ?", id)
      return this.sql.exec("DELETE FROM webhooks WHERE id = ?", id).rowsWritten > 0
    })
  }

  disable(id: string, reason: DisabledReason, now: number): void {
    this.sql.exec(
      "UPDATE webhooks SET enabled = 0, disabled_reason = ?, updated_at = ? WHERE id = ?",
      reason,
      now,
      id,
    )
  }

  get(id: string): Webhook | null {
    const row = this.row(id)
    return row ? this.toWebhook(row) : null
  }

  list(): Webhook[] {
    return this.sql
      .exec<WebhookRow>("SELECT * FROM webhooks ORDER BY created_at")
      .toArray()
      .map((row) => this.toWebhook(row))
  }

  target(id: string, now: number): Target | null {
    const row = this.row(id)
    if (!row) return null
    return {
      webhookId: row.id,
      url: row.url,
      headers: headersOf(row.headers),
      secrets: secretsOf(row, now),
    }
  }

  recent(id: string, limit: number = KEEP_RECENT): Delivery[] {
    return this.sql
      .exec<DeliveryRow>(
        "SELECT * FROM webhook_deliveries WHERE webhook_id = ? ORDER BY created_at DESC LIMIT ?",
        id,
        limit,
      )
      .toArray()
      .map(toDelivery)
  }

  /** One delivery per enabled webhook that wants the event. Returns how many. */
  enqueue(
    event: WebhookEvent,
    now: number,
    canonical: (jid: string) => string = (jid) => jid,
  ): number {
    const rows = this.sql
      .exec<WebhookRow>("SELECT * FROM webhooks WHERE enabled = 1")
      .toArray()
      .filter((row) => wants(row, event, canonical))
    for (const row of rows) {
      const transcribe =
        row.transcribe_voice === 1 && event.voice ? JSON.stringify(event.voice) : null
      this.sql.exec(
        `INSERT INTO webhook_deliveries (webhook_id, event_id, type, payload, transcribe,
           created_at, state, next_attempt_at)
         VALUES (?, ?, ?, ?, ?, ?, 'pending', ?)
         ON CONFLICT (webhook_id, event_id) DO NOTHING`,
        row.id,
        event.id,
        event.type,
        serialize(event, row.account),
        transcribe,
        now,
        now,
      )
    }
    return rows.length
  }

  due(now: number, limit: number): DueDelivery[] {
    const rows = this.sql
      .exec<DeliveryRow & SecretColumns & Pick<WebhookRow, "url" | "headers">>(
        `SELECT d.*, w.url, w.headers, w.secret, w.previous_secret, w.previous_secret_until
         FROM webhook_deliveries d JOIN webhooks w ON w.id = d.webhook_id
         WHERE d.state = 'pending' AND d.next_attempt_at <= ? AND w.enabled = 1
         ORDER BY d.next_attempt_at LIMIT ?`,
        now,
        limit,
      )
      .toArray()
    return rows.map((row) => ({
      webhookId: row.webhook_id,
      url: row.url,
      headers: headersOf(row.headers),
      secrets: secretsOf(row, now),
      eventId: row.event_id,
      payload: row.payload,
      attempts: row.attempts,
      transcribe: row.transcribe === null ? null : VoiceRef.parse(JSON.parse(row.transcribe)),
    }))
  }

  /** When the next delivery is due, or null when nothing is waiting. */
  nextDueAt(): number | null {
    return this.sql
      .exec<{ at: number | null }>(
        `SELECT min(d.next_attempt_at) AS at FROM webhook_deliveries d
         JOIN webhooks w ON w.id = d.webhook_id WHERE d.state = 'pending' AND w.enabled = 1`,
      )
      .one().at
  }

  /** The payload after the transcript was added; it no longer waits for one. */
  setPayload(webhookId: string, eventId: string, payload: string): void {
    this.sql.exec(
      "UPDATE webhook_deliveries SET payload = ?, transcribe = NULL WHERE webhook_id = ? AND event_id = ?",
      payload,
      webhookId,
      eventId,
    )
  }

  delivered(webhookId: string, eventId: string, status: number, now: number): void {
    this.sql.exec(
      `UPDATE webhook_deliveries SET state = 'delivered', attempts = attempts + 1,
         next_attempt_at = NULL, last_attempt_at = ?, last_status = ?, last_error = NULL,
         delivered_at = ?
       WHERE webhook_id = ? AND event_id = ?`,
      now,
      status,
      now,
      webhookId,
      eventId,
    )
    this.sql.exec("UPDATE webhooks SET failing_since = NULL WHERE id = ?", webhookId)
  }

  /**
   * A failed attempt, retried at `retryAt` or given up when it is null. Returns
   * when the webhook started failing.
   */
  failed(
    webhookId: string,
    eventId: string,
    outcome: { readonly status: number | null; readonly error: string },
    retryAt: number | null,
    now: number,
  ): number {
    this.sql.exec(
      `UPDATE webhook_deliveries SET state = ?, attempts = attempts + 1, next_attempt_at = ?,
         last_attempt_at = ?, last_status = ?, last_error = ?
       WHERE webhook_id = ? AND event_id = ?`,
      retryAt === null ? "failed" : "pending",
      retryAt,
      now,
      outcome.status,
      outcome.error,
      webhookId,
      eventId,
    )
    this.sql.exec(
      "UPDATE webhooks SET failing_since = COALESCE(failing_since, ?) WHERE id = ?",
      now,
      webhookId,
    )
    return (
      this.sql
        .exec<{ failing_since: number | null }>(
          "SELECT failing_since FROM webhooks WHERE id = ?",
          webhookId,
        )
        .toArray()[0]?.failing_since ?? now
    )
  }

  /**
   * Drops the oldest pending deliveries past MAX_PENDING, then forgets finished ones,
   * keeping the latest of each webhook to show. Runs hourly rather than on every
   * event, which would scan the whole queue of a dead endpoint each time.
   */
  prune(now: number): void {
    this.sql.exec(
      `UPDATE webhook_deliveries SET state = 'dropped', next_attempt_at = NULL,
         last_error = 'dropped: too many pending deliveries'
       WHERE state = 'pending' AND rowid IN (
         SELECT rowid FROM (
           SELECT rowid, row_number() OVER (PARTITION BY webhook_id ORDER BY created_at DESC) AS n
           FROM webhook_deliveries WHERE state = 'pending')
         WHERE n > ?)`,
      MAX_PENDING,
    )
    this.sql.exec(
      `DELETE FROM webhook_deliveries
       WHERE state != 'pending'
         AND created_at < CASE state WHEN 'delivered' THEN ? ELSE ? END
         AND rowid NOT IN (
           SELECT rowid FROM (
             SELECT rowid, row_number() OVER (PARTITION BY webhook_id ORDER BY created_at DESC) AS n
             FROM webhook_deliveries)
           WHERE n <= ?)`,
      now - KEEP_DELIVERED_MS,
      now - KEEP_FAILED_MS,
      KEEP_RECENT,
    )
  }

  clear(): void {
    this.storage.transactionSync(() => {
      this.sql.exec("DELETE FROM webhook_deliveries")
      this.sql.exec("DELETE FROM webhooks")
    })
  }

  private row(id: string): WebhookRow | null {
    return this.sql.exec<WebhookRow>("SELECT * FROM webhooks WHERE id = ?", id).toArray()[0] ?? null
  }

  private toWebhook(row: WebhookRow): Webhook {
    const pending = this.sql
      .exec<{ n: number }>(
        "SELECT count(*) AS n FROM webhook_deliveries WHERE webhook_id = ? AND state = 'pending'",
        row.id,
      )
      .one().n
    const last = this.sql
      .exec<DeliveryRow>(
        `SELECT * FROM webhook_deliveries WHERE webhook_id = ? AND last_attempt_at IS NOT NULL
         ORDER BY last_attempt_at DESC LIMIT 1`,
        row.id,
      )
      .toArray()[0]
    return {
      id: row.id,
      account: row.account,
      url: row.url,
      events: eventsOf(row.events),
      chats: row.chats === null ? null : strings(row.chats),
      chatTypes: chatKindsOf(row.chat_types),
      headerNames: Object.keys(headersOf(row.headers)),
      description: row.description,
      transcribeVoice: row.transcribe_voice === 1,
      enabled: row.enabled === 1,
      disabledReason:
        row.disabled_reason === "gone" || row.disabled_reason === "failing"
          ? row.disabled_reason
          : null,
      failingSince: row.failing_since,
      createdVia: CREATED_VIA.find((via) => via === row.created_via) ?? "api",
      createdAt: row.created_at,
      updatedAt: row.updated_at,
      pending,
      lastDelivery: last ? toDelivery(last) : null,
    }
  }
}
