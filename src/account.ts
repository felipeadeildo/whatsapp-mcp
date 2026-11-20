// One Durable Object per WhatsApp account. An open outbound WebSocket pins it for
// at most 15 minutes and an idle object is evicted after 70-140s, so a 30s alarm
// keeps it in memory and rebuilds the socket after evictions and deploys.
import {
  createAuthenticationState,
  DisconnectReason,
  type HostConnectionUpdate,
} from "@oxidezap/baileyrs/host"
import type { GroupMetadata } from "@oxidezap/baileyrs/lib/Types/GroupMetadata.js"
import { WAMessageAddressingMode } from "@oxidezap/baileyrs/lib/Types/Message.js"
import { isBoom } from "@oxidezap/baileyrs/lib/Utils/boom.js"
import { jidNormalizedUser } from "@oxidezap/baileyrs/lib/WABinary/jid-utils.js"
import { DurableObject } from "cloudflare:workers"
import { renderSVG } from "uqr"
import { z } from "zod"

import { handleApi } from "./api/rest"
import { errorText, RequestError, whatsappStatus } from "./errors"
import {
  Archive,
  type ArchivedMessage,
  type ArchiveStats,
  chatTypeOf,
  type ChatListQuery,
  type ChatSummary,
  type MessageQuery,
  type Match,
  type SearchHit,
  type SearchQuery,
} from "./store/archive"
import { durableKvStore } from "./store/durable-kv"
import type { Identity } from "./store/names"
import { type ReadMessage, speechFromEnv, voiceTranscripts } from "./transcribe"
import { type MediaView, mediaViewer, visionFromEnv } from "./view-media"
import { fetchWaWebVersion } from "./wa-version"
import { type ContactName, removeContact, resolveAddress, saveContact } from "./whatsapp/contacts"
import { whatsappMedia } from "./whatsapp/media"
import { decodeRaw, type KeyedMessage } from "./whatsapp/raw"
import { type MentionTarget, mentionJid, mentionText } from "./whatsapp/mentions"
import { makeSocket, type Socket } from "./whatsapp/socket"
import { type ArchiveChange, refreshGroupNames, syncArchive } from "./whatsapp/sync"
import { type Attempt, drain, post } from "./webhooks/deliver"
import {
  type ChatRef,
  chatData,
  type ConnectionChange,
  connectionEvent,
  messageData,
  messageEvent,
  newId,
  type Person,
  reactionEvent,
  type Self,
  serialize,
  testEvent,
  type WebhookEvent,
} from "./webhooks/events"
import {
  fieldsOf,
  MAX_WEBHOOKS,
  patchOf,
  type WebhookCreate,
  type WebhookUpdate,
} from "./webhooks/schema"
import { newSecret } from "./webhooks/sign"
import { type CreatedVia, type Delivery, type Webhook, WebhookStore } from "./webhooks/store"
import "./wasm"

const HEARTBEAT_MS = 30_000
const WANTS_CONNECTION = "wants_connection"
const IMPLICIT_DEDUP_SECONDS = 120
const EXPLICIT_DEDUP_SECONDS = 24 * 60 * 60
const HISTORY_WAIT_MS = 20_000
const GROUPS_REFRESHED_AT = "groups_refreshed_at"
// The first number to link owns the archive, and still does after it unlinks.
const OWNER = "owner"
const GROUP_REFRESH_MS = 6 * 60 * 60 * 1000
// The phone sends history in batches; a pause this long means it is done or stalled.
const HISTORY_IDLE_MS = 45_000
// Set while a number is linked, so reconnects are not reported as new links.
const LINKED = "linked"
const SECRET_GRACE_MS = 24 * 60 * 60 * 1000
const PRUNE_EVERY_MS = 60 * 60 * 1000
// WhatsApp lets the sender edit a message for 15 minutes; later edits reach nobody.
export const EDIT_WINDOW_SECONDS = 15 * 60
// How long a group's addressing mode is trusted before it is asked again.
const ADDRESSING_TTL_MS = 10 * 60 * 1000

// The library types USync results as `unknown`.
const AboutResult = z.object({ status: z.object({ status: z.string().nullish() }) })

export type ConnectionState = "idle" | "connecting" | "pairing" | "open" | "closed" | "logged_out"

export interface HistorySync {
  readonly active: boolean
  readonly progress: number | null
}

export interface AccountStatus {
  readonly state: ConnectionState
  readonly me: string | null
  readonly myName: string | null
  readonly qrSvg: string | null
  readonly lastError: string | null
  /** The number the archive belongs to, kept while unlinked. */
  readonly owner: string | null
  /** Another number tried to link and was refused. */
  readonly refused: string | null
  readonly startedAt: number
  readonly history: HistorySync
}

export interface Overview extends AccountStatus {
  readonly archive: ArchiveStats
}

export interface SendRequest {
  readonly to: string
  readonly text: string
  readonly replyTo?: string | undefined
  readonly mentions?: readonly string[] | undefined
  readonly idempotencyKey?: string | undefined
}

export interface SendResult {
  readonly chatJid: string
  readonly id: string
  readonly deduplicated: boolean
}

export type Presence = "typing" | "recording" | "paused"

export interface MessagesData {
  readonly chat: Record<string, unknown>
  readonly messages: readonly Record<string, unknown>[]
}

export interface MessageData {
  readonly chat: Record<string, unknown>
  readonly message: Record<string, unknown>
}

/** Who the linked number is, by every JID WhatsApp may use for it. */
export interface Me {
  readonly jid: string | null
  readonly lid: string | null
  readonly phone: string | null
  readonly name: string | null
  readonly state: ConnectionState
}

export interface NumberCheck {
  readonly phone: string
  readonly onWhatsApp: boolean
  readonly jid: string | null
}

export interface GroupInfo {
  readonly jid: string
  readonly subject: string
  readonly description: string | null
  readonly owner: string | null
  readonly createdAt: number | null
  readonly size: number
  readonly announceOnly: boolean
  readonly restricted: boolean
  /** Whether members are addressed by LID or by phone JID. */
  readonly addressingMode: "lid" | "pn"
  readonly participants: readonly {
    readonly jid: string
    readonly name: string | null
    readonly contactName: string | null
    readonly profileName: string | null
    readonly phone: string | null
    readonly admin: "admin" | "superadmin" | null
  }[]
}

export interface Profile {
  readonly jid: string
  readonly name: string | null
  readonly phone: string | null
  readonly aliases: readonly string[]
  readonly about: string | null
  readonly pictureUrl: string | null
}

function addressingOf(group: GroupMetadata): "lid" | "pn" {
  return group.addressingMode === WAMessageAddressingMode.LID ? "lid" : "pn"
}

// The phone JID the socket is logged in as, without the device suffix.
function numberOf(socket: Socket | null): string | null {
  const id = socket?.user?.id
  return id ? jidNormalizedUser(id) : null
}

async function digest(parts: readonly string[]): Promise<string> {
  const bytes = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(parts.join("\u0000")),
  )
  return Array.from(new Uint8Array(bytes), (byte) => byte.toString(16).padStart(2, "0")).join("")
}

export class WhatsAppAccount extends DurableObject<Env> {
  private readonly store = durableKvStore(this.ctx.storage)
  private readonly archive = new Archive(this.ctx.storage)
  private socket: Socket | null = null
  private opening: Promise<void> | null = null
  private state: ConnectionState = "idle"
  private qrSvg: string | null = null
  private lastError: string | null = null
  private refused: string | null = null
  private readonly startedAt = Date.now()
  private readonly historyWaiters = new Map<string, () => void>()
  private readonly files = whatsappMedia({
    raw: (chatJid, id) => this.archive.getRaw(chatJid, id),
    saveRaw: (chatJid, id, raw) => this.archive.saveRaw(chatJid, id, raw),
    socket: () => this.connectedSocket(),
  })
  private readonly transcripts = voiceTranscripts({
    speech: speechFromEnv(this.env),
    fetchAudio: (message) => this.files.fetch(message),
    save: (message, transcript) =>
      this.archive.saveTranscript(message.chatJid, message.id, transcript),
    waitUntil: (work) => this.ctx.waitUntil(work),
  })
  private readonly media = mediaViewer({
    vision: visionFromEnv(this.env),
    transcripts: this.transcripts,
    fetchMedia: (message) => this.files.fetch(message),
    thumbnail: (message) => this.files.thumbnail(message),
    saveDescription: (message, description) =>
      this.archive.saveDescription(message.chatJid, message.id, description),
    waitUntil: (work) => this.ctx.waitUntil(work),
  })
  private historyProgress: number | null = null
  private historyBatchAt = 0
  private readonly webhooks = new WebhookStore(this.ctx.storage)
  private draining: Promise<void> | null = null
  private drainAgain = false
  private prunedAt = 0
  private readonly addressing = new Map<string, { mode: "lid" | "pn"; at: number }>()

  /** The REST API for programs; the Worker has already authenticated the caller. */
  override async fetch(request: Request): Promise<Response> {
    return handleApi(request, this)
  }

  async start(): Promise<AccountStatus> {
    this.refused = null
    await this.ctx.storage.put(WANTS_CONNECTION, true)
    await this.keepAlive()
    return this.status()
  }

  async logout(): Promise<AccountStatus> {
    await this.ctx.storage.put(WANTS_CONNECTION, false)
    const me = numberOf(this.socket)
    // Unlinking needs a paired session; while still pairing only the socket goes.
    if (this.socket?.user) await this.socket.logout()
    else await this.socket?.end(undefined)
    this.forgetCredentials()
    this.connectionChanged("unlinked", me, null)
    await this.scheduleAlarm()
    return this.status()
  }

  // The only way to delete the archive. Any number can link after it, so the
  // webhooks go too: they would send the next owner's messages to the last one's.
  async erase(): Promise<AccountStatus> {
    await this.logout()
    this.archive.clear()
    this.webhooks.clear()
    this.ctx.storage.kv.delete(OWNER)
    // The next pairing starts from an empty archive, so it should not wait for the throttle.
    await this.ctx.storage.delete(GROUPS_REFRESHED_AT)
    this.lastError = null
    this.refused = null
    return this.status()
  }

  status(): AccountStatus {
    const user = this.socket?.user
    return {
      state: this.state,
      me: numberOf(this.socket),
      myName: user?.name || null,
      qrSvg: this.state === "pairing" ? this.qrSvg : null,
      lastError: this.lastError,
      owner: this.owner(),
      refused: this.refused,
      startedAt: this.startedAt,
      history: {
        active: Date.now() - this.historyBatchAt < HISTORY_IDLE_MS && this.historyProgress !== 100,
        progress: this.historyProgress,
      },
    }
  }

  // Also wakes a connection that should be up, instead of waiting for the next alarm.
  async overview(): Promise<Overview> {
    if (this.state === "idle" && (await this.ctx.storage.get<boolean>(WANTS_CONNECTION))) {
      void this.keepAlive()
    }
    return { ...this.status(), archive: this.archive.stats() }
  }

  async alarm(): Promise<void> {
    try {
      if (await this.ctx.storage.get<boolean>(WANTS_CONNECTION)) await this.ensureConnected()
    } catch (error) {
      this.lastError = errorText(error)
    } finally {
      await this.deliver()
    }
  }

  listChats(query: ChatListQuery): ChatSummary[] {
    return this.archive.listChats(query)
  }

  findChats(text: string, limit: number): Match<ChatSummary>[] {
    return this.archive.findChats(text, limit)
  }

  findPeople(text: string, limit: number): Match<Identity>[] {
    return this.archive.findPeople(text, limit)
  }

  async getMessages(query: MessageQuery, transcribeVoice: boolean): Promise<ReadMessage[]> {
    const senderJid = query.senderJid === undefined ? undefined : this.resolveChat(query.senderJid)
    const messages = this.archive.getMessages({
      ...query,
      chatJid: this.resolveChat(query.chatJid),
      senderJid,
    })
    return this.transcripts.read(messages, transcribeVoice)
  }

  async getMessageContext(
    chatJid: string,
    id: string,
    around: number,
    transcribeVoice: boolean,
  ): Promise<ReadMessage[]> {
    const messages = this.archive.getMessageContext(this.resolveChat(chatJid), id, around)
    return this.transcripts.read(messages, transcribeVoice)
  }

  async viewMedia(chatJid: string, id: string): Promise<MediaView> {
    const chat = this.resolveChat(chatJid)
    const message = this.archive.getMessage(chat, id)
    if (!message) throw new Error(`message ${id} not found in ${chat}`)
    return this.media.view(message)
  }

  searchMessages(query: SearchQuery): SearchHit[] {
    const chatJid = query.chatJid === undefined ? undefined : this.resolveChat(query.chatJid)
    const senderJid = query.senderJid === undefined ? undefined : this.resolveChat(query.senderJid)
    return this.archive.searchMessages({ ...query, chatJid, senderJid })
  }

  async send(request: SendRequest): Promise<SendResult> {
    const socket = this.connectedSocket()
    const chatJid = this.resolveChat(request.to)
    const targets = await this.mentionTargets(chatJid, request.mentions ?? [])
    const mentions = targets.map((target) => target.jid)
    const text = mentionText(request.text, targets)
    const requestKey =
      request.idempotencyKey ?? (await digest([chatJid, text, request.replyTo ?? "", ...mentions]))
    const window = request.idempotencyKey ? EXPLICIT_DEDUP_SECONDS : IMPLICIT_DEDUP_SECONDS
    const previous = this.archive.findSend(requestKey, window)
    if (previous) return { chatJid, id: previous, deduplicated: true }

    const quoted = request.replyTo ? this.rawMessage(chatJid, request.replyTo) : undefined
    const sent = await socket.sendMessage(chatJid, { text, mentions }, { quoted })
    const id = sent?.key.id
    if (!id) throw new Error("WhatsApp did not return a message id")
    // The echo of the sent message is what makes `message.sent`.
    this.archive.rememberSend(requestKey, id)
    return { chatJid, id, deduplicated: false }
  }

  async react(chatJid: string, messageId: string, emoji: string): Promise<void> {
    const socket = this.connectedSocket()
    const jid = this.resolveChat(chatJid)
    const { key } = this.rawMessage(jid, messageId)
    await socket.sendMessage(jid, { react: { text: emoji, key } })
    if (this.archive.saveReaction(jid, messageId, null, emoji, Date.now())) {
      this.publish([{ type: "reaction", chatJid: jid, id: messageId, senderJid: null, emoji }])
    }
  }

  /** Returns the chat's JID, resolved from a phone number if one was given. */
  async editMessage(chatJid: string, messageId: string, text: string): Promise<string> {
    const socket = this.connectedSocket()
    const jid = this.resolveChat(chatJid)
    const { key } = this.rawMessage(jid, messageId)
    if (!key.fromMe) {
      throw new RequestError(
        403,
        "not_own_message",
        "only messages sent by this account can be edited",
      )
    }
    const sentAt = this.archive.getMessage(jid, messageId)?.sentAt ?? 0
    const age = Math.floor(Date.now() / 1000) - sentAt
    if (age > EDIT_WINDOW_SECONDS) {
      throw new RequestError(
        409,
        "edit_window_closed",
        `WhatsApp allows edits for ${EDIT_WINDOW_SECONDS / 60} minutes; message ${messageId} was sent ${Math.floor(age / 60)} minutes ago`,
      )
    }
    await socket.sendMessage(jid, { text, edit: key })
    if (this.archive.applyEdit(jid, messageId, text)) {
      this.publish([{ type: "edit", chatJid: jid, id: messageId }])
    }
    return jid
  }

  async deleteMessage(chatJid: string, messageId: string): Promise<void> {
    const socket = this.connectedSocket()
    const jid = this.resolveChat(chatJid)
    const { key } = this.rawMessage(jid, messageId)
    await socket.sendMessage(jid, { delete: key })
    if (this.archive.applyRevoke(jid, messageId)) {
      this.publish([{ type: "revoke", chatJid: jid, id: messageId }])
    }
  }

  async markRead(chatJid: string): Promise<number> {
    const socket = this.connectedSocket()
    const jid = this.resolveChat(chatJid)
    const unread = Math.max(this.archive.getChat(jid)?.unreadCount ?? 0, 1)
    const keys = this.archive
      .latestIncoming(jid, unread)
      .flatMap(({ id }) => this.archive.getRaw(jid, id) ?? [])
      .map((raw) => decodeRaw(raw).key)
    if (keys.length > 0) await socket.readMessages(keys)
    this.archive.saveChats([{ jid, unreadCount: 0 }])
    return keys.length
  }

  async saveContact(target: string, name: ContactName): Promise<Identity> {
    return saveContact(this.connectedSocket(), this.archive, target, name)
  }

  async removeContact(target: string): Promise<Identity> {
    return removeContact(this.connectedSocket(), this.archive, target)
  }

  async checkNumbers(phones: readonly string[]): Promise<NumberCheck[]> {
    const socket = this.connectedSocket()
    const results =
      (await socket.onWhatsApp(...phones.map((phone) => `${phone}@s.whatsapp.net`))) ?? []
    return phones.map((phone) => {
      const found = results.find((result) => result.jid.startsWith(`${phone}@`))
      return { phone, onWhatsApp: found?.exists ?? false, jid: found?.exists ? found.jid : null }
    })
  }

  async getGroupInfo(groupJid: string): Promise<GroupInfo> {
    const group = await this.groupMetadata(groupJid)
    return {
      jid: group.id,
      subject: group.subject,
      description: group.desc ?? null,
      owner: group.ownerPn ?? group.owner ?? null,
      // The bridge reports a missing date as NaN.
      createdAt: Number.isFinite(group.creation) ? (group.creation ?? null) : null,
      size: group.size ?? group.participants.length,
      announceOnly: group.announce ?? false,
      restricted: group.restrict ?? false,
      addressingMode: addressingOf(group),
      participants: group.participants.map((participant) => {
        const person = this.archive.getPerson(jidNormalizedUser(participant.id))
        const names = this.archive.namesBySource(person.jid)
        return {
          jid: person.jid,
          name: person.name ?? participant.notify ?? null,
          contactName: names.contact ?? null,
          profileName: names.push ?? names.business ?? participant.notify ?? null,
          phone: person.phone,
          admin: participant.admin ?? null,
        }
      }),
    }
  }

  /** Shows "typing…" or "recording audio…" in a chat until a message goes out, `paused`, or about 25 s pass. */
  async setPresence(chatJid: string, state: Presence): Promise<void> {
    const socket = this.connectedSocket()
    const jid = this.resolveChat(chatJid)
    await socket.sendPresenceUpdate(state === "typing" ? "composing" : state, jid)
  }

  me(): Me {
    const jid = numberOf(this.socket) ?? this.owner()
    return {
      jid,
      lid: this.ownLid(jid),
      phone: jid ? `+${jid.slice(0, jid.indexOf("@"))}` : null,
      name: this.socket?.user?.name || null,
      state: this.state,
    }
  }

  /** A chat's messages in the shape webhooks carry them. */
  async readMessages(query: MessageQuery, transcribeVoice: boolean): Promise<MessagesData> {
    const chatJid = this.resolveChat(query.chatJid)
    const messages = await this.getMessages({ ...query, chatJid }, transcribeVoice)
    const self = this.self()
    return {
      chat: chatData(this.chatRef(chatJid)),
      messages: messages.map((message) => messageData(message, this.senderOf(message), self)),
    }
  }

  /** One message in the shape webhooks carry it, or null when the archive lacks it. */
  async readMessage(
    chatJid: string,
    id: string,
    transcribeVoice: boolean,
  ): Promise<MessageData | null> {
    const jid = this.resolveChat(chatJid)
    const [message] = await this.getMessageContext(jid, id, 0, transcribeVoice)
    if (!message) return null
    return {
      chat: chatData(this.chatRef(jid)),
      message: messageData(message, this.senderOf(message), this.self()),
    }
  }

  async getProfile(jid: string): Promise<Profile> {
    const socket = this.connectedSocket()
    const target = this.resolveChat(jid)
    // Both lookups fail when the contact hides them from us; that is not an error.
    const [picture, statuses] = await Promise.all([
      socket.profilePictureUrl(target, "image").catch(() => undefined),
      socket.fetchStatus(target).catch(() => undefined),
    ])
    const person = this.archive.getPerson(target)
    return {
      jid: target,
      name: person.name,
      phone: person.phone,
      aliases: person.aliases,
      about: AboutResult.safeParse(statuses?.[0]).data?.status.status ?? null,
      pictureUrl: picture ?? null,
    }
  }

  // Returns how many messages the phone sent back within HISTORY_WAIT_MS.
  async loadOlderMessages(chatJid: string, count: number): Promise<number> {
    const socket = this.connectedSocket()
    const jid = this.resolveChat(chatJid)
    const oldest = this.archive.oldestMessage(jid)
    if (!oldest) throw new Error(`no archived messages in ${jid} to page back from`)
    const { key } = this.rawMessage(jid, oldest.id)
    const before = this.archive.stats().messages
    const arrived = new Promise<void>((resolve) => {
      this.historyWaiters.set(jid, resolve)
      setTimeout(resolve, HISTORY_WAIT_MS)
    })
    await socket.fetchMessageHistory(count, key, oldest.sentAt)
    await arrived
    this.historyWaiters.delete(jid)
    return this.archive.stats().messages - before
  }

  listWebhooks(): Webhook[] {
    return this.webhooks.list()
  }

  getWebhook(id: string): { webhook: Webhook; deliveries: Delivery[] } | null {
    const webhook = this.webhooks.get(id)
    return webhook ? { webhook, deliveries: this.webhooks.recent(id) } : null
  }

  // The secret is shown here and on rotation only.
  createWebhook(
    input: WebhookCreate,
    account: string,
    via: CreatedVia,
  ): { webhook: Webhook; secret: string } {
    if (this.webhooks.count() >= MAX_WEBHOOKS) {
      throw new Error(`an account has at most ${MAX_WEBHOOKS} webhooks`)
    }
    const fields = fieldsOf(input)
    const chats = fields.chats?.map((chat) => this.resolveChat(chat)) ?? null
    const secret = newSecret()
    const webhook = this.webhooks.create(
      newId("wh"),
      { ...fields, chats, account, createdVia: via },
      secret,
      Date.now(),
    )
    return { webhook, secret }
  }

  updateWebhook(
    id: string,
    input: WebhookUpdate,
  ): { webhook: Webhook; secret: string | null } | null {
    const patch = patchOf(input)
    if (patch.chats) patch.chats = patch.chats.map((chat) => this.resolveChat(chat))
    const now = Date.now()
    let secret: string | null = null
    if (input.rotate_secret && this.webhooks.get(id)) {
      secret = newSecret()
      this.webhooks.rotateSecret(id, secret, now + SECRET_GRACE_MS, now)
    }
    const webhook = this.webhooks.update(id, patch, now)
    if (!webhook) return null
    if (patch.enabled) this.ctx.waitUntil(this.deliver())
    return { webhook, secret }
  }

  deleteWebhook(id: string): boolean {
    return this.webhooks.delete(id)
  }

  /** Sends `webhook.test` right away, outside the queue, and says how it went. */
  async testWebhook(id: string): Promise<Attempt | null> {
    const now = Date.now()
    const target = this.webhooks.target(id, now)
    const webhook = this.webhooks.get(id)
    if (!target || !webhook) return null
    const event = testEvent(now)
    return post(target, event.id, serialize(event, webhook.account), {
      fetch: (url, init) => fetch(url, init),
      now: () => Date.now(),
    })
  }

  // WhatsApp's CDN drops media after a few weeks. The phone, when online, can upload
  // it again under a new path.
  private async keepAlive(): Promise<void> {
    await this.ensureConnected()
    await this.scheduleAlarm()
  }

  // One alarm serves the heartbeat and the webhook retries: it is set for whichever
  // comes first, and never pushed later, so steady traffic cannot starve the heartbeat.
  private async scheduleAlarm(): Promise<void> {
    const now = Date.now()
    const wants = await this.ctx.storage.get<boolean>(WANTS_CONNECTION)
    const due = this.webhooks.nextDueAt()
    const times = [
      ...(wants ? [now + HEARTBEAT_MS] : []),
      ...(due === null ? [] : [Math.max(due, now)]),
    ]
    if (times.length === 0) {
      await this.ctx.storage.deleteAlarm()
      return
    }
    const target = Math.min(...times)
    const current = await this.ctx.storage.getAlarm()
    if (current !== null && current > now && current <= target) return
    await this.ctx.storage.setAlarm(target)
  }

  // Live archive changes become webhook events in the same turn as the writes, so
  // the Durable Object commits both together.
  private publish(changes: readonly ArchiveChange[]): void {
    if (!this.webhooks.hasActive()) return
    const now = Date.now()
    const events = changes.flatMap((change) => this.eventFor(change, now) ?? [])
    this.emit(events)
  }

  private emit(events: readonly WebhookEvent[]): void {
    const now = Date.now()
    let queued = 0
    const canonical = (jid: string) => this.archive.canonical(jid)
    for (const event of events) queued += this.webhooks.enqueue(event, now, canonical)
    if (queued > 0) this.ctx.waitUntil(this.deliver())
  }

  private eventFor(change: ArchiveChange, now: number): WebhookEvent | null {
    const chat = this.chatRef(change.chatJid)
    if (change.type === "reaction") {
      const sender = change.senderJid === null ? null : this.person(change.senderJid)
      return reactionEvent(chat, change.id, sender, change.emoji, now)
    }
    const message = this.archive.getMessage(change.chatJid, change.id)
    if (!message) return null
    const sender = this.senderOf(message)
    const type =
      change.type === "edit"
        ? "message.edited"
        : change.type === "revoke"
          ? "message.deleted"
          : message.fromMe
            ? "message.sent"
            : "message.received"
    return messageEvent(type, chat, message, sender, this.self(), now)
  }

  // The socket knows the LID while connected; the archive remembers it otherwise.
  private ownLid(jid: string | null): string | null {
    const socketLid = this.socket?.user?.lid
    if (socketLid) return jidNormalizedUser(socketLid)
    const known = jid ? this.archive.canonical(jid) : null
    return known?.endsWith("@lid") ? known : null
  }

  private self(): Self {
    const { jid, lid } = this.me()
    return new Set([jid, lid, jid ? this.archive.canonical(jid) : null].filter((id) => id !== null))
  }

  // Asks WhatsApp, and learns each member's LID and phone pair on the way.
  private async groupMetadata(groupJid: string): Promise<GroupMetadata> {
    const socket = this.connectedSocket()
    const jid = jidNormalizedUser(groupJid)
    if (!jid.endsWith("@g.us"))
      throw new RequestError(400, "invalid_address", `${jid} is not a group`)
    let group: GroupMetadata
    try {
      group = await socket.groupMetadata(jid)
    } catch (error) {
      const status = whatsappStatus(error)
      if (status === 403 || status === 404) {
        throw new RequestError(
          404,
          "not_found",
          `group ${jid} not found, or this number is not in it`,
        )
      }
      throw new RequestError(502, "whatsapp_error", errorText(error))
    }
    for (const participant of group.participants) {
      const ids = [participant.id, participant.lid, participant.phoneNumber].filter(
        (id): id is string => Boolean(id),
      )
      const lid = ids.find((id) => id.endsWith("@lid"))
      const pn = ids.find((id) => id.endsWith("@s.whatsapp.net"))
      if (lid && pn) this.archive.learnPair(jidNormalizedUser(pn), jidNormalizedUser(lid))
    }
    this.addressing.set(jid, { mode: addressingOf(group), at: Date.now() })
    return group
  }

  // `null` when WhatsApp would not say; mentions then go by the canonical JID.
  private async addressingMode(groupJid: string): Promise<"lid" | "pn" | null> {
    const cached = this.addressing.get(groupJid)
    if (cached && Date.now() - cached.at < ADDRESSING_TTL_MS) return cached.mode
    return this.groupMetadata(groupJid).then(addressingOf, () => null)
  }

  // In a group, people are mentioned by the JID kind the group addresses them with.
  private async mentionTargets(
    chatJid: string,
    mentions: readonly string[],
  ): Promise<MentionTarget[]> {
    if (mentions.length === 0) return []
    const mode = chatJid.endsWith("@g.us") ? await this.addressingMode(chatJid) : null
    return mentions.map((mention) => {
      const typed = mention.includes("@")
        ? jidNormalizedUser(mention)
        : `${mention.replace(/\D/g, "")}@s.whatsapp.net`
      const jid = this.resolveChat(mention)
      const pn = this.archive.phoneJidOf(jid)
      const lid = jid.endsWith("@lid") ? jid : null
      const known = [typed, jid, pn, lid].filter((id): id is string => id !== null)
      return { jid: mentionJid({ jid, pn, lid }, mode), known }
    })
  }

  private chatRef(jid: string): ChatRef {
    const chat = this.archive.getChat(jid)
    return { jid, name: chat?.name ?? null, type: chat?.type ?? chatTypeOf(jid) }
  }

  private person(jid: string): Person {
    const { name, phone } = this.archive.getPerson(jid)
    return { jid, name, phone }
  }

  // Webhooks name the sender of received messages only.
  private senderOf(message: ArchivedMessage): Person | null {
    return message.fromMe || !message.senderJid ? null : this.person(message.senderJid)
  }

  // Reports links and unlinks, not every reconnect.
  private connectionChanged(
    state: ConnectionChange,
    me: string | null,
    error: string | null,
  ): void {
    const linked = this.ctx.storage.kv.get<boolean>(LINKED) ?? false
    if (state === "linked" && linked) return
    if (state === "unlinked" && !linked) return
    if (state !== "refused") this.ctx.storage.kv.put(LINKED, state === "linked")
    this.emit([connectionEvent(state, { me, owner: this.owner(), error }, Date.now())])
  }

  // One drain at a time; changes that arrive meanwhile run another round.
  private deliver(): Promise<void> {
    if (this.draining) {
      this.drainAgain = true
      return this.draining
    }
    this.draining = this.drainAll().finally(() => {
      this.draining = null
    })
    return this.draining
  }

  private async drainAll(): Promise<void> {
    try {
      do {
        this.drainAgain = false
        await drain({
          store: this.webhooks,
          fetch: (url, init) => fetch(url, init),
          now: () => Date.now(),
          transcribe: (voice) => this.transcribedMessage(voice.chatJid, voice.id),
        })
      } while (this.drainAgain)
      if (Date.now() - this.prunedAt > PRUNE_EVERY_MS) {
        this.webhooks.prune(Date.now())
        this.prunedAt = Date.now()
      }
    } catch (error) {
      console.warn("webhook delivery failed:", errorText(error))
    } finally {
      await this.scheduleAlarm()
    }
  }

  private async transcribedMessage(
    chatJid: string,
    id: string,
  ): Promise<Record<string, unknown> | null> {
    const message = this.archive.getMessage(chatJid, id)
    if (!message) return null
    const [read] = await this.transcripts.read([message], true)
    if (!read) return null
    return messageData(read, this.senderOf(read), this.self())
  }

  // Concurrent callers share one attempt.
  private ensureConnected(): Promise<void> {
    if (this.socket) return Promise.resolve()
    this.opening ??= this.openSocket().finally(() => {
      this.opening = null
    })
    return this.opening
  }

  private async openSocket(): Promise<void> {
    this.state = "connecting"
    const [auth, version] = await Promise.all([
      createAuthenticationState(this.store),
      fetchWaWebVersion(),
    ])
    // The socket connects on creation; progress arrives as `connection.update`.
    const socket = makeSocket({ auth, version })
    // A socket that was dropped or refused no longer speaks for the account.
    socket.ev.on("connection.update", (update) => {
      if (this.socket === socket) this.onConnectionUpdate(socket, update)
    })
    syncArchive(socket, this.archive, {
      accepts: () => this.socket === socket && this.ownedBy(socket),
      onMessages: (messages, source) => {
        if (source !== "history") return
        for (const chat of new Set(messages.map((message) => message.chatJid))) {
          this.historyWaiters.get(chat)?.()
        }
      },
      onHistoryBatch: (progress) => {
        this.historyBatchAt = Date.now()
        if (progress !== null) this.historyProgress = progress
      },
      onChanges: (changes) => this.publish(changes),
    })
    this.socket = socket
  }

  private owner(): string | null {
    return this.ctx.storage.kv.get<string>(OWNER) ?? null
  }

  // Before the first link nobody owns the archive, so any number may write to it.
  private ownedBy(socket: Socket): boolean {
    const owner = this.owner()
    return owner === null || numberOf(socket) === owner
  }

  private onConnectionUpdate(socket: Socket, update: HostConnectionUpdate): void {
    if (update.qr) {
      this.state = "pairing"
      this.qrSvg = renderSVG(update.qr)
      this.lastError = null
    }
    if (update.connection === "open") {
      const me = numberOf(socket)
      const owner = this.owner()
      if (me && owner && me !== owner) {
        this.ctx.waitUntil(this.refuse(socket, me))
        return
      }
      if (me && !owner) this.ctx.storage.kv.put(OWNER, me)
      this.state = "open"
      this.qrSvg = null
      this.lastError = null
      this.connectionChanged("linked", me, null)
      this.ctx.waitUntil(this.refreshGroups())
    } else if (update.connection === "close") {
      // `close` is terminal for this socket; the next heartbeat builds a new one,
      // unless the device was unlinked.
      const error = update.lastDisconnect?.error
      this.lastError = error?.message ?? "connection closed"
      if (isBoom(error, DisconnectReason.loggedOut)) {
        this.forgetCredentials()
        this.connectionChanged("unlinked", numberOf(socket), this.lastError)
        void this.ctx.storage.put(WANTS_CONNECTION, false)
      } else {
        this.socket = null
        this.state = "closed"
      }
    }
  }

  // Reconnects are frequent and subjects rarely change, so this is throttled.
  private async refreshGroups(): Promise<void> {
    const last = (await this.ctx.storage.get<number>(GROUPS_REFRESHED_AT)) ?? 0
    if (!this.socket || Date.now() - last < GROUP_REFRESH_MS) return
    await this.ctx.storage.put(GROUPS_REFRESHED_AT, Date.now())
    await refreshGroupNames(this.socket, this.archive).catch((error: Error) => {
      console.warn("group refresh failed:", error.message)
    })
  }

  // WhatsApp unlinks devices on its own, even after internal errors, so the archive
  // stays: linking the same number again merges into it.
  private forgetCredentials(): void {
    this.socket = null
    this.store.clear()
    this.state = "logged_out"
  }

  // Unlinking the refused device keeps a dead entry off that phone.
  private async refuse(socket: Socket, me: string): Promise<void> {
    this.socket = null
    this.state = "logged_out"
    this.refused = me
    this.lastError = `${me} is not the owner of this account`
    this.connectionChanged("refused", me, this.lastError)
    await this.ctx.storage.put(WANTS_CONNECTION, false)
    await this.scheduleAlarm()
    await socket.logout().catch(() => socket.end(undefined))
    this.store.clear()
  }

  private connectedSocket(): Socket {
    if (!this.socket || this.state !== "open") {
      throw new RequestError(503, "not_connected", `WhatsApp is not connected (${this.state})`)
    }
    return this.socket
  }

  private resolveChat(to: string): string {
    return resolveAddress(this.archive, to)
  }

  // Edits, reactions, receipts and history requests must reference the key with
  // the JIDs WhatsApp addressed the message with, not the canonical ones.
  private rawMessage(chatJid: string, messageId: string): KeyedMessage {
    const raw = this.archive.getRaw(chatJid, messageId)
    if (!raw) {
      throw new RequestError(
        404,
        "message_not_found",
        `message ${messageId} not found in ${chatJid}`,
      )
    }
    return decodeRaw(raw)
  }
}
