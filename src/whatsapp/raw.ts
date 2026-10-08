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

export function decodeRaw(raw: Uint8Array): KeyedMessage {
  const message = proto.WebMessageInfo.decode(raw)
  if (!hasKey(message)) throw new Error("archived message has no key")
  return message
}
