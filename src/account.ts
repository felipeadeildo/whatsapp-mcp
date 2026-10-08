// One Durable Object per WhatsApp account. An open outbound WebSocket pins it for
// at most 15 minutes and an idle object is evicted after 70-140s, so a 30s alarm
// keeps it in memory and rebuilds the socket after evictions and deploys.
import {
  createAuthenticationState,
  DisconnectReason,
  type HostConnectionUpdate,
} from "@oxidezap/baileyrs/host"
import { isBoom } from "@oxidezap/baileyrs/lib/Utils/boom.js"
import { jidNormalizedUser } from "@oxidezap/baileyrs/lib/WABinary/jid-utils.js"
import { DurableObject } from "cloudflare:workers"
import { renderSVG } from "uqr"
import { z } from "zod"

import {
  Archive,
  type ArchivedMessage,
  type ArchiveStats,
  type ChatListQuery,
  type ChatSummary,
  type MessageQuery,
  type Match,
  type SearchHit,
  type SearchQuery,
} from "./store/archive"
import { durableKvStore } from "./store/durable-kv"
import type { Identity } from "./store/names"
import { fetchWaWebVersion } from "./wa-version"
import { type ContactName, removeContact, resolveAddress, saveContact } from "./whatsapp/contacts"
import { decodeRaw, type KeyedMessage } from "./whatsapp/raw"
import { makeSocket, type Socket } from "./whatsapp/socket"
import { refreshGroupNames, syncArchive } from "./whatsapp/sync"
import "./wasm"

const HEARTBEAT_MS = 30_000
const WANTS_CONNECTION = "wants_connection"
const IMPLICIT_DEDUP_SECONDS = 120
const EXPLICIT_DEDUP_SECONDS = 24 * 60 * 60
const HISTORY_WAIT_MS = 20_000
const GROUPS_REFRESHED_AT = "groups_refreshed_at"
const GROUP_REFRESH_MS = 6 * 60 * 60 * 1000

// The library types USync results as `unknown`.
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
    readonly name: string | null
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
  private readonly startedAt = Date.now()
  private readonly historyWaiters = new Map<string, () => void>()

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
    const user = this.socket?.user
    return {
      state: this.state,
      me: user?.id ? jidNormalizedUser(user.id) : null,
      myName: user?.name || null,
      qrSvg: this.state === "pairing" ? this.qrSvg : null,
      lastError: this.lastError,
      startedAt: this.startedAt,
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
    if (await this.ctx.storage.get<boolean>(WANTS_CONNECTION)) await this.keepAlive()
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

  getMessages(query: MessageQuery): ArchivedMessage[] {
    const senderJid = query.senderJid === undefined ? undefined : this.resolveChat(query.senderJid)
    return this.archive.getMessages({
      ...query,
      chatJid: this.resolveChat(query.chatJid),
      senderJid,
    })
  }

  getMessageContext(chatJid: string, id: string, around: number): ArchivedMessage[] {
    return this.archive.getMessageContext(this.resolveChat(chatJid), id, around)
  }

  searchMessages(query: SearchQuery): SearchHit[] {
    const chatJid = query.chatJid === undefined ? undefined : this.resolveChat(query.chatJid)
    const senderJid = query.senderJid === undefined ? undefined : this.resolveChat(query.senderJid)
    return this.archive.searchMessages({ ...query, chatJid, senderJid })
  }

  async send(request: SendRequest): Promise<SendResult> {
    const socket = this.connectedSocket()
    const chatJid = this.resolveChat(request.to)
    const mentions = (request.mentions ?? []).map((jid) => this.resolveChat(jid))
    const requestKey =
      request.idempotencyKey ??
      (await digest([chatJid, request.text, request.replyTo ?? "", ...mentions]))
    const window = request.idempotencyKey ? EXPLICIT_DEDUP_SECONDS : IMPLICIT_DEDUP_SECONDS
    const previous = this.archive.findSend(requestKey, window)
    if (previous) return { chatJid, id: previous, deduplicated: true }

    const quoted = request.replyTo ? this.rawMessage(chatJid, request.replyTo) : undefined
    const sent = await socket.sendMessage(chatJid, { text: request.text, mentions }, { quoted })
    const id = sent?.key.id
    if (!id) throw new Error("WhatsApp did not return a message id")
    this.archive.rememberSend(requestKey, id)
    return { chatJid, id, deduplicated: false }
  }

  async react(chatJid: string, messageId: string, emoji: string): Promise<void> {
    const socket = this.connectedSocket()
    const jid = this.resolveChat(chatJid)
    const { key } = this.rawMessage(jid, messageId)
    await socket.sendMessage(jid, { react: { text: emoji, key } })
    this.archive.saveReaction(jid, messageId, null, emoji, Date.now())
  }

  async editMessage(chatJid: string, messageId: string, text: string): Promise<void> {
    const socket = this.connectedSocket()
    const jid = this.resolveChat(chatJid)
    const { key } = this.rawMessage(jid, messageId)
    if (!key.fromMe) throw new Error("only messages sent by this account can be edited")
    await socket.sendMessage(jid, { text, edit: key })
    this.archive.applyEdit(jid, messageId, text)
  }

  async deleteMessage(chatJid: string, messageId: string): Promise<void> {
    const socket = this.connectedSocket()
    const jid = this.resolveChat(chatJid)
    const { key } = this.rawMessage(jid, messageId)
    await socket.sendMessage(jid, { delete: key })
    this.archive.applyRevoke(jid, messageId)
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
    const socket = this.connectedSocket()
    const group = await socket.groupMetadata(jidNormalizedUser(groupJid))
    return {
      jid: group.id,
      subject: group.subject,
      description: group.desc ?? null,
      owner: group.ownerPn ?? group.owner ?? null,
      createdAt: group.creation ?? null,
      size: group.size ?? group.participants.length,
      announceOnly: group.announce ?? false,
      restricted: group.restrict ?? false,
      participants: group.participants.map((participant) => {
        const person = this.archive.getPerson(jidNormalizedUser(participant.id))
        return {
          jid: person.jid,
          name: person.name ?? participant.notify ?? null,
          phone: person.phone,
          admin: participant.admin ?? null,
        }
      }),
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

  private async keepAlive(): Promise<void> {
    await this.ensureConnected()
    await this.ctx.storage.setAlarm(Date.now() + HEARTBEAT_MS)
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
      this.lastError = null
    }
    if (update.connection === "open") {
      this.state = "open"
      this.qrSvg = null
      this.lastError = null
      this.ctx.waitUntil(this.refreshGroups())
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

  // Reconnects are frequent and subjects rarely change, so this is throttled.
  private async refreshGroups(): Promise<void> {
    const last = (await this.ctx.storage.get<number>(GROUPS_REFRESHED_AT)) ?? 0
    if (!this.socket || Date.now() - last < GROUP_REFRESH_MS) return
    await this.ctx.storage.put(GROUPS_REFRESHED_AT, Date.now())
    await refreshGroupNames(this.socket, this.archive).catch((error: Error) => {
      console.warn("group refresh failed:", error.message)
    })
  }

  private forgetDevice(): void {
    this.socket = null
    // A new pairing starts from an empty archive, so it should not wait for the throttle.
    void this.ctx.storage.delete(GROUPS_REFRESHED_AT)
    this.store.clear()
    this.archive.clear()
    this.state = "logged_out"
  }

  private connectedSocket(): Socket {
    if (!this.socket || this.state !== "open") {
      throw new Error(`WhatsApp is not connected (${this.state})`)
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
    if (!raw) throw new Error(`message ${messageId} not found in ${chatJid}`)
    return decodeRaw(raw)
  }
}
