import { describe, expect, test } from "bun:test"

import type { MediaFile } from "../src/bytes"
import { type Speech, type TranscriptsDeps, voiceTranscripts } from "../src/transcribe"
import { archivedMessage as message } from "./archived-message"

// Each voice message's audio says "said <id>". Records what was fetched, saved and
// left running in the background.
function fakeAccount(speech: Speech | null | string = async (audio) => decode(audio)) {
  const saved = new Map<string, string>()
  const fetched: string[] = []
  const background: Promise<unknown>[] = []
  const deps: TranscriptsDeps = {
    speech,
    async fetchAudio(voice) {
      fetched.push(voice.id)
      return { bytes: new TextEncoder().encode(`said ${voice.id}`), mimetype: "audio/ogg" }
    },
    save: (voice, transcript) => saved.set(voice.id, transcript),
    waitUntil: (work) => background.push(work),
    waitMs: 50,
  }
  return { deps, saved, fetched, background }
}

function decode(audio: MediaFile): string {
  return new TextDecoder().decode(audio.bytes)
}

describe("voice transcripts", () => {
  test("a read transcribes the newest voice messages and leaves the rest out", async () => {
    const account = fakeAccount()
    const transcripts = voiceTranscripts({ ...account.deps, perRead: 2 })
    const voices = [1, 2, 3].map((n) => message(`V${n}`, { sentAt: n }))
    const text = message("T", { kind: "text", text: "hi" })

    const read = await transcripts.read([...voices, text], true)

    expect(read.map((m) => [m.id, m.transcript, m.transcriptStatus])).toEqual([
      ["V1", null, "not_requested"],
      ["V2", "said V2", undefined],
      ["V3", "said V3", undefined],
      ["T", null, undefined],
    ])
    expect(Object.fromEntries(account.saved)).toEqual({ V2: "said V2", V3: "said V3" })
  })

  test("a read that does not want transcripts makes no calls", async () => {
    const account = fakeAccount()
    const read = await voiceTranscripts(account.deps).read([message("V")], false)

    expect(read[0]?.transcriptStatus).toBe("not_requested")
    expect(account.fetched).toEqual([])
  })

  test("a slow transcript is pending, and saves once it finishes", async () => {
    const account = fakeAccount(async (audio) => {
      await Bun.sleep(120)
      return decode(audio)
    })
    const read = await voiceTranscripts(account.deps).read([message("V")], true)

    expect(read[0]?.transcriptStatus).toBe("pending")
    expect(account.saved.size).toBe(0)
    await Promise.all(account.background)
    expect(account.saved.get("V")).toBe("said V")
  })

  test("overlapping reads share one transcription", async () => {
    let calls = 0
    const account = fakeAccount(async (audio) => {
      calls += 1
      await Bun.sleep(10)
      return decode(audio)
    })
    const transcripts = voiceTranscripts(account.deps)
    const [first, second] = await Promise.all([
      transcripts.read([message("V")], true),
      transcripts.read([message("V")], true),
    ])

    expect(calls).toBe(1)
    expect(first[0]?.transcript).toBe("said V")
    expect(second[0]?.transcript).toBe("said V")
  })

  test("a failure says why, and the next read tries again", async () => {
    let attempts = 0
    const account = fakeAccount(async (audio) => {
      attempts += 1
      if (attempts === 1) throw new Error("the phone is offline")
      return decode(audio)
    })
    const transcripts = voiceTranscripts(account.deps)

    const failed = await transcripts.read([message("V")], true)
    expect(failed[0]).toMatchObject({
      transcriptStatus: "unavailable",
      transcriptError: "the phone is offline",
    })
    const retried = await transcripts.read([message("V")], true)
    expect(retried[0]?.transcript).toBe("said V")
  })

  test("silence is no speech, saved or just transcribed", async () => {
    const account = fakeAccount(async () => "")
    const transcripts = voiceTranscripts(account.deps)
    const read = await transcripts.read([message("NEW"), message("OLD", { transcript: "" })], true)

    expect(read.map((m) => m.transcriptStatus)).toEqual(["no_speech", "no_speech"])
    expect(account.saved.get("NEW")).toBe("")
    expect(account.fetched).toEqual(["NEW"])
  })

  test("transcribed and deleted messages are left as they are", async () => {
    const account = fakeAccount()
    const read = await voiceTranscripts(account.deps).read(
      [message("DONE", { transcript: "already" }), message("GONE", { deleted: true })],
      true,
    )

    expect(read.map((m) => [m.transcript, m.transcriptStatus])).toEqual([
      ["already", undefined],
      [null, undefined],
    ])
    expect(account.fetched).toEqual([])
  })

  test("a broken configuration shows on every voice message without calls", async () => {
    const account = fakeAccount('TRANSCRIBE_MODEL must be one of … or "off", not "whisper".')
    const read = await voiceTranscripts(account.deps).read([message("V")], true)

    expect(read[0]).toMatchObject({
      transcriptStatus: "unavailable",
      transcriptError: 'TRANSCRIBE_MODEL must be one of … or "off", not "whisper".',
    })
    expect(account.fetched).toEqual([])
  })

  test("with transcription off, voice messages carry no status", async () => {
    const account = fakeAccount(null)
    const read = await voiceTranscripts(account.deps).read([message("V")], true)

    expect(read[0]?.transcriptStatus).toBeUndefined()
    expect(account.fetched).toEqual([])
  })
})
