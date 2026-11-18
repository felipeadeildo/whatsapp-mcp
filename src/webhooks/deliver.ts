// Sends what is due, at least once: `webhook-id` stays the same across attempts so
// receivers can drop repeats. Order is not kept, so one bad event cannot hold the rest.
import { z } from "zod"

import { errorText } from "../errors"
import { signatureHeader } from "./sign"
import type { DueDelivery, Target, WebhookStore } from "./store"

// After the first attempt, as Svix does: 8 attempts over about 28 hours.
export const RETRY_DELAYS_MS = [
  5_000,
  5 * 60_000,
  30 * 60_000,
  2 * 60 * 60_000,
  5 * 60 * 60_000,
  10 * 60 * 60_000,
  10 * 60 * 60_000,
] as const
/** A webhook that has only failed for this long is turned off. */
export const DISABLE_AFTER_MS = 3 * 24 * 60 * 60 * 1000
const TIMEOUT_MS = 10_000
// Workers keep at most six connections open at once.
const PARALLEL = 6
const BATCH = 50
const ROUNDS = 4
const ERROR_BYTES = 1024
const USER_AGENT = "whatsapp-mcp-webhooks/1"

export interface Attempt {
  readonly ok: boolean
  readonly status: number | null
  readonly error: string | null
  readonly ms: number
}

export type Fetch = (url: string, init: RequestInit) => Promise<Response>

export interface DeliverDeps {
  readonly store: WebhookStore
  readonly fetch: Fetch
  now(): number
  /** The message's data with its transcript, for webhooks that wait for one. */
  transcribe?(voice: { chatJid: string; id: string }): Promise<Record<string, unknown> | null>
}

async function excerpt(response: Response): Promise<string> {
  const text = await response.text().catch(() => "")
  const body = text.slice(0, ERROR_BYTES)
  return body ? `HTTP ${response.status}: ${body}` : `HTTP ${response.status}`
}

/** One signed POST. Redirects are not followed, so the signature stays with its host. */
export async function post(
  target: Target,
  eventId: string,
  body: string,
  deps: Pick<DeliverDeps, "fetch" | "now">,
): Promise<Attempt> {
  const started = deps.now()
  const timestamp = Math.floor(started / 1000)
  try {
    const response = await deps.fetch(target.url, {
      method: "POST",
      redirect: "manual",
      signal: AbortSignal.timeout(TIMEOUT_MS),
      headers: {
        ...target.headers,
        "content-type": "application/json",
        "user-agent": USER_AGENT,
        "webhook-id": eventId,
        "webhook-timestamp": String(timestamp),
        "webhook-signature": await signatureHeader(target.secrets, eventId, timestamp, body),
      },
      body,
    })
    const ms = deps.now() - started
    if (response.status >= 200 && response.status < 300) {
      await response.body?.cancel()
      return { ok: true, status: response.status, error: null, ms }
    }
    return { ok: false, status: response.status, error: await excerpt(response), ms }
  } catch (error) {
    const timedOut = error instanceof DOMException && error.name === "TimeoutError"
    const message = timedOut ? `no response in ${TIMEOUT_MS / 1000} s` : errorText(error)
    return { ok: false, status: null, error: message, ms: deps.now() - started }
  }
}

const Payload = z.looseObject({ data: z.record(z.string(), z.unknown()) })

async function withTranscript(due: DueDelivery, deps: DeliverDeps): Promise<string> {
  if (!due.transcribe || !deps.transcribe) return due.payload
  let payload = due.payload
  try {
    const message = await deps.transcribe(due.transcribe)
    if (message) {
      const event = Payload.parse(JSON.parse(due.payload))
      payload = JSON.stringify({ ...event, data: { ...event.data, message } })
    }
  } catch (error) {
    console.warn("webhook transcript failed:", errorText(error))
  }
  // A retry sends exactly what the first attempt sent.
  deps.store.setPayload(due.webhookId, due.eventId, payload)
  return payload
}

async function deliverOne(due: DueDelivery, deps: DeliverDeps): Promise<void> {
  const payload = await withTranscript(due, deps)
  const attempt = await post(due, due.eventId, payload, deps)
  const now = deps.now()
  const { store } = deps
  if (attempt.ok) {
    store.delivered(due.webhookId, due.eventId, attempt.status ?? 200, now)
    return
  }
  const outcome = { status: attempt.status, error: attempt.error ?? "failed" }
  if (attempt.status === 410) {
    store.failed(due.webhookId, due.eventId, outcome, null, now)
    store.disable(due.webhookId, "gone", now)
    return
  }
  const delay = RETRY_DELAYS_MS[due.attempts]
  const failingSince = store.failed(
    due.webhookId,
    due.eventId,
    outcome,
    delay === undefined ? null : now + delay,
    now,
  )
  if (now - failingSince >= DISABLE_AFTER_MS) store.disable(due.webhookId, "failing", now)
}

async function inParallel<T>(items: readonly T[], limit: number, work: (item: T) => Promise<void>) {
  let next = 0
  const lane = async () => {
    while (next < items.length) {
      const item = items[next]
      next += 1
      if (item !== undefined) await work(item)
    }
  }
  await Promise.all(Array.from({ length: Math.min(limit, items.length) }, lane))
}

/** Sends what is due now, a few batches at most. Returns how many attempts it made. */
export async function drain(deps: DeliverDeps): Promise<number> {
  let attempts = 0
  for (let round = 0; round < ROUNDS; round += 1) {
    const due = deps.store.due(deps.now(), BATCH)
    if (due.length === 0) break
    await inParallel(due, PARALLEL, (item) => deliverOne(item, deps))
    attempts += due.length
  }
  return attempts
}
