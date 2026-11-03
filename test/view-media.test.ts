import { describe, expect, test } from "bun:test"

import type { MediaFile } from "../src/bytes"
import type { ArchivedMessage } from "../src/store/archive"
import type { VoiceTranscripts } from "../src/transcribe"
import {
  describePrompt,
  type MediaViewerDeps,
  mediaViewer,
  tidyDescription,
  type Vision,
} from "../src/view-media"
import { archivedMessage } from "./archived-message"

const PREVIEW = new Uint8Array([0xff, 0xd8, 0xff, 0x01])

function image(id: string, overrides: Partial<ArchivedMessage> = {}): ArchivedMessage {
  const media = { mimetype: "image/jpeg", fileName: null, bytes: 1000 }
  return archivedMessage(id, {
    kind: "image",
    media: { ...media, seconds: null, width: null, height: null },
    ...overrides,
  })
}

// Every image file holds its message id. Records what was downloaded, described,
// saved and left running in the background.
function fakeAccount(
  vision: Vision | null | string = async (file) =>
    `a photo of ${new TextDecoder().decode(file.bytes)}`,
  options: { fileBytes?: number; failDownload?: string } = {},
) {
  const downloaded: string[] = []
  const saved = new Map<string, string>()
  const background: Promise<unknown>[] = []
  const transcripts: VoiceTranscripts = {
    read: async (messages) => messages.map((message) => ({ ...message, transcript: "hello" })),
  }
  const deps: MediaViewerDeps = {
    vision,
    transcripts,
    async fetchMedia(message): Promise<MediaFile> {
      downloaded.push(message.id)
      if (options.failDownload) throw new Error(options.failDownload)
      const bytes = options.fileBytes
        ? new Uint8Array(options.fileBytes)
        : new TextEncoder().encode(message.id)
      return { bytes, mimetype: message.media?.mimetype ?? "image/jpeg" }
    },
    thumbnail: () => PREVIEW,
    saveDescription: (message, description) => saved.set(message.id, description),
    waitUntil: (work) => background.push(work),
    waitMs: 50,
  }
  return { deps, downloaded, saved, background }
}

describe("view media", () => {
  test("an image shows as sent, and its first view saves a description", async () => {
    const account = fakeAccount()
    const view = await mediaViewer(account.deps).view(image("CAT"))

    expect(view.image).toEqual({
      bytes: new TextEncoder().encode("CAT"),
      mimetype: "image/jpeg",
      source: "original",
    })
    expect(view.message.description).toBe("a photo of CAT")
    expect(account.saved.get("CAT")).toBe("a photo of CAT")
  })

  test("an image already described is not described again", async () => {
    let calls = 0
    const account = fakeAccount(async () => {
      calls += 1
      return "again"
    })
    const view = await mediaViewer(account.deps).view(image("CAT", { description: "a cat" }))

    expect(calls).toBe(0)
    expect(view.message.description).toBe("a cat")
    expect(view.image?.source).toBe("original")
  })

  test("a slow description is pending, and saves once it finishes", async () => {
    const account = fakeAccount(async () => {
      await Bun.sleep(120)
      return "a slow cat"
    })
    const view = await mediaViewer(account.deps).view(image("CAT"))

    expect(view.descriptionStatus).toBe("pending")
    expect(view.image?.source).toBe("original")
    await Promise.all(account.background)
    expect(account.saved.get("CAT")).toBe("a slow cat")
  })

  test("a failed description says why and still shows the image", async () => {
    const account = fakeAccount(async () => {
      throw new Error("model overloaded")
    })
    const view = await mediaViewer(account.deps).view(image("CAT"))

    expect(view).toMatchObject({
      descriptionStatus: "unavailable",
      descriptionError: "model overloaded",
      image: { source: "original" },
    })
  })

  test("a broken configuration shows on the image without a call", async () => {
    const account = fakeAccount('DESCRIBE_MODEL must be one of … or "off", not "gpt".')
    const view = await mediaViewer(account.deps).view(image("CAT"))

    expect(view.descriptionError).toStartWith("DESCRIBE_MODEL must be")
    expect(view.image?.source).toBe("original")
  })

  test("a failed download shows the preview and says why", async () => {
    const account = fakeAccount(undefined, { failDownload: "the phone did not upload it again" })
    const view = await mediaViewer(account.deps).view(image("OLD"))

    expect(view.image).toEqual({ bytes: PREVIEW, mimetype: "image/jpeg", source: "thumbnail" })
    expect(view.notice).toBe("the original image is unavailable: the phone did not upload it again")
    expect(account.saved.size).toBe(0)
  })

  test("an image too large to show is described and shown by its preview", async () => {
    const account = fakeAccount(async () => "a poster", { fileBytes: 4_000_000 })
    const view = await mediaViewer(account.deps).view(image("BIG"))

    expect(view.image?.source).toBe("thumbnail")
    expect(view.notice).toBe("the image is too large to show (4.0 MB)")
    expect(view.message.description).toBe("a poster")
  })

  test("a file too large to download is not downloaded", async () => {
    const account = fakeAccount()
    const big = image("HUGE", {
      media: {
        mimetype: "image/png",
        fileName: null,
        bytes: 30_000_000,
        seconds: null,
        width: null,
        height: null,
      },
    })
    const view = await mediaViewer(account.deps).view(big)

    expect(account.downloaded).toEqual([])
    expect(view.notice).toBe("the original image is unavailable: the file is too large (30.0 MB)")
  })

  test("a sticker shows without a description, and descriptions can be off", async () => {
    const off = fakeAccount(null)
    const photo = await mediaViewer(off.deps).view(image("CAT"))
    const described = fakeAccount()
    const sticker = await mediaViewer(described.deps).view(image("STK", { kind: "sticker" }))

    expect(photo.image?.source).toBe("original")
    expect(photo.message.description).toBeNull()
    expect(sticker.image?.source).toBe("original")
    expect(described.saved.size).toBe(0)
  })

  test("a video shows its preview without downloading", async () => {
    const account = fakeAccount()
    const view = await mediaViewer(account.deps).view(image("VID", { kind: "video" }))

    expect(view.image?.source).toBe("thumbnail")
    expect(account.downloaded).toEqual([])
  })

  test("a voice message shows its transcript", async () => {
    const account = fakeAccount()
    const view = await mediaViewer(account.deps).view(archivedMessage("V"))

    expect(view).toMatchObject({ image: null, message: { transcript: "hello" } })
  })

  test("a message without viewable media is refused", async () => {
    const viewer = mediaViewer(fakeAccount().deps)
    const pdf = image("PDF", {
      kind: "document",
      media: {
        mimetype: "application/pdf",
        fileName: "a.pdf",
        bytes: 10,
        seconds: null,
        width: null,
        height: null,
      },
    })

    for (const message of [
      pdf,
      archivedMessage("T", { kind: "text", text: "hi" }),
      image("GONE", { deleted: true }),
    ]) {
      const failure = await viewer.view(message).catch((error: unknown) => error)
      expect(failure).toBeInstanceOf(Error)
    }
  })
})

describe("descriptions", () => {
  test("a line the model repeats is kept once", () => {
    expect(tidyDescription(" Rua à noite.\n\n一風堂\n一風堂\n一風堂\nラーメン ")).toBe(
      "Rua à noite.\n\n一風堂\nラーメン",
    )
  })

  test("the prompt names the configured language, English when none is", () => {
    expect(describePrompt("pt")).toContain("Write in Portuguese.")
    expect(describePrompt(null)).toContain("Write in English.")
  })
})
