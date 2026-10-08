/**
 * One Durable Object per WhatsApp account. It owns the linked-device session:
 * the zapo client, its Signal keys and message archive (in the object's SQLite),
 * and the outbound WebSocket to WhatsApp.
 *
 * Keeping the object resident: an open outbound WebSocket pins a Durable Object
 * for at most 15 minutes, and an idle object is evicted 70-140s after its last
 * event. A short repeating alarm is an event, so it keeps the object in memory
 * and doubles as the reconnect loop after evictions, deploys and drops.
 */
import { DurableObject } from "cloudflare:workers"
import { createSqliteStore } from "@zapo-js/store-sqlite"
import { renderSVG } from "uqr"
import { ConsoleLogger, createStore, parsePhoneJid, WaClient } from "zapo-js"
import { toError } from "zapo-js/util"

import { durableSqliteConnection } from "./store/durable-sqlite"

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
  private client: WaClient | null = null
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
    // zapo's `logout()` needs a paired session; while still pairing, only the
    // socket has to go.
    if (this.client?.getCredentials()?.meJid) await this.client.logout()
    else await this.client?.disconnect()
    this.client = null
    this.state = "logged_out"
    return this.status()
  }

  async send(to: string, text: string): Promise<{ id: string }> {
    if (!this.client || this.state !== "open") throw new Error(`not connected (${this.state})`)
    const result = await this.client.message.send(toJid(to), text)
    return { id: result.id }
  }

  status(): AccountStatus {
    return {
      state: this.state,
      me: this.client?.getCredentials()?.meJid ?? null,
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
    this.ensureConnected()
    await this.ctx.storage.setAlarm(Date.now() + HEARTBEAT_MS)
  }

  private ensureConnected(): void {
    if (this.state === "connecting" || this.state === "pairing" || this.state === "open") return
    const client = (this.client ??= this.createClient())
    this.state = "connecting"
    // `connect()` resolves only after pairing completes, so it is not awaited;
    // progress arrives through the `auth_*` and `connection` events.
    client.connect().catch((error) => {
      this.lastError = toError(error).message
      this.state = "closed"
    })
  }

  private createClient(): WaClient {
    const sqlite = createSqliteStore({ connection: durableSqliteConnection(this.ctx.storage) })
    const store = createStore({
      backends: { sqlite },
      providers: {
        auth: "sqlite",
        signal: "sqlite",
        preKey: "sqlite",
        session: "sqlite",
        identity: "sqlite",
        senderKey: "sqlite",
        appState: "sqlite",
        privacyToken: "sqlite",
        messages: "sqlite",
        threads: "sqlite",
        contacts: "sqlite",
      },
    })
    const client = new WaClient({ store, sessionId: "default" }, new ConsoleLogger("info"))

    client.on("auth_qr", ({ qr }) => {
      this.state = "pairing"
      this.qrSvg = renderSVG(qr)
    })
    client.on("auth_paired", () => {
      this.qrSvg = null
    })
    client.on("connection", (event) => {
      if (event.status === "open") {
        this.state = "open"
        this.lastError = null
        return
      }
      if (event.status === "close") {
        this.lastError = `${event.reason}${event.code === null ? "" : ` (${event.code})`}`
        if (event.isLogout) {
          // A logged-out client is single-shot: the next start pairs a fresh one.
          this.client = null
          this.state = "logged_out"
          void this.ctx.storage.put(WANTS_CONNECTION, false)
        } else {
          this.state = "closed"
        }
      }
    })
    client.on("message", () => {
      this.messagesReceived += 1
    })
    return client
  }
}

/**
 * Accepts a full JID or a phone number with country code. Phones are parsed
 * here because zapo's own recipient normalization reads a `-` as a group id,
 * which would turn "+55 11 99999-9999" into a group JID.
 */
function toJid(to: string): string {
  return to.includes("@") ? to : parsePhoneJid(to)
}
