import { describe, expect, test } from "bun:test"
import type { WAMessage } from "@oxidezap/baileyrs/lib/Types/Message.js"

import { readTranscribeConfig, withoutCaptionCredits } from "../src/transcribe"
import { mediaOf } from "../src/whatsapp/raw"

describe("transcription config", () => {
  test("off or empty turns it off, and the language is optional", () => {
    expect(readTranscribeConfig({ TRANSCRIBE_MODEL: "off" })).toBeNull()
    expect(readTranscribeConfig({})).toBeNull()
    expect(
      readTranscribeConfig({
        TRANSCRIBE_MODEL: "@cf/openai/whisper-large-v3-turbo",
        TRANSCRIBE_LANGUAGE: "",
      }),
    ).toEqual({ model: "@cf/openai/whisper-large-v3-turbo", language: null })
    expect(
      readTranscribeConfig({ TRANSCRIBE_MODEL: "@cf/deepgram/nova-3", TRANSCRIBE_LANGUAGE: "pt" }),
    ).toEqual({ model: "@cf/deepgram/nova-3", language: "pt" })
  })

  test("an unknown model says what to fix", () => {
    expect(readTranscribeConfig({ TRANSCRIBE_MODEL: "whisper" })).toStartWith(
      "TRANSCRIBE_MODEL must be",
    )
  })
})

describe("caption credits", () => {
  test("a transcript that is only a subtitle credit is no speech", () => {
    expect(withoutCaptionCredits(" Legendas pela comunidade Amara.org ")).toBe("")
    expect(withoutCaptionCredits("Obrigado por assistir!")).toBe("")
  })

  test("real speech is kept, even when it mentions a credit", () => {
    expect(withoutCaptionCredits(" see you at eight ")).toBe("see you at eight")
    const long = `${"I talked a lot about the project and the delivery date ".repeat(3)}thanks for watching`
    expect(withoutCaptionCredits(long)).toBe(long)
  })
})

function withContent(content: WAMessage["message"]): WAMessage {
  return { key: { remoteJid: "1@lid", id: "A", fromMe: false }, message: content }
}

describe("media in a message", () => {
  const voice = { mimetype: "audio/ogg; codecs=opus", ptt: true }

  test("a voice message has its type and mimetype", () => {
    const message = withContent({ audioMessage: voice })
    expect(mediaOf(message)).toEqual({
      type: "audio",
      mimetype: "audio/ogg; codecs=opus",
      viewOnce: false,
      thumbnail: null,
    })
  })

  test("view-once media is flagged, wrapped or not", () => {
    const wrapped = withContent({ viewOnceMessageV2: { message: { imageMessage: {} } } })
    const flagged = withContent({ audioMessage: { ...voice, viewOnce: true } })
    expect(mediaOf(wrapped)?.viewOnce).toBe(true)
    expect(mediaOf(flagged)?.viewOnce).toBe(true)
  })

  test("a video carries its preview, and a missing mimetype gets the usual one", () => {
    const preview = new Uint8Array([0xff, 0xd8, 0xff])
    const video = withContent({ ptvMessage: { jpegThumbnail: preview } })
    expect(mediaOf(video)).toEqual({
      type: "video",
      mimetype: "video/mp4",
      viewOnce: false,
      thumbnail: preview,
    })
    expect(mediaOf(withContent({ stickerMessage: {} }))?.mimetype).toBe("image/webp")
  })

  test("a message without media has none", () => {
    const text = withContent({ conversation: "oi" })
    expect(mediaOf(text)).toBeNull()
  })
})
