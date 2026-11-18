// One shape for webhooks in the REST API and in the MCP tools, so both validate and
// answer alike.
import { z } from "zod"

import { EVENT_TYPES } from "./events"
import type { Delivery, Webhook, WebhookFields, WebhookPatch } from "./store"

export const MAX_WEBHOOKS = 10
const MAX_HEADERS = 5
const HEADER_NAME = /^[A-Za-z0-9!#$%&'*+.^_`|~-]{1,64}$/
const RESERVED_HEADERS = new Set(["content-type", "content-length", "user-agent", "host"])
const LOCAL_HOSTS = new Set(["localhost", "127.0.0.1", "[::1]"])

function isWebhookUrl(value: string): boolean {
  if (!URL.canParse(value)) return false
  const url = new URL(value)
  if (url.username || url.password) return false
  return url.protocol === "https:" || (url.protocol === "http:" && LOCAL_HOSTS.has(url.hostname))
}

const url = z
  .string()
  .trim()
  .max(2048)
  .refine(isWebhookUrl, "an https:// URL without credentials (http:// only for localhost)")
  .describe("Where events are POSTed, https://")

const events = z
  .array(z.enum([...EVENT_TYPES, "*"]))
  .min(1)
  .describe(`Event types to receive, or ["*"] for all: ${EVENT_TYPES.join(", ")}`)

const chats = z
  .array(z.string().min(1))
  .min(1)
  .max(100)
  .nullable()
  .describe("Only events in these chats (JIDs or phone numbers); null for every chat")

const chatTypes = z
  .array(z.enum(["direct", "group"]))
  .min(1)
  .describe("Only events in these kinds of chat")

const headers = z
  .record(z.string(), z.string().max(1000))
  .refine((value) => Object.keys(value).length <= MAX_HEADERS, `at most ${MAX_HEADERS} headers`)
  .refine(
    (value) =>
      Object.keys(value).every((name) => {
        const lower = name.toLowerCase()
        return (
          HEADER_NAME.test(name) && !RESERVED_HEADERS.has(lower) && !lower.startsWith("webhook-")
        )
      }),
    "header names must be tokens, and cannot be content-type, user-agent, host or webhook-*",
  )
  .describe(
    "Fixed headers sent with every request, e.g. Authorization. Values are never shown back",
  )

const description = z
  .string()
  .trim()
  .max(200)
  .nullable()
  .describe("What the webhook is for, to recognize it later")

const transcribeVoice = z
  .boolean()
  .describe(
    "Transcribe a received voice or audio message before delivering it: waits up to 10 s and costs a speech-to-text call, saved for later reads",
  )

export const WebhookCreate = z.object({
  url,
  events: events.default(["*"]),
  chats: chats.default(null),
  chat_types: chatTypes.default(["direct", "group"]),
  headers: headers.default({}),
  description: description.default(null),
  transcribe_voice: transcribeVoice.default(false),
})
export type WebhookCreate = z.infer<typeof WebhookCreate>

export const WebhookUpdate = z.object({
  url: url.optional(),
  events: events.optional(),
  chats: chats.optional(),
  chat_types: chatTypes.optional(),
  headers: headers.optional().describe("Replaces all fixed headers"),
  description: description.optional(),
  transcribe_voice: transcribeVoice.optional(),
  enabled: z
    .boolean()
    .optional()
    .describe("false pauses deliveries; true resumes them and resends what is pending"),
  rotate_secret: z
    .boolean()
    .optional()
    .describe("Issue a new signing secret; the old one keeps signing too for 24 hours"),
})
export type WebhookUpdate = z.infer<typeof WebhookUpdate>

/** The fields to save; chats still need resolving to canonical JIDs. */
export function fieldsOf(input: WebhookCreate): WebhookFields {
  return {
    url: input.url,
    events: input.events,
    chats: input.chats,
    chatTypes: input.chat_types,
    headers: input.headers,
    description: input.description,
    transcribeVoice: input.transcribe_voice,
  }
}

export function patchOf(input: WebhookUpdate): WebhookPatch {
  const patch: WebhookPatch = {}
  if (input.url !== undefined) patch.url = input.url
  if (input.events !== undefined) patch.events = input.events
  if (input.chats !== undefined) patch.chats = input.chats
  if (input.chat_types !== undefined) patch.chatTypes = input.chat_types
  if (input.headers !== undefined) patch.headers = input.headers
  if (input.description !== undefined) patch.description = input.description
  if (input.transcribe_voice !== undefined) patch.transcribeVoice = input.transcribe_voice
  if (input.enabled !== undefined) patch.enabled = input.enabled
  return patch
}

type Time = (ms: number) => string

function at(time: Time, ms: number | null): string | null {
  return ms === null ? null : time(ms)
}

export function presentDelivery(delivery: Delivery, time: Time): Record<string, unknown> {
  return {
    event_id: delivery.eventId,
    type: delivery.type,
    state: delivery.state,
    attempts: delivery.attempts,
    created_at: time(delivery.createdAt),
    last_attempt_at: at(time, delivery.lastAttemptAt),
    last_status: delivery.lastStatus,
    last_error: delivery.lastError,
    next_attempt_at: at(time, delivery.nextAttemptAt),
    delivered_at: at(time, delivery.deliveredAt),
  }
}

export function webhookStatus(webhook: Webhook): "ok" | "failing" | "disabled" {
  if (!webhook.enabled) return "disabled"
  return webhook.failingSince === null ? "ok" : "failing"
}

export function presentWebhook(webhook: Webhook, time: Time): Record<string, unknown> {
  return {
    id: webhook.id,
    url: webhook.url,
    events: webhook.events,
    chats: webhook.chats,
    chat_types: webhook.chatTypes,
    header_names: webhook.headerNames,
    description: webhook.description,
    transcribe_voice: webhook.transcribeVoice,
    enabled: webhook.enabled,
    status: webhookStatus(webhook),
    disabled_reason: webhook.disabledReason,
    failing_since: at(time, webhook.failingSince),
    pending: webhook.pending,
    last_delivery: webhook.lastDelivery ? presentDelivery(webhook.lastDelivery, time) : null,
    created_via: webhook.createdVia,
    created_at: time(webhook.createdAt),
    updated_at: time(webhook.updatedAt),
  }
}
