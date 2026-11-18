import { describe, expect, test } from "bun:test"

import { DISABLE_AFTER_MS, drain, type Fetch, RETRY_DELAYS_MS } from "../src/webhooks/deliver"
import {
  type ChatRef,
  connectionEvent,
  messageEvent,
  reactionEvent,
  type WebhookEvent,
} from "../src/webhooks/events"
import { WebhookCreate, WebhookUpdate } from "../src/webhooks/schema"
import { signatureHeader } from "../src/webhooks/sign"
import { MAX_PENDING, type WebhookFields, WebhookStore } from "../src/webhooks/store"
import { archivedMessage } from "./archived-message"
import { memoryDatabase } from "./memory-database"

const DIRECT: ChatRef = { jid: "1@lid", name: "Marina", type: "direct" }
const GROUP: ChatRef = { jid: "120363000000000001@g.us", name: "Família", type: "group" }
const T0 = 1_791_000_000_000

function phoneToLid(jid: string): string {
  return jid.endsWith("@s.whatsapp.net") ? DIRECT.jid : jid
}

const FIELDS: WebhookFields = {
  url: "https://hooks.example.com/whatsapp",
  events: ["*"],
  chats: null,
  chatTypes: ["direct", "group"],
  headers: {},
  description: null,
  transcribeVoice: false,
}

function storeWith(...hooks: Partial<WebhookFields>[]) {
  const store = new WebhookStore(memoryDatabase())
  const ids = hooks.map((fields, index) => {
    const id = `wh_${index}`
    store.create(
      id,
      { ...FIELDS, ...fields, account: "personal", createdVia: "api" },
      "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
      T0,
    )
    return id
  })
  return { store, ids }
}

function received(chat: ChatRef = DIRECT, kind: "text" | "voice" = "text"): WebhookEvent {
  const message = archivedMessage("A", {
    chatJid: chat.jid,
    kind,
    text: kind === "text" ? "oi" : null,
  })
  return messageEvent("message.received", chat, message, null, T0)
}

function bodyOf(init: RequestInit): string {
  return typeof init.body === "string" ? init.body : ""
}

interface Sent {
  readonly url: string
  readonly headers: Record<string, string>
  readonly body: string
}

function endpoint(statuses: readonly (number | Error)[] = [200]) {
  const sent: Sent[] = []
  let call = 0
  const fetch: Fetch = async (url, init) => {
    const status = statuses[Math.min(call, statuses.length - 1)] ?? 200
    call += 1
    sent.push({
      url,
      headers: Object.fromEntries(new Headers(init.headers).entries()),
      body: bodyOf(init),
    })
    if (status instanceof Error) throw status
    return new Response(status === 200 ? "ok" : "nope", { status })
  }
  return { fetch, sent }
}

function clock(start = T0) {
  let now = start
  return { now: () => now, advance: (ms: number) => (now += ms), set: (ms: number) => (now = ms) }
}

describe("webhook signatures", () => {
  test("match the Standard Webhooks test vector", async () => {
    const header = await signatureHeader(
      ["whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"],
      "msg_p5jXN8AQM9LWM0D4loKWxJek",
      1614265330,
      '{"test": 2432232314}',
    )
    expect(header).toBe("v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE=")
  })

  test("carry one signature per secret while a rotated one is still valid", async () => {
    const header = await signatureHeader(["whsec_AAAA", "whsec_BBBB"], "evt_1", 1, "{}")
    expect(header.split(" ")).toHaveLength(2)
  })
})

describe("webhook queue", () => {
  test("an event becomes one delivery per webhook that wants it", () => {
    const { store } = storeWith(
      {},
      { events: ["message.sent"] },
      { chatTypes: ["group"] },
      { chats: ["2@lid"] },
      { chats: ["1@lid"] },
    )
    expect(store.enqueue(received(), T0)).toBe(2)
    expect(store.due(T0, 10).map((due) => due.webhookId)).toEqual(["wh_0", "wh_4"])
  })

  test("a chat saved by phone still matches once its LID is known", () => {
    const { store } = storeWith({ chats: ["5511999999999@s.whatsapp.net"] })
    expect(store.enqueue(received(), T0, phoneToLid)).toBe(1)
  })

  test("account events ignore the chat filters", () => {
    const { store } = storeWith({ chats: ["1@lid"], chatTypes: ["group"] })
    const event = connectionEvent("unlinked", { me: null, owner: null, error: null }, T0)
    expect(store.enqueue(event, T0)).toBe(1)
  })

  test("disabled webhooks receive nothing, and get what was pending when enabled again", () => {
    const { store, ids } = storeWith({})
    const [id = ""] = ids
    store.enqueue(received(), T0)
    store.update(id, { enabled: false }, T0)
    expect(store.enqueue(received(), T0)).toBe(0)
    expect(store.due(T0, 10)).toHaveLength(0)
    expect(store.nextDueAt()).toBeNull()
    store.update(id, { enabled: true }, T0 + 1)
    expect(store.due(T0 + 1, 10)).toHaveLength(1)
  })

  test("the payload names the account and keeps every field", () => {
    const { store } = storeWith({})
    store.enqueue(received(GROUP), T0)
    const [due] = store.due(T0, 1)
    const body = JSON.parse(due?.payload ?? "{}")
    expect(body).toMatchObject({
      type: "message.received",
      account: "personal",
      created_at: new Date(T0).toISOString(),
      data: {
        chat: GROUP,
        message: { id: "A", from_me: false, text: "oi", transcript: null, reply_to: null },
      },
    })
  })

  test("a reaction carries the emoji and who reacted", () => {
    const { store } = storeWith({})
    const sender = { jid: "1@lid", name: "Marina", phone: "+5511999999999" }
    store.enqueue(reactionEvent(DIRECT, "A", sender, "👍", T0), T0)
    const body = JSON.parse(store.due(T0, 1)[0]?.payload ?? "{}")
    expect(body.data).toEqual({
      chat: DIRECT,
      message_id: "A",
      from_me: false,
      sender,
      emoji: "👍",
    })
  })

  test("a dead endpoint keeps only the newest pending deliveries", () => {
    const { store } = storeWith({})
    for (let index = 0; index <= MAX_PENDING; index += 1) store.enqueue(received(), T0 + index)
    store.prune(T0 + MAX_PENDING)
    expect(store.get("wh_0")?.pending).toBe(MAX_PENDING)
    expect(store.recent("wh_0", MAX_PENDING + 1).at(-1)?.state).toBe("dropped")
  })

  test("deleting a webhook deletes its queue", () => {
    const { store } = storeWith({})
    store.enqueue(received(), T0)
    expect(store.delete("wh_0")).toBe(true)
    expect(store.due(T0, 10)).toHaveLength(0)
    expect(store.list()).toHaveLength(0)
  })

  test("header values never come back, only their names", () => {
    const { store } = storeWith({ headers: { Authorization: "Bearer s3cret" } })
    const webhook = store.get("wh_0")
    expect(webhook?.headerNames).toEqual(["Authorization"])
    expect(JSON.stringify(webhook)).not.toContain("s3cret")
  })
})

describe("webhook delivery", () => {
  test("posts a signed event and marks it delivered", async () => {
    const { store } = storeWith({ headers: { Authorization: "Bearer token" } })
    const event = received()
    store.enqueue(event, T0)
    const { fetch, sent } = endpoint([200])
    const time = clock()
    await drain({ store, fetch, now: time.now })

    const [request] = sent
    expect(request?.url).toBe(FIELDS.url)
    expect(request?.headers["webhook-id"]).toBe(event.id)
    expect(request?.headers.authorization).toBe("Bearer token")
    const timestamp = Number(request?.headers["webhook-timestamp"])
    expect(request?.headers["webhook-signature"]).toBe(
      await signatureHeader(
        ["whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"],
        event.id,
        timestamp,
        request?.body ?? "",
      ),
    )
    expect(store.recent("wh_0")[0]?.state).toBe("delivered")
    expect(store.get("wh_0")?.failingSince).toBeNull()
    expect(store.nextDueAt()).toBeNull()
  })

  test("failures retry on the schedule and give up after the last attempt", async () => {
    const { store } = storeWith({})
    store.enqueue(received(), T0)
    const { fetch, sent } = endpoint([500])
    const time = clock()
    await drain({ store, fetch, now: time.now })
    for (const delay of RETRY_DELAYS_MS) {
      expect(store.nextDueAt()).toBe(time.now() + delay)
      time.advance(delay)
      await drain({ store, fetch, now: time.now })
    }
    expect(sent).toHaveLength(RETRY_DELAYS_MS.length + 1)
    const [delivery] = store.recent("wh_0")
    expect(delivery).toMatchObject({ state: "failed", lastStatus: 500, attempts: 8 })
    expect(delivery?.lastError).toContain("nope")
  })

  test("a network error is a failure like any other", async () => {
    const { store } = storeWith({})
    store.enqueue(received(), T0)
    await drain({ store, fetch: endpoint([new Error("dns")]).fetch, now: clock().now })
    expect(store.recent("wh_0")[0]).toMatchObject({ state: "pending", lastError: "dns" })
    expect(store.get("wh_0")?.failingSince).toBe(T0)
  })

  test("410 Gone turns the webhook off at once", async () => {
    const { store } = storeWith({})
    store.enqueue(received(), T0)
    await drain({ store, fetch: endpoint([410]).fetch, now: clock().now })
    expect(store.get("wh_0")).toMatchObject({ enabled: false, disabledReason: "gone" })
  })

  test("a webhook that only failed for three days is turned off", async () => {
    const { store } = storeWith({})
    const time = clock()
    const { fetch } = endpoint([500])
    store.enqueue(received(), time.now())
    await drain({ store, fetch, now: time.now })
    time.advance(DISABLE_AFTER_MS)
    store.enqueue(received(), time.now())
    await drain({ store, fetch, now: time.now })
    expect(store.get("wh_0")).toMatchObject({ enabled: false, disabledReason: "failing" })
  })

  test("a success ends the failure streak", async () => {
    const { store } = storeWith({})
    const time = clock()
    store.enqueue(received(), time.now())
    await drain({ store, fetch: endpoint([500]).fetch, now: time.now })
    time.advance(RETRY_DELAYS_MS[0])
    await drain({ store, fetch: endpoint([200]).fetch, now: time.now })
    expect(store.get("wh_0")?.failingSince).toBeNull()
  })

  test("waits for the transcript when the webhook asks, and retries send the same body", async () => {
    const plainUrl = "https://plain.example.com/hook"
    const { store } = storeWith({ transcribeVoice: true }, { url: plainUrl })
    store.enqueue(received(DIRECT, "voice"), T0)
    const asked: string[] = []
    const transcribe = async (voice: { chatJid: string; id: string }) => {
      asked.push(voice.id)
      return { id: voice.id, kind: "voice", transcript: "chego às 19h" }
    }
    const sent: Sent[] = []
    let transcribedCalls = 0
    const fetch: Fetch = async (url, init) => {
      sent.push({ url, headers: {}, body: bodyOf(init) })
      if (url === plainUrl) return new Response("ok")
      transcribedCalls += 1
      return new Response(null, { status: transcribedCalls === 1 ? 500 : 200 })
    }
    const time = clock()
    await drain({ store, fetch, now: time.now, transcribe })
    time.advance(RETRY_DELAYS_MS[0])
    await drain({ store, fetch, now: time.now, transcribe })

    expect(asked).toEqual(["A"])
    const transcribed = sent.filter((request) => request.body.includes("chego às 19h"))
    expect(transcribed).toHaveLength(2)
    const plain = sent.find((request) => !request.body.includes("chego às 19h"))
    expect(JSON.parse(plain?.body ?? "{}").data.message.transcript).toBeNull()
  })

  test("old deliveries are pruned, keeping the latest of each webhook", async () => {
    const { store } = storeWith({})
    const time = clock()
    for (let index = 0; index < 60; index += 1) store.enqueue(received(), time.now())
    await drain({ store, fetch: endpoint([200]).fetch, now: time.now })
    time.advance(2 * 24 * 60 * 60 * 1000)
    store.prune(time.now())
    expect(store.recent("wh_0", 100)).toHaveLength(50)
  })
})

describe("webhook input", () => {
  test("defaults to every event in every direct chat and group", () => {
    const input = WebhookCreate.parse({ url: "https://hooks.example.com" })
    expect(input).toMatchObject({
      events: ["*"],
      chats: null,
      chat_types: ["direct", "group"],
      transcribe_voice: false,
    })
  })

  test("refuses plain http outside localhost, and credentials in the URL", () => {
    expect(WebhookCreate.safeParse({ url: "http://hooks.example.com" }).success).toBe(false)
    expect(WebhookCreate.safeParse({ url: "https://a:b@hooks.example.com" }).success).toBe(false)
    expect(WebhookCreate.safeParse({ url: "http://localhost:3000/hook" }).success).toBe(true)
  })

  test("refuses headers that would override the signature or the body type", () => {
    for (const name of ["webhook-signature", "Content-Type", "bad name"]) {
      const parsed = WebhookCreate.safeParse({
        url: "https://h.example.com",
        headers: { [name]: "x" },
      })
      expect(parsed.success).toBe(false)
    }
  })

  test("refuses unknown event types", () => {
    expect(WebhookUpdate.safeParse({ events: ["message.typing"] }).success).toBe(false)
  })
})
