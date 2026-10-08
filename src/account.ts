/**
 * One Durable Object per WhatsApp account. It owns the linked-device session:
 * the baileyrs socket (whatsapp-rust compiled to WASM), the engine's state in
 * the object's SQLite, and the outbound WebSocket to WhatsApp.
 *
 * Keeping the object resident: an open outbound WebSocket pins a Durable Object
 * for at most 15 minutes, and an idle object is evicted 70-140s after its last
 * event. A short repeating alarm is an event, so it keeps the object in memory
 * and rebuilds the socket after evictions, deploys and terminal closes.
 * Transient drops are retried by the Rust engine itself and never reach us.
 */
import makeWASocket, {
  createAuthenticationState,
  DisconnectReason,
  type HostConnectionUpdate,
  type HostWASocket,
} from "@oxidezap/baileyrs/host"
import { isBoom } from "@oxidezap/baileyrs/lib/Utils/boom.js"
import { DurableObject } from "cloudflare:workers"
import { renderSVG } from "uqr"

import { durableKvStore } from "./store/durable-kv"
import { fetchWaWebVersion } from "./wa-version"
import "./wasm"

const HEARTBEAT_MS = 30_000
const WANTS_CONNECTION = "wants_connection"

type ConnectionState = "idle" | "connecting" | "pairing" | "open" | "closed" | "logged_out"

export interface AccountStatus {
  readonly state: ConnectionState
  readonly me: string | null
  readonly qrSvg: string | null
  readonly lastError: string | null
  readonly startedAt: number
  readonly messagesReceived: number
}

export class WhatsAppAccount extends DurableObject<Env> {
  private readonly store = durableKvStore(this.ctx.storage)
  private socket: HostWASocket | null = null
  private opening: Promise<void> | null = null
  private state: ConnectionState = "idle"
  private qrSvg: string | null = null
  private lastError: string | null = null
  private messagesReceived = 0
  private readonly startedAt = Date.now()

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
    else await this.socket?.end()
    this.forgetDevice()
    return this.status()
  }

  async send(to: string, text: string): Promise<{ id: string | null }> {
    if (!this.socket || this.state !== "open") throw new Error(`not connected (${this.state})`)
    const message = await this.socket.sendMessage(toJid(to), { text })
    return { id: message.key.id ?? null }
  }

  status(): AccountStatus {
    return {
      state: this.state,
      me: this.socket?.user?.id ?? null,
      qrSvg: this.state === "pairing" ? this.qrSvg : null,
      lastError: this.lastError,
      startedAt: this.startedAt,
      messagesReceived: this.messagesReceived,
    }
  }

  async alarm(): Promise<void> {
    if (await this.ctx.storage.get<boolean>(WANTS_CONNECTION)) await this.keepAlive()
  }

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
    const socket = makeWASocket({ auth, version })
    socket.ev.on("connection.update", (update) => this.onConnectionUpdate(update))
    socket.ev.on("messages.upsert", ({ messages }) => {
      this.messagesReceived += messages.length
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
    this.state = "logged_out"
  }
}

/** Accepts a full JID or a phone number with country code. */
function toJid(to: string): string {
  if (to.includes("@")) return to
  const digits = to.replace(/\D/g, "")
  if (!digits) throw new Error(`invalid recipient: ${to}`)
  return `${digits}@s.whatsapp.net`
}
