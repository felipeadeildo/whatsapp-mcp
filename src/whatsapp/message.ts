/**
 * Turns WhatsApp's protobuf messages into the flat records the archive keeps.
 *
 * A raw message can be wrapped (ephemeral, view-once, document-with-caption)
 * and can also be an instruction about another message (edit, revoke,
 * reaction) rather than content of its own. This module resolves both so the
 * archive only deals with "a message with this text" or "change that message".
 */
import { getContentType, normalizeMessageContent } from "@oxidezap/baileyrs/lib/Media/content.js"
import type {
  WAMessage,
  WAMessageKey,
  WAMessageUpdate,
} from "@oxidezap/baileyrs/lib/Types/Message.js"
import type { proto } from "@oxidezap/baileyrs/lib/WAProto/runtime.js"

export type MessageKind =
  | "text"
  | "image"
  | "video"
  | "audio"
  | "voice"
  | "document"
  | "sticker"
  | "location"
  | "contact"
  | "poll"
  | "other"

export interface MediaInfo {
  readonly mimetype: string | null
  readonly fileName: string | null
  readonly bytes: number | null
  readonly seconds: number | null
  readonly width: number | null
  readonly height: number | null
}

export interface StoredMessage {
  readonly chatJid: string
  readonly id: string
  readonly fromMe: boolean
  /** Who wrote it: the participant in groups, the chat in DMs, `null` when it is us. */
  readonly senderJid: string | null
  readonly senderName: string | null
  /** Unix seconds. */
  readonly sentAt: number
  readonly kind: MessageKind
  /** Body, caption, or a one-line rendering for non-text kinds (location, poll...). */
  readonly text: string | null
  readonly quotedId: string | null
  readonly media: MediaInfo | null
}

/** What a raw message means for the archive. */
export type MessageEvent =
  | { readonly type: "message"; readonly message: StoredMessage }
  | { readonly type: "edit"; readonly chatJid: string; readonly id: string; readonly text: string }
  | { readonly type: "revoke"; readonly chatJid: string; readonly id: string }
  | {
      readonly type: "reaction"
      readonly chatJid: string
      readonly id: string
      readonly senderJid: string | null
      /** Empty string removes the reaction. */
      readonly emoji: string
      readonly at: number
    }
  | { readonly type: "ignored" }

// The enum object lives in the protobuf runtime, which the host build does not
// load, so the wire values are spelled out against the enum's type.
type ProtocolType = proto.Message.ProtocolMessage.Type
const PROTOCOL_REVOKE: ProtocolType = 0
const PROTOCOL_MESSAGE_EDIT: ProtocolType = 14

type ProtoNumber = number | { toNumber(): number } | null | undefined

function toNumber(value: ProtoNumber): number | null {
  if (value === null || value === undefined) return null
  return typeof value === "number" ? value : value.toNumber()
}

function senderOf(key: WAMessageKey, chatJid: string): string | null {
  if (key.fromMe) return null
  return key.participant ?? chatJid
}

function media(content: {
  mimetype?: string | null
  fileName?: string | null
  fileLength?: ProtoNumber
  seconds?: number | null
  width?: number | null
  height?: number | null
}): MediaInfo {
  return {
    mimetype: content.mimetype ?? null,
    fileName: content.fileName ?? null,
    bytes: toNumber(content.fileLength),
    seconds: content.seconds ?? null,
    width: content.width ?? null,
    height: content.height ?? null,
  }
}

interface Body {
  readonly kind: MessageKind
  readonly text: string | null
  readonly media: MediaInfo | null
  readonly context: proto.IContextInfo | null | undefined
}

function bodyOf(content: proto.IMessage): Body | null {
  if (content.conversation) {
    return { kind: "text", text: content.conversation, media: null, context: null }
  }
  const extended = content.extendedTextMessage
  if (extended) {
    return { kind: "text", text: extended.text ?? null, media: null, context: extended.contextInfo }
  }
  const image = content.imageMessage
  if (image) {
    return {
      kind: "image",
      text: image.caption ?? null,
      media: media(image),
      context: image.contextInfo,
    }
  }
  const video = content.videoMessage
  if (video) {
    return {
      kind: "video",
      text: video.caption ?? null,
      media: media(video),
      context: video.contextInfo,
    }
  }
  const audio = content.audioMessage
  if (audio) {
    const kind = audio.ptt ? "voice" : "audio"
    return { kind, text: null, media: media(audio), context: audio.contextInfo }
  }
  const document = content.documentMessage
  if (document) {
    const text = document.caption ?? document.title ?? document.fileName ?? null
    return { kind: "document", text, media: media(document), context: document.contextInfo }
  }
  const sticker = content.stickerMessage
  if (sticker) {
    return { kind: "sticker", text: null, media: media(sticker), context: sticker.contextInfo }
  }
  const location = content.locationMessage ?? content.liveLocationMessage
  if (location) {
    const place = "name" in location ? [location.name, location.address] : []
    const text = [...place, `${location.degreesLatitude},${location.degreesLongitude}`]
      .filter(Boolean)
      .join(" · ")
    return { kind: "location", text, media: null, context: location.contextInfo }
  }
  const contact = content.contactMessage
  if (contact) {
    return {
      kind: "contact",
      text: contact.displayName ?? null,
      media: null,
      context: contact.contextInfo,
    }
  }
  const contacts = content.contactsArrayMessage
  if (contacts) {
    const names = (contacts.contacts ?? []).map((card) => card.displayName).filter(Boolean)
    return { kind: "contact", text: names.join(", "), media: null, context: contacts.contextInfo }
  }
  const poll =
    content.pollCreationMessage ?? content.pollCreationMessageV2 ?? content.pollCreationMessageV3
  if (poll) {
    const options = (poll.options ?? []).map((option) => option.optionName).filter(Boolean)
    const text = `${poll.name ?? "poll"}: ${options.join(" / ")}`
    return { kind: "poll", text, media: null, context: poll.contextInfo }
  }
  return null
}

// proto.WebMessageInfo.StubType.REVOKE, spelled out for the same reason.
const STUB_REVOKE: proto.WebMessageInfo.StubType = 1

/**
 * Classifies an in-place update to a known message: an edit carries the new
 * content, a revoke turns the message into a stub. Anything else (status,
 * receipts, poll tallies) is not archived.
 */
export function interpretUpdate(update: WAMessageUpdate, chatJid: string): MessageEvent {
  const id = update.key.id
  if (!id) return { type: "ignored" }
  if (update.update.messageStubType === STUB_REVOKE) return { type: "revoke", chatJid, id }
  const content = normalizeMessageContent(update.update.message)
  const edited = content?.editedMessage?.message ?? content?.protocolMessage?.editedMessage
  const text = edited ? bodyOf(normalizeMessageContent(edited) ?? {})?.text : null
  return text ? { type: "edit", chatJid, id, text } : { type: "ignored" }
}

/** Classifies one raw message. `chatJid` is passed in normalized form by the caller. */
export function interpret(raw: WAMessage, chatJid: string): MessageEvent {
  const { key } = raw
  const content = normalizeMessageContent(raw.message)
  if (!key.id || !content) return { type: "ignored" }
  const sentAt = toNumber(raw.messageTimestamp) ?? Math.floor(Date.now() / 1000)

  const protocol = content.protocolMessage
  if (protocol) {
    const targetId = protocol.key?.id
    if (!targetId) return { type: "ignored" }
    if (protocol.type === PROTOCOL_REVOKE) return { type: "revoke", chatJid, id: targetId }
    if (protocol.type === PROTOCOL_MESSAGE_EDIT && protocol.editedMessage) {
      const edited = bodyOf(normalizeMessageContent(protocol.editedMessage) ?? {})
      if (edited?.text) return { type: "edit", chatJid, id: targetId, text: edited.text }
    }
    return { type: "ignored" }
  }

  const reaction = content.reactionMessage
  if (reaction) {
    const targetId = reaction.key?.id
    if (!targetId) return { type: "ignored" }
    return {
      type: "reaction",
      chatJid,
      id: targetId,
      senderJid: senderOf(key, chatJid),
      emoji: reaction.text ?? "",
      at: toNumber(reaction.senderTimestampMs) ?? sentAt * 1000,
    }
  }

  const body = bodyOf(content)
  const kind = body?.kind ?? "other"
  return {
    type: "message",
    message: {
      chatJid,
      id: key.id,
      fromMe: key.fromMe ?? false,
      senderJid: senderOf(key, chatJid),
      senderName: raw.pushName ?? null,
      sentAt,
      kind,
      text: body?.text ?? (kind === "other" ? (getContentType(content) ?? null) : null),
      quotedId: body?.context?.stanzaId ?? null,
      media: body?.media ?? null,
    },
  }
}
