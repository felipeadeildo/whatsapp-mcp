// What a webhook receives. Payloads are for programs: JIDs next to names, nulls kept
// so every field is always there, and times in ISO 8601 UTC.
import type { ArchivedMessage, ChatType } from "../store/archive"
import type { ReadMessage } from "../transcribe"

export const EVENT_TYPES = [
  "message.received",
  "message.sent",
  "message.edited",
  "message.deleted",
  "message.reaction",
  "account.connection",
] as const

export type SubscribableType = (typeof EVENT_TYPES)[number]
export type EventType = SubscribableType | "webhook.test"

export function isEventType(value: string): value is SubscribableType {
  return (EVENT_TYPES as readonly string[]).includes(value)
}

export interface Person {
  readonly jid: string
  readonly name: string | null
  readonly phone: string | null
}

export interface ChatRef {
  readonly jid: string
  readonly name: string | null
  readonly type: ChatType
}

export interface WebhookEvent {
  readonly id: string
  readonly type: EventType
  readonly createdAt: number
  readonly data: Record<string, unknown>
  /** The chat the event happened in, for the webhooks' chat filters. */
  readonly chat: ChatRef | null
  /** A voice or audio message a webhook may want transcribed before delivery. */
  readonly voice: { readonly chatJid: string; readonly id: string } | null
}

export function newId(prefix: string): string {
  return `${prefix}_${crypto.randomUUID().replaceAll("-", "")}`
}

export function isoTime(ms: number): string {
  return new Date(ms).toISOString()
}

const VOICE_KINDS: ReadonlySet<string> = new Set(["voice", "audio"])

export function messageData(message: ReadMessage, sender: Person | null): Record<string, unknown> {
  return {
    id: message.id,
    sent_at: isoTime(message.sentAt * 1000),
    from_me: message.fromMe,
    sender,
    kind: message.kind,
    text: message.text,
    reply_to: message.quotedId,
    mentions: message.mentions.map(({ jid, name }) => ({ jid, name })),
    forwarded: message.forwarded,
    media: message.media,
    transcript: message.transcript,
    ...(message.transcriptStatus ? { transcript_status: message.transcriptStatus } : {}),
    description: message.description,
    edited: message.edited,
    deleted: message.deleted,
    reactions: message.reactions.map(({ emoji, senderJid }) => ({
      emoji,
      sender_jid: senderJid,
    })),
  }
}

export function chatData(chat: ChatRef): Record<string, unknown> {
  return { jid: chat.jid, name: chat.name, type: chat.type }
}

export function messageEvent(
  type: "message.received" | "message.sent" | "message.edited" | "message.deleted",
  chat: ChatRef,
  message: ArchivedMessage,
  sender: Person | null,
  now: number,
): WebhookEvent {
  const isNewVoice = type === "message.received" && VOICE_KINDS.has(message.kind)
  return {
    id: newId("evt"),
    type,
    createdAt: now,
    chat,
    voice: isNewVoice && message.transcript === null ? { chatJid: chat.jid, id: message.id } : null,
    data: { chat: chatData(chat), message: messageData(message, sender) },
  }
}

export function reactionEvent(
  chat: ChatRef,
  messageId: string,
  sender: Person | null,
  emoji: string,
  now: number,
): WebhookEvent {
  return {
    id: newId("evt"),
    type: "message.reaction",
    createdAt: now,
    chat,
    voice: null,
    data: { chat: chatData(chat), message_id: messageId, from_me: sender === null, sender, emoji },
  }
}

export type ConnectionChange = "linked" | "unlinked" | "refused"

export function connectionEvent(
  state: ConnectionChange,
  details: { me: string | null; owner: string | null; error: string | null },
  now: number,
): WebhookEvent {
  return {
    id: newId("evt"),
    type: "account.connection",
    createdAt: now,
    chat: null,
    voice: null,
    data: { state, ...details },
  }
}

export function testEvent(now: number): WebhookEvent {
  return {
    id: newId("evt"),
    type: "webhook.test",
    createdAt: now,
    chat: null,
    voice: null,
    data: { message: "This is a test from whatsapp-mcp." },
  }
}

/** The body a webhook receives. */
export function serialize(event: WebhookEvent, account: string): string {
  return JSON.stringify({
    id: event.id,
    type: event.type,
    account,
    created_at: isoTime(event.createdAt),
    data: event.data,
  })
}
