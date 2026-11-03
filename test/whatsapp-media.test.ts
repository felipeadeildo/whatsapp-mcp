import { describe, expect, test } from "bun:test"
import type { WAMessage } from "@oxidezap/baileyrs/lib/Types/Message.js"

import { errorText } from "../src/errors"
import { type MediaSocket, whatsappMedia } from "../src/whatsapp/media"
import { encodeRaw } from "../src/whatsapp/raw"

// What the CDN said in production for Luciana's voice message and the receipt from 3 October.
const GONE_410 =
  "the CDN rejected the media reference: Download media not found/expired with status: 410: Download media not found/expired with status: 410: HTTP status 410"
const EXPIRED_403 =
  "the CDN rejected the media reference: MediaFailure { preparation: None, iq_kind: None, iq_code: None, http_status: Some(403), io_kind: None, media_validation: false, .. }: Download failed with status: 403: HTTP status 403"

const PREVIEW = new Uint8Array([0xff, 0xd8, 0xff, 0x01])
const PHOTO = new TextEncoder().encode("photo")

function archived(content: WAMessage["message"]): Uint8Array {
  const raw = encodeRaw({ key: { remoteJid: "1@lid", id: "A", fromMe: false }, message: content })
  if (!raw) throw new Error("could not encode the test message")
  return raw
}

const photo = archived({
  imageMessage: { mimetype: "image/jpeg", directPath: "/old", jpegThumbnail: PREVIEW },
})

// A phone that answers the first download with `failures` in order, then the file.
// A reupload hands back the message with a new direct path.
function fakeWhatsApp(
  raw: Uint8Array | null,
  failures: string[] = [],
  reupload: (message: WAMessage) => Promise<WAMessage> = async (message) => ({
    ...message,
    message: { imageMessage: { ...message.message?.imageMessage, directPath: "/new" } },
  }),
) {
  const calls: string[] = []
  let archivedRaw = raw
  const socket: MediaSocket = {
    async downloadMedia(message) {
      calls.push(`download ${message.message?.imageMessage?.directPath ?? "?"}`)
      const failure = failures.shift()
      if (failure) throw new Error(failure)
      return PHOTO
    },
    async updateMediaMessage(message) {
      calls.push("reupload")
      return reupload(message)
    },
  }
  const media = whatsappMedia({
    raw: () => archivedRaw,
    saveRaw: (_chatJid, _id, fresh) => {
      calls.push("save")
      archivedRaw = fresh
    },
    socket: () => socket,
  })
  return { media, calls }
}

const A = { chatJid: "1@lid", id: "A" }

async function failureOf(work: Promise<unknown>): Promise<string> {
  try {
    await work
  } catch (error) {
    return errorText(error)
  }
  throw new Error("expected a failure")
}

describe("whatsapp media", () => {
  test("a file downloads on the first try", async () => {
    const { media, calls } = fakeWhatsApp(photo)

    expect(await media.fetch(A)).toEqual({ bytes: PHOTO, mimetype: "image/jpeg" })
    expect(calls).toEqual(["download /old"])
  })

  test("an expired link asks the phone to upload the file again", async () => {
    for (const failure of [GONE_410, EXPIRED_403]) {
      const { media, calls } = fakeWhatsApp(photo, [failure])

      expect((await media.fetch(A)).bytes).toEqual(PHOTO)
      expect(calls).toEqual(["download /old", "reupload", "download /new", "save"])
    }
  })

  test("after a reupload, the next download uses the new link without the phone", async () => {
    const { media, calls } = fakeWhatsApp(photo, [GONE_410])
    await media.fetch(A)
    calls.length = 0

    await media.fetch(A)
    expect(calls).toEqual(["download /new"])
  })

  test("any other failure does not bother the phone", async () => {
    const { media, calls } = fakeWhatsApp(photo, ["HTTP status 500"])

    expect(await failureOf(media.fetch(A))).toBe("could not download the media: HTTP status 500")
    expect(calls).toEqual(["download /old"])
  })

  test("a phone that cannot upload the file again says so", async () => {
    const { media } = fakeWhatsApp(photo, [GONE_410], async () => {
      throw new Error("Media not found on server")
    })

    expect(await failureOf(media.fetch(A))).toBe(
      "the media expired on WhatsApp's servers and the phone did not upload it again: Media not found on server",
    )
  })

  test("view-once media is refused and has no preview", async () => {
    const once = archived({
      viewOnceMessageV2: { message: { imageMessage: { jpegThumbnail: PREVIEW } } },
    })
    const { media, calls } = fakeWhatsApp(once)

    expect(await failureOf(media.fetch(A))).toBe("view-once media is not opened")
    expect(media.thumbnail(A)).toBeNull()
    expect(calls).toEqual([])
  })

  test("a message without media, or missing from the archive, is refused", async () => {
    const text = fakeWhatsApp(archived({ conversation: "oi" })).media
    const missing = fakeWhatsApp(null).media

    expect(await failureOf(text.fetch(A))).toBe("the archived message has no media to download")
    expect(await failureOf(missing.fetch(A))).toBe("message A not found in 1@lid")
    expect(missing.thumbnail(A)).toBeNull()
  })

  test("the preview comes from the archived message", () => {
    expect(fakeWhatsApp(photo).media.thumbnail(A)).toEqual(PREVIEW)
  })
})
