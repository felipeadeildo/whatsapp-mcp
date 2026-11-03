import type { WAMessage } from "@oxidezap/baileyrs/lib/Types/Message.js"

import type { MediaFile } from "../bytes"
import { errorText } from "../errors"
import { decodeRaw, mediaOf } from "./raw"

// The two socket calls a download needs, so tests can stand in for WhatsApp.
export interface MediaSocket {
  downloadMedia(message: WAMessage, type: "buffer"): Promise<Uint8Array>
  updateMediaMessage(message: WAMessage): Promise<WAMessage>
}

export interface MessageRef {
  readonly chatJid: string
  readonly id: string
}

export interface WhatsAppMediaDeps {
  // The archived protobuf of a message, null when the archive has none.
  raw(chatJid: string, id: string): Uint8Array | null
  // Throws when WhatsApp is not connected, so the error shows at download time.
  socket(): MediaSocket
}

export interface WhatsAppMedia {
  // The file a message carries. Asks the phone to upload it again when the CDN link
  // expired. Refuses view-once media.
  fetch(message: MessageRef): Promise<MediaFile>
  // The JPEG preview inside the message, null when it has none or is view-once.
  thumbnail(message: MessageRef): Uint8Array | null
}

// How the CDN answers for media it no longer has, or for a link past its expiry.
const MEDIA_EXPIRED = /\b(403|404|410)\b|expired/i

async function download(socket: MediaSocket, message: WAMessage): Promise<Uint8Array> {
  try {
    return await socket.downloadMedia(message, "buffer")
  } catch (error) {
    if (!MEDIA_EXPIRED.test(errorText(error))) {
      throw new Error(`could not download the media: ${errorText(error)}`, { cause: error })
    }
  }
  try {
    const reuploaded = await socket.updateMediaMessage(message)
    return await socket.downloadMedia(reuploaded, "buffer")
  } catch (error) {
    throw new Error(
      `the media expired on WhatsApp's servers and the phone did not upload it again: ${errorText(error)}`,
      { cause: error },
    )
  }
}

export function whatsappMedia(deps: WhatsAppMediaDeps): WhatsAppMedia {
  return {
    async fetch(ref) {
      const raw = deps.raw(ref.chatJid, ref.id)
      if (!raw) throw new Error(`message ${ref.id} not found in ${ref.chatJid}`)
      const message = decodeRaw(raw)
      const media = mediaOf(message)
      if (!media) throw new Error("the archived message has no media to download")
      if (media.viewOnce) throw new Error("view-once media is not opened")
      return { bytes: await download(deps.socket(), message), mimetype: media.mimetype }
    },

    thumbnail(ref) {
      const raw = deps.raw(ref.chatJid, ref.id)
      const media = raw ? mediaOf(decodeRaw(raw)) : null
      return media && !media.viewOnce ? media.thumbnail : null
    },
  }
}
