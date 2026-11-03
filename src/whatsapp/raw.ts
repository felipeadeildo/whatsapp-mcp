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

export interface AudioContent {
  readonly mimetype: string
  readonly viewOnce: boolean
}

// WhatsApp sends voice messages as Opus in Ogg.
const DEFAULT_AUDIO_MIMETYPE = "audio/ogg; codecs=opus"

export function audioOf(message: WAMessage): AudioContent | null {
  const outer = message.message
  const audio = normalizeMessageContent(outer)?.audioMessage
  if (!audio) return null
  const wrapper = outer?.ephemeralMessage?.message ?? outer
  const wrappedViewOnce = Boolean(
    wrapper?.viewOnceMessage ?? wrapper?.viewOnceMessageV2 ?? wrapper?.viewOnceMessageV2Extension,
  )
  return {
    mimetype: audio.mimetype || DEFAULT_AUDIO_MIMETYPE,
    viewOnce: wrappedViewOnce || audio.viewOnce === true,
  }
}

export function decodeRaw(raw: Uint8Array): KeyedMessage {
  const message = proto.WebMessageInfo.decode(raw)
  if (!hasKey(message)) throw new Error("archived message has no key")
  return message
}
