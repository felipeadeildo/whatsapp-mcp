/**
 * One Durable Object per WhatsApp account. It owns the linked-device session:
 * the baileyrs socket (whatsapp-rust compiled to WASM), the engine's state and
 * the message archive in the object's SQLite, and the outbound WebSocket.
 *
 * Its public methods are the RPC surface the HTTP API and the MCP tools call.
 * Reads come from the archive; actions go through the socket.
 *
 * Keeping the object resident: an open outbound WebSocket pins a Durable Object
 * for at most 15 minutes, and an idle object is evicted 70-140s after its last
 * event. A short repeating alarm is an event, so it keeps the object in memory
 * and rebuilds the socket after evictions, deploys and terminal closes.
 * Transient drops are retried by the Rust engine itself and never reach us.
 */
import {
  createAuthenticationState,
  DisconnectReason,
  type HostConnectionUpdate,
} from "@oxidezap/baileyrs/host"
import { isBoom } from "@oxidezap/baileyrs/lib/Utils/boom.js"
import type { WAMessageKey } from "@oxidezap/baileyrs/lib/Types/Message.js"
import { DurableObject } from "cloudflare:workers"
import { renderSVG } from "uqr"
import { z } from "zod"

import {
  Archive,
  type ArchivedMessage,
  type ArchiveStats,
  type ChatListQuery,
  type ChatSummary,
  type ContactSummary,
  type MessageQuery,
  type SearchHit,
  type SearchQuery,
} from "./store/archive"
import { durableKvStore } from "./store/durable-kv"
import { fetchWaWebVersion } from "./wa-version"
import { makeSocket, type Socket } from "./whatsapp/socket"
import { chatOf, syncArchive } from "./whatsapp/sync"
import "./wasm"

const HEARTBEAT_MS = 30_000
const WANTS_CONNECTION = "wants_connection"
/** How long a send without an explicit idempotency key is deduplicated. */
const IMPLICIT_DEDUP_SECONDS = 120
const EXPLICIT_DEDUP_SECONDS = 24 * 60 * 60
const HISTORY_WAIT_MS = 20_000

/** The status protocol's entry in a USync result, typed `unknown` by the library. */
const AboutResult = z.object({ status: z.object({ status: z.string().nullish() }) })

export type ConnectionState = "idle" | "connecting" | "pairing" | "open" | "closed" | "logged_out"

export interface AccountStatus {
  readonly state: ConnectionState
  readonly me: string | null
  readonly myName: string | null
  readonly qrSvg: string | null
  readonly lastError: string | null
  readonly startedAt: number
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
  /** `true` when an identical earlier request was answered instead of sending again. */
  readonly deduplicated: boolean
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
  readonly participants: readonly {
    readonly jid: string
    readonly admin: "admin" | "superadmin" | null
  }[]
}

export interface Profile {
  readonly jid: string
  readonly name: string | null
  readonly about: string | null
  readonly pictureUrl: string | null
}

export class WhatsAppAccount extends DurableObject<Env> {
  private readonly store = durableKvStore(this.ctx.storage)
  private readonly archive = new Archive(this.ctx.storage)
  private socket: Socket | null = null
  private opening: Promise<void> | null = null
  private state: ConnectionState = "idle"
  private qrSvg: string | null = null
  private lastError: string | null = null
  private readonly startedAt = Date.now()
  /** Chats waiting for an on-demand history batch, resolved when one arrives. */
  private readonly historyWaiters = new Map<string, () => void>()

  // Lifecycle

  async start(): Promise<AccountStatus> {
    await this.ctx.storage.put(WANTS_CONNECTION, true)
    await this.keepAlive()
    return this.status()
  }

  async logout(): Promise<AccountStatus> {
    await this.ctx.storage.put(WANTS_CONNECTION, false)
    await this.ctx.storage.deleteAlarm()
    // Unlinking needs a paired session; while still pairing only the socket goes.
    if (this.socket?.user) await this.socket.logout()
    else await this.socket?.end(undefined)
    this.forgetDevice()
    return this.status()
  }

  status(): AccountStatus {
    return {
      state: this.state,
      me: this.socket?.user?.id ? chatOf(this.socket.user.id) : null,
      myName: this.socket?.user?.name ?? null,
      qrSvg: this.state === "pairing" ? this.qrSvg : null,
      lastError: this.lastError,
      startedAt: this.startedAt,
    }
  }

  overview(): Overview {
    return { ...this.status(), archive: this.archive.stats() }
  }

  async alarm(): Promise<void> {
    if (await this.ctx.storage.get<boolean>(WANTS_CONNECTION)) await this.keepAlive()
  }

  // Reads (archive)

  listChats(query: ChatListQuery): ChatSummary[] {
    return this.archive.listChats(query)
  }

  findChats(text: string, limit: number): ChatSummary[] {
    return this.archive.findChats(text, limit)
  }

  getMessages(query: MessageQuery): ArchivedMessage[] {
    return this.archive.getMessages({ ...query, chatJid: this.resolveChat(query.chatJid) })
  }

  getMessageContext(chatJid: string, id: string, around: number): ArchivedMessage[] {
    return this.archive.getMessageContext(this.resolveChat(chatJid), id, around)
  }

  searchMessages(query: SearchQuery): SearchHit[] {
    const chatJid = query.chatJid === undefined ? undefined : this.resolveChat(query.chatJid)
    return this.archive.searchMessages({ ...query, chatJid })
  }

  findContacts(text: string, limit: number): ContactSummary[] {
    return this.archive.findContacts(text, limit)
  }

  // Actions (socket)

  async send(request: SendRequest): Promise<SendResult> {
    const socket = this.connectedSocket()
    const chatJid = this.resolveChat(request.to)
    const requestKey =
      request.idempotencyKey ??
      (await digest([chatJid, request.text, request.replyTo ?? "", ...(request.mentions ?? [])]))
    const window = request.idempotencyKey ? EXPLICIT_DEDUP_SECONDS : IMPLICIT_DEDUP_SECONDS
    const previous = this.archive.findSend(requestKey, window)
    if (previous) return { chatJid, id: previous, deduplicated: true }

    const quoted = request.replyTo ? this.archive.getMessage(chatJid, request.replyTo) : null
    if (request.replyTo && !quoted)
      throw new Error(`message ${request.replyTo} not found in ${chatJid}`)
    const sent = await socket.sendMessage(
      chatJid,
      {
        text: request.text,
        mentions: (request.mentions ?? []).map((jid) => this.resolveChat(jid)),
      },
      quoted
        ? { quoted: { key: keyOf(quoted), message: { conversation: quoted.text ?? "" } } }
        : undefined,
    )
    const id = sent?.key.id
    if (!id) throw new Error("WhatsApp did not return a message id")
    this.archive.rememberSend(requestKey, id)
    return { chatJid, id, deduplicated: false }
  }

  async react(chatJid: string, messageId: string, emoji: string): Promise<void> {
    const socket = this.connectedSocket()
    const message = this.requireMessage(chatJid, messageId)
    await socket.sendMessage(message.chatJid, { react: { text: emoji, key: keyOf(message) } })
    this.archive.saveReaction(message.chatJid, message.id, null, emoji, Date.now())
  }

  async editMessage(chatJid: string, messageId: string, text: string): Promise<void> {
    const socket = this.connectedSocket()
    const message = this.requireMessage(chatJid, messageId)
    if (!message.fromMe) throw new Error("only messages sent by this account can be edited")
    await socket.sendMessage(message.chatJid, { text, edit: keyOf(message) })
    this.archive.applyEdit(message.chatJid, message.id, text)
  }

  async deleteMessage(chatJid: string, messageId: string): Promise<void> {
    const socket = this.connectedSocket()
    const message = this.requireMessage(chatJid, messageId)
    await socket.sendMessage(message.chatJid, { delete: keyOf(message) })
    this.archive.applyRevoke(message.chatJid, message.id)
  }

  /** Marks the chat read up to its latest message, as opening it on the phone does. */
  async markRead(chatJid: string): Promise<number> {
    const socket = this.connectedSocket()
    const jid = this.resolveChat(chatJid)
    const chat = this.archive.getChat(jid)
    const unread = Math.max(chat?.unreadCount ?? 0, 1)
    const incoming = this.archive
      .getMessages({ chatJid: jid, limit: unread })
      .filter((message) => !message.fromMe)
    if (incoming.length > 0) await socket.readMessages(incoming.map(keyOf))
    this.archive.saveChats([
      {
        jid,
        name: null,
        unreadCount: 0,
        lastMessageAt: null,
        archived: null,
        pinned: null,
        mutedUntil: null,
      },
    ])
    return incoming.length
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
    const socket = this.connectedSocket()
    const group = await socket.groupMetadata(this.resolveChat(groupJid))
    return {
      jid: group.id,
      subject: group.subject,
      description: group.desc ?? null,
      owner: group.owner ?? null,
      createdAt: group.creation ?? null,
      size: group.size ?? group.participants.length,
      announceOnly: group.announce ?? false,
      restricted: group.restrict ?? false,
      participants: group.participants.map((participant) => ({
        jid: participant.phoneNumber ?? participant.id,
        admin: participant.admin ?? null,
      })),
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
    const about = AboutResult.safeParse(statuses?.[0]).data?.status.status ?? null
    const name =
      this.archive.getChat(target)?.name ?? this.archive.findContacts(target, 1)[0]?.name ?? null
    return {
      jid: target,
      name,
      about,
      pictureUrl: picture ?? null,
    }
  }

  /**
   * Asks the phone for messages older than the oldest archived one and waits
   * briefly for them to arrive. Returns how many new messages were stored.
   */
  async loadOlderMessages(chatJid: string, count: number): Promise<number> {
    const socket = this.connectedSocket()
    const jid = this.resolveChat(chatJid)
    const oldest = this.archive.oldestMessage(jid)
    if (!oldest) throw new Error(`no archived messages in ${jid} to page back from`)
    const before = this.archive.stats().messages
    const arrived = new Promise<void>((resolve) => {
      this.historyWaiters.set(jid, resolve)
      setTimeout(resolve, HISTORY_WAIT_MS)
    })
    await socket.fetchMessageHistory(
      count,
      { remoteJid: jid, id: oldest.id, fromMe: oldest.fromMe },
      oldest.sentAt,
    )
    await arrived
    this.historyWaiters.delete(jid)
    return this.archive.stats().messages - before
  }

  // Connection

  private async keepAlive(): Promise<void> {
    await this.ensureConnected()
    await this.ctx.storage.setAlarm(Date.now() + HEARTBEAT_MS)
  }

  /** Opens the socket unless one is open or opening; concurrent callers share the attempt. */
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
    socket.ev.on("connection.update", (update) => this.onConnectionUpdate(update))
    syncArchive(socket, this.archive, {
      onMessages: (messages, source) => {
        if (source !== "history") return
        for (const chat of new Set(messages.map((message) => message.chatJid))) {
          this.historyWaiters.get(chat)?.()
        }
      },
    })
    this.socket = socket
  }

  private onConnectionUpdate(update: HostConnectionUpdate): void {
    if (update.qr) {
      this.state = "pairing"
      this.qrSvg = renderSVG(update.qr)
    }
    if (update.connection === "open") {
      this.state = "open"
      this.qrSvg = null
      this.lastError = null
    } else if (update.connection === "close") {
      // `close` is terminal for this socket; the next heartbeat builds a new one,
      // unless the device was unlinked.
      const error = update.lastDisconnect?.error
      this.lastError = error?.message ?? "connection closed"
      if (isBoom(error, DisconnectReason.loggedOut)) {
        this.forgetDevice()
        void this.ctx.storage.put(WANTS_CONNECTION, false)
      } else {
        this.socket = null
        this.state = "closed"
      }
    }
  }

  private forgetDevice(): void {
    this.socket = null
    this.store.clear()
    this.archive.clear()
    this.state = "logged_out"
  }

  // Helpers

  private connectedSocket(): Socket {
    if (!this.socket || this.state !== "open")
      throw new Error(`WhatsApp is not connected (${this.state})`)
    return this.socket
  }

  /** Accepts a JID or a phone number with country code, returns a normalized JID. */
  private resolveChat(to: string): string {
    if (to.includes("@")) return chatOf(to) ?? to
    const digits = to.replace(/\D/g, "")
    if (!digits) throw new Error(`not a JID or phone number: ${to}`)
    return `${digits}@s.whatsapp.net`
  }

  private requireMessage(chatJid: string, messageId: string): ArchivedMessage {
    const jid = this.resolveChat(chatJid)
    const message = this.archive.getMessage(jid, messageId)
    if (!message) throw new Error(`message ${messageId} not found in ${jid}`)
    return message
  }
}

function keyOf(message: ArchivedMessage): WAMessageKey {
  return {
    remoteJid: message.chatJid,
    id: message.id,
    fromMe: message.fromMe,
    participant: message.chatJid.endsWith("@g.us") ? (message.senderJid ?? undefined) : undefined,
  }
}

async function digest(parts: readonly string[]): Promise<string> {
  const bytes = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(parts.join("\u0000")),
  )
  return Array.from(new Uint8Array(bytes), (byte) => byte.toString(16).padStart(2, "0")).join("")
}
