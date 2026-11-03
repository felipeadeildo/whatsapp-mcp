import { normalizeMessageContent } from "@oxidezap/baileyrs/lib/Media/content.js"
import type { WAMessage, WAMessageKey } from "@oxidezap/baileyrs/lib/Types/Message.js"
import { proto } from "@oxidezap/baileyrs/lib/WAProto/runtime.js"

export type KeyedMessage = WAMessage & { key: WAMessageKey }

function hasKey<T extends { key?: WAMessageKey | null }>(
  message: T,
): message is T & { key: WAMessageKey } {
  return message.key !== null && message.key !== undefined
}

export function encodeRaw(message: WAMessage): Uint8Array | null {
  try {
    return proto.WebMessageInfo.encode(message).finish()
  } catch {
    // The derived columns are still worth keeping; only re-derivation is lost.
    return null
  }
}

export interface MediaContent {
  readonly type: "audio" | "image" | "sticker" | "video" | "document"
  readonly mimetype: string
  readonly viewOnce: boolean
  // The small JPEG preview WhatsApp sends inside the message, when it has one.
  readonly thumbnail: Uint8Array | null
}

const DEFAULT_MIMETYPES: Record<MediaContent["type"], string> = {
  audio: "audio/ogg; codecs=opus",
  image: "image/jpeg",
  sticker: "image/webp",
  video: "video/mp4",
  document: "application/octet-stream",
}

interface MediaFields {
  readonly mimetype?: string | null
  readonly viewOnce?: boolean | null
  readonly jpegThumbnail?: Uint8Array | null
}

function mediaFieldsOf(content: proto.IMessage): [MediaContent["type"], MediaFields] | null {
  if (content.audioMessage) return ["audio", content.audioMessage]
  if (content.imageMessage) return ["image", content.imageMessage]
  const sticker = content.stickerMessage ?? content.lottieStickerMessage?.message?.stickerMessage
  if (sticker) return ["sticker", sticker]
  const video = content.videoMessage ?? content.ptvMessage
  if (video) return ["video", video]
  if (content.documentMessage) return ["document", content.documentMessage]
  return null
}

export function mediaOf(message: WAMessage): MediaContent | null {
  const outer = message.message
  const content = normalizeMessageContent(outer)
  const found = content ? mediaFieldsOf(content) : null
  if (!found) return null
  const [type, fields] = found
  const wrapper = outer?.ephemeralMessage?.message ?? outer
  const wrappedViewOnce = Boolean(
    wrapper?.viewOnceMessage ?? wrapper?.viewOnceMessageV2 ?? wrapper?.viewOnceMessageV2Extension,
  )
  const thumbnail = fields.jpegThumbnail
  return {
    type,
    mimetype: fields.mimetype || DEFAULT_MIMETYPES[type],
    viewOnce: wrappedViewOnce || fields.viewOnce === true,
    thumbnail: thumbnail && thumbnail.length > 0 ? thumbnail : null,
  }
}

export function decodeRaw(raw: Uint8Array): KeyedMessage {
  const message = proto.WebMessageInfo.decode(raw)
  if (!hasKey(message)) throw new Error("archived message has no key")
  return message
}
