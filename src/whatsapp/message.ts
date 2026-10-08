/**
 * Turns WhatsApp's protobuf messages into the flat records the archive keeps.
 *
 * A raw message can be wrapped (ephemeral, view-once, document-with-caption)
 * and can also be an instruction about another message (edit, revoke,
 * reaction) rather than content of its own. This module resolves both so the
 * archive only deals with "a message with this text" or "change that message".
 *
 * The set of content types and how each becomes text follows mautrix-whatsapp's
 * converter (pkg/msgconv), the most complete WhatsApp-to-text mapping around:
 * business messages (templates, buttons, lists, interactive) keep their body,
 * header, footer and button labels so a reader sees what the person saw.
 */
import { normalizeMessageContent } from "@oxidezap/baileyrs/lib/Media/content.js"
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
  | "video_note"
  | "audio"
  | "voice"
  | "document"
  | "sticker"
  | "album"
  | "location"
  | "live_location"
  | "contact"
  | "poll"
  | "event"
  | "group_invite"
  | "pin"
  | "unavailable"
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
  /** The chat and sender exactly as the message key carried them. */
  readonly remoteJid: string
  readonly participant: string | null
  readonly id: string
  readonly fromMe: boolean
  readonly pushName: string | null
  /** Unix seconds. */
  readonly sentAt: number
  readonly kind: MessageKind
  /** Body, caption, or a readable rendering for non-text kinds. */
  readonly text: string | null
  readonly quotedId: string | null
  /** JIDs @mentioned in the text, as the text spells them (`@<user part>`). */
  readonly mentions: readonly string[]
  readonly forwarded: boolean
  readonly media: MediaInfo | null
}

/** What a raw message means for the archive. */
export type MessageEvent =
  | { readonly type: "message"; readonly message: StoredMessage }
  | {
      readonly type: "edit"
      readonly remoteJid: string
      readonly id: string
      readonly text: string
    }
  | { readonly type: "revoke"; readonly remoteJid: string; readonly id: string }
  | {
      readonly type: "reaction"
      readonly remoteJid: string
      readonly id: string
      /** `null` when the reaction is ours. */
      readonly senderJid: string | null
      /** Empty string removes the reaction. */
      readonly emoji: string
      /** Unix milliseconds. */
      readonly at: number
    }
  | { readonly type: "ignored" }

// Enum objects live in the protobuf runtime, which this module does not load,
// so the wire values are spelled out against the enums' types.
type ProtocolType = proto.Message.ProtocolMessage.Type
const PROTOCOL_REVOKE: ProtocolType = 0
const PROTOCOL_MESSAGE_EDIT: ProtocolType = 14
const STUB_REVOKE: proto.WebMessageInfo.StubType = 1

type ProtoNumber = number | { toNumber(): number } | null | undefined

export function toNumber(value: ProtoNumber): number | null {
  if (value === null || value === undefined) return null
  return typeof value === "number" ? value : value.toNumber()
}

/** Joins the non-empty parts with a blank line, like a message bubble stacks them. */
function stack(...parts: readonly (string | null | undefined)[]): string | null {
  const present = parts.map((part) => part?.trim()).filter((part) => part)
  return present.length > 0 ? present.join("\n\n") : null
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

function body(
  kind: MessageKind,
  text: string | null | undefined,
  context: proto.IContextInfo | null | undefined,
): Body {
  return { kind, text: text?.trim() || null, media: null, context }
}

function withMedia(
  kind: MessageKind,
  text: string | null | undefined,
  content: Parameters<typeof media>[0] & {
    contextInfo?: proto.IContextInfo | null
  },
): Body {
  return { kind, text: text?.trim() || null, media: media(content), context: content.contextInfo }
}

function buttonLabels(labels: readonly (string | null | undefined)[]): string | null {
  const present = labels.filter((label) => label)
  return present.length > 0 ? present.map((label) => `[${label}]`).join(" ") : null
}

function templateBody(template: proto.Message.ITemplateMessage): Body | null {
  const hydrated = template.hydratedTemplate ?? template.hydratedFourRowTemplate
  if (!hydrated) {
    const interactive = template.interactiveMessageTemplate
    return interactive ? interactiveBody(interactive) : null
  }
  const buttons = (hydrated.hydratedButtons ?? []).map(
    (button) =>
      button.quickReplyButton?.displayText ??
      (button.urlButton ? `${button.urlButton.displayText} (${button.urlButton.url})` : null) ??
      (button.callButton
        ? `${button.callButton.displayText} (${button.callButton.phoneNumber})`
        : null),
  )
  const text = stack(
    hydrated.hydratedTitleText,
    hydrated.hydratedContentText,
    hydrated.hydratedFooterText,
    buttonLabels(buttons),
  )
  const header = hydrated.imageMessage ?? hydrated.videoMessage ?? hydrated.documentMessage
  if (header) {
    const kind = hydrated.imageMessage ? "image" : hydrated.videoMessage ? "video" : "document"
    return withMedia(kind, text, { ...header, contextInfo: template.contextInfo })
  }
  return body("text", text, template.contextInfo)
}

function interactiveBody(message: proto.Message.IInteractiveMessage): Body {
  const header = message.header
  const flowButtons = (message.nativeFlowMessage?.buttons ?? []).map((button) => button.name)
  const text = stack(
    stack(header?.title, header?.subtitle),
    message.body?.text,
    message.footer?.text,
    buttonLabels(flowButtons),
  )
  const headerMedia = header?.imageMessage ?? header?.videoMessage ?? header?.documentMessage
  if (headerMedia) {
    const kind = header?.imageMessage ? "image" : header?.videoMessage ? "video" : "document"
    return withMedia(kind, text, { ...headerMedia, contextInfo: message.contextInfo })
  }
  return body("text", text, message.contextInfo)
}

function listBody(list: proto.Message.IListMessage): Body {
  const rows = (list.sections ?? []).flatMap((section) =>
    (section.rows ?? []).map(
      (row) => `- ${stack(row.title, row.description)?.replace("\n\n", ": ") ?? ""}`,
    ),
  )
  const options = rows.length > 0 ? `${list.buttonText ?? "Options"}:\n${rows.join("\n")}` : null
  return body(
    "text",
    stack(list.title, list.description, options, list.footerText),
    list.contextInfo,
  )
}

function pollBody(poll: proto.Message.IPollCreationMessage): Body {
  const options = (poll.options ?? []).map((option) => `- ${option.optionName ?? ""}`)
  const text = stack(poll.name, options.join("\n"))
  return body("poll", text, poll.contextInfo)
}

/** Text and kind of a message's own content, or `null` for content that is not a message. */
function bodyOf(content: proto.IMessage): Body | null {
  if (content.conversation) return body("text", content.conversation, null)
  const extended = content.extendedTextMessage
  if (extended) return body("text", extended.text, extended.contextInfo)

  const image = content.imageMessage
  if (image) return withMedia("image", image.caption, image)
  const video = content.videoMessage
  if (video) return withMedia("video", video.caption, video)
  const videoNote = content.ptvMessage
  if (videoNote) return withMedia("video_note", videoNote.caption, videoNote)
  const audio = content.audioMessage
  if (audio) return withMedia(audio.ptt ? "voice" : "audio", null, audio)
  const document = content.documentMessage
  if (document)
    return withMedia("document", document.caption ?? document.title ?? document.fileName, document)
  const sticker = content.stickerMessage ?? content.lottieStickerMessage?.message?.stickerMessage
  if (sticker) return withMedia("sticker", null, sticker)
  const album = content.albumMessage
  if (album) {
    const counts = [
      album.expectedImageCount ? `${album.expectedImageCount} photos` : null,
      album.expectedVideoCount ? `${album.expectedVideoCount} videos` : null,
    ]
    return body("album", counts.filter(Boolean).join(", "), album.contextInfo)
  }

  const location = content.locationMessage
  if (location) {
    const where = stack(location.name, location.address)
    return body(
      "location",
      stack(where, `${location.degreesLatitude},${location.degreesLongitude}`),
      location.contextInfo,
    )
  }
  const live = content.liveLocationMessage
  if (live) {
    return body(
      "live_location",
      stack(live.caption, `${live.degreesLatitude},${live.degreesLongitude}`),
      live.contextInfo,
    )
  }
  const contact = content.contactMessage
  if (contact) return body("contact", contact.displayName, contact.contextInfo)
  const contacts = content.contactsArrayMessage
  if (contacts) {
    const names = (contacts.contacts ?? []).map((card) => card.displayName).filter(Boolean)
    return body("contact", names.join(", "), contacts.contextInfo)
  }

  const poll =
    content.pollCreationMessage ??
    content.pollCreationMessageV2 ??
    content.pollCreationMessageV3 ??
    content.pollCreationMessageV5
  if (poll) return pollBody(poll)
  const event = content.eventMessage
  if (event) {
    const when = toNumber(event.startTime)
    const start = when ? new Date(when * 1000).toISOString() : null
    return body(
      "event",
      stack(event.name, event.description, event.location?.name, start),
      event.contextInfo,
    )
  }
  const invite = content.groupInviteMessage
  if (invite)
    return body("group_invite", stack(invite.groupName, invite.caption), invite.contextInfo)
  if (content.pinInChatMessage) return body("pin", null, null)

  if (content.templateMessage) return templateBody(content.templateMessage)
  const hsm = content.highlyStructuredMessage?.hydratedHsm
  if (hsm) return templateBody(hsm)
  const buttons = content.buttonsMessage
  if (buttons) {
    const labels = buttonLabels(
      (buttons.buttons ?? []).map((button) => button.buttonText?.displayText),
    )
    const text = stack(buttons.text, buttons.contentText, buttons.footerText, labels)
    const header = buttons.imageMessage ?? buttons.videoMessage ?? buttons.documentMessage
    if (header) {
      const kind = buttons.imageMessage ? "image" : buttons.videoMessage ? "video" : "document"
      return withMedia(kind, text, { ...header, contextInfo: buttons.contextInfo })
    }
    return body("text", text, buttons.contextInfo)
  }
  if (content.interactiveMessage) return interactiveBody(content.interactiveMessage)
  const list = content.listMessage
  if (list) return listBody(list)

  // Replies to business messages read as what the person tapped.
  const buttonReply = content.buttonsResponseMessage
  if (buttonReply) return body("text", buttonReply.selectedDisplayText, buttonReply.contextInfo)
  const templateReply = content.templateButtonReplyMessage
  if (templateReply)
    return body("text", templateReply.selectedDisplayText, templateReply.contextInfo)
  const listReply = content.listResponseMessage
  if (listReply)
    return body("text", stack(listReply.title, listReply.description), listReply.contextInfo)
  const interactiveReply = content.interactiveResponseMessage
  if (interactiveReply)
    return body("text", interactiveReply.body?.text, interactiveReply.contextInfo)

  // A comment on a channel post carries an ordinary message inside.
  const comment = content.commentMessage?.message
  if (comment) return bodyOf(normalizeMessageContent(comment) ?? {})

  // WhatsApp could not decrypt it yet; a retry replaces this row when it lands.
  if (content.placeholderMessage) return body("unavailable", null, null)
  return null
}

/** The type name of content this module does not render, for the `other` kind. */
function contentType(content: proto.IMessage): string | null {
  for (const [key, value] of Object.entries(content)) {
    if (
      value !== null &&
      value !== undefined &&
      key.endsWith("Message") &&
      key !== "senderKeyDistributionMessage"
    ) {
      return key
    }
  }
  return null
}

/**
 * Classifies an in-place update to a known message: an edit carries the new
 * content, a revoke turns the message into a stub. Anything else (status,
 * receipts, poll tallies) is not archived.
 */
export function interpretUpdate(update: WAMessageUpdate): MessageEvent {
  const { id, remoteJid } = update.key
  if (!id || !remoteJid) return { type: "ignored" }
  if (update.update.messageStubType === STUB_REVOKE) return { type: "revoke", remoteJid, id }
  const content = normalizeMessageContent(update.update.message)
  const edited = content?.editedMessage?.message ?? content?.protocolMessage?.editedMessage
  const text = edited ? bodyOf(normalizeMessageContent(edited) ?? {})?.text : null
  return text ? { type: "edit", remoteJid, id, text } : { type: "ignored" }
}

/**
 * A reaction to the message `target` points at. `fallbackAt` stands in for the
 * reaction's own timestamp when it has none (Unix milliseconds).
 */
export function reactionEvent(
  target: WAMessageKey,
  reaction: proto.IReaction,
  fallbackAt: number,
): MessageEvent {
  const { id, remoteJid } = target
  if (!id || !remoteJid) return { type: "ignored" }
  const sender = reaction.key
  return {
    type: "reaction",
    remoteJid,
    id,
    senderJid: sender?.fromMe ? null : (sender?.participant ?? sender?.remoteJid ?? null),
    emoji: reaction.text ?? "",
    at: toNumber(reaction.senderTimestampMs) ?? fallbackAt,
  }
}

/** Reactions a history message carries along (history does not replay them as messages). */
export function attachedReactions(raw: WAMessage): MessageEvent[] {
  return (raw.reactions ?? []).map((reaction) => reactionEvent(raw.key, reaction, 0))
}

/** Classifies one raw message. */
export function interpret(raw: WAMessage): MessageEvent {
  const key: WAMessageKey = raw.key
  const { id, remoteJid } = key
  const content = normalizeMessageContent(raw.message)
  if (!id || !remoteJid || !content) return { type: "ignored" }
  // History puts a group message's author on the message, not on its key.
  const participant = key.participant ?? raw.participant ?? null
  const sentAt = toNumber(raw.messageTimestamp) ?? Math.floor(Date.now() / 1000)

  const protocol = content.protocolMessage
  if (protocol) {
    const targetId = protocol.key?.id
    if (!targetId) return { type: "ignored" }
    if (protocol.type === PROTOCOL_REVOKE) return { type: "revoke", remoteJid, id: targetId }
    if (protocol.type === PROTOCOL_MESSAGE_EDIT && protocol.editedMessage) {
      const edited = bodyOf(normalizeMessageContent(protocol.editedMessage) ?? {})
      if (edited?.text) return { type: "edit", remoteJid, id: targetId, text: edited.text }
    }
    return { type: "ignored" }
  }

  const reaction = content.reactionMessage
  if (reaction) {
    const targetId = reaction.key?.id
    if (!targetId) return { type: "ignored" }
    return {
      type: "reaction",
      remoteJid,
      id: targetId,
      senderJid: key.fromMe ? null : (participant ?? remoteJid),
      emoji: reaction.text ?? "",
      at: toNumber(reaction.senderTimestampMs) ?? sentAt * 1000,
    }
  }

  // Poll votes are encrypted to the poll and arrive as their own messages;
  // tallying them is not the archive's job.
  if (content.pollUpdateMessage || content.encCommentMessage || content.secretEncryptedMessage) {
    return { type: "ignored" }
  }

  const rendered = bodyOf(content)
  const unknownType = rendered ? null : contentType(content)
  // Content with no recognizable message in it (bare key distribution, context
  // info only) is protocol plumbing, not something anyone wrote.
  if (!rendered && !unknownType) return { type: "ignored" }
  const context = rendered?.context
  return {
    type: "message",
    message: {
      remoteJid,
      participant,
      id,
      fromMe: key.fromMe ?? false,
      pushName: raw.pushName ?? null,
      sentAt,
      kind: rendered?.kind ?? "other",
      text: rendered ? rendered.text : unknownType,
      quotedId: context?.stanzaId ?? null,
      mentions: context?.mentionedJid ?? [],
      forwarded: context?.isForwarded ?? false,
      media: rendered?.media ?? null,
    },
  }
}
