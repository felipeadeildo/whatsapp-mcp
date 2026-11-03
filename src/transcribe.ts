// Voice and audio transcripts, made when a read returns a message that has none and
// saved so later reads and search have them. Workers AI does the speech to text;
// TRANSCRIBE_MODEL picks Whisper or Deepgram, which take different inputs.
import { type MediaFile, toBase64 } from "./bytes"
import { withDeadline } from "./deadline"
import { errorText } from "./errors"
import type { ArchivedMessage } from "./store/archive"
import type { MessageKind } from "./whatsapp/message"

// Why a voice or audio message carries no transcript. `pending` is still running,
// `not_requested` was left out of this read, `unavailable` failed with
// `transcriptError`, `no_speech` means nothing was said.
export type TranscriptStatus = "pending" | "not_requested" | "unavailable" | "no_speech"

export type ReadMessage = ArchivedMessage & {
  readonly transcriptStatus?: TranscriptStatus
  readonly transcriptError?: string
}

// Speech to text for one file. "" when nothing was said.
export type Speech = (audio: MediaFile) => Promise<string>

export interface TranscriptsDeps {
  // Null when transcription is off, or a message that says how to fix the configuration.
  readonly speech: Speech | null | string
  fetchAudio(message: ArchivedMessage): Promise<MediaFile>
  save(message: ArchivedMessage, transcript: string): void
  // Keeps a transcription the read stopped waiting for alive until it saves.
  waitUntil(work: Promise<unknown>): void
  readonly perRead?: number
  readonly waitMs?: number
}

export interface VoiceTranscripts {
  // Adds what is known about each voice or audio message's transcript. With `wanted`,
  // transcribes up to `perRead` of those without one, newest first, and waits up to
  // `waitMs` for them.
  read(messages: readonly ArchivedMessage[], wanted: boolean): Promise<ReadMessage[]>
}

const TRANSCRIBED_KINDS: ReadonlySet<MessageKind> = new Set(["voice", "audio"])
const PER_READ = 10
const WAIT_MS = 8_000
// Workers AI takes the audio in one request; larger files are long recordings.
const MAX_BYTES = 20 * 1024 * 1024

type Outcome =
  | { readonly transcript: string }
  | { readonly transcriptStatus: "pending" | "not_requested" | "no_speech" }
  | { readonly transcriptStatus: "unavailable"; readonly transcriptError: string }

const NO_SPEECH: Outcome = { transcriptStatus: "no_speech" }
const PENDING: Outcome = { transcriptStatus: "pending" }
const NOT_REQUESTED: Outcome = { transcriptStatus: "not_requested" }

function spoken(transcript: string): Outcome {
  return transcript === "" ? NO_SPEECH : { transcript }
}

function unavailable(error: unknown): Outcome {
  return { transcriptStatus: "unavailable", transcriptError: errorText(error) }
}

export function voiceTranscripts(deps: TranscriptsDeps): VoiceTranscripts {
  const { speech, perRead = PER_READ, waitMs = WAIT_MS } = deps
  // Keyed by chat and message id, so overlapping reads share one transcription.
  const running = new Map<string, Promise<string>>()

  async function transcribeMessage(message: ArchivedMessage, toText: Speech): Promise<string> {
    const size = message.media?.bytes ?? 0
    if (size > MAX_BYTES) throw new Error(`too large to transcribe (${size} bytes)`)
    const transcript = await toText(await deps.fetchAudio(message))
    deps.save(message, transcript)
    return transcript
  }

  function transcribeOnce(message: ArchivedMessage, toText: Speech): Promise<string> {
    const key = `${message.chatJid}\u0000${message.id}`
    const existing = running.get(key)
    if (existing) return existing
    const job = transcribeMessage(message, toText).finally(() => running.delete(key))
    running.set(key, job)
    deps.waitUntil(job.catch(() => undefined))
    return job
  }

  async function transcribeNewest(
    missing: readonly ArchivedMessage[],
    toText: Speech,
  ): Promise<Map<ArchivedMessage, Outcome>> {
    const outcomes = new Map<ArchivedMessage, Outcome>()
    const newest = missing.toSorted((a, b) => b.sentAt - a.sentAt).slice(0, perRead)
    await Promise.all(
      newest.map(async (message) => {
        const job = transcribeOnce(message, toText).then(spoken, unavailable)
        outcomes.set(message, await withDeadline(job, waitMs, PENDING))
      }),
    )
    return outcomes
  }

  async function outcomesOf(
    missing: readonly ArchivedMessage[],
    wanted: boolean,
  ): Promise<Map<ArchivedMessage, Outcome>> {
    if (speech === null || missing.length === 0) return new Map()
    if (typeof speech === "string") {
      return new Map(missing.map((message) => [message, unavailable(speech)]))
    }
    const outcomes = new Map(missing.map((message) => [message, NOT_REQUESTED]))
    if (!wanted) return outcomes
    for (const [message, outcome] of await transcribeNewest(missing, speech)) {
      outcomes.set(message, outcome)
    }
    return outcomes
  }

  return {
    async read(messages, wanted) {
      const voices = messages.filter(
        (message) => TRANSCRIBED_KINDS.has(message.kind) && !message.deleted,
      )
      const missing = voices.filter((message) => message.transcript === null)
      const outcomes = await outcomesOf(missing, wanted)
      for (const message of voices) {
        if (message.transcript === "") outcomes.set(message, NO_SPEECH)
      }
      return messages.map((message) => ({ ...message, ...outcomes.get(message) }))
    },
  }
}

export interface TranscribeConfig {
  readonly model: SpeechModel
  // ISO 639-1, e.g. "pt"; null lets the model detect it.
  readonly language: string | null
}

type Transcriber = (ai: Ai, audio: MediaFile, language: string | null) => Promise<string>

const WHISPER = "@cf/openai/whisper-large-v3-turbo"
const NOVA = "@cf/deepgram/nova-3"

const TRANSCRIBERS = {
  [WHISPER]: async (ai, audio, language) => {
    const result = await ai.run(WHISPER, {
      audio: toBase64(audio.bytes),
      ...(language ? { language } : {}),
      // Skipping silence and not chaining segments keeps Whisper from inventing text.
      vad_filter: true,
      condition_on_previous_text: false,
    })
    return withoutCaptionCredits(result.text)
  },
  [NOVA]: async (ai, audio, language) => {
    const result = await ai.run(NOVA, {
      audio: { body: new Blob([audio.bytes]).stream(), contentType: audio.mimetype },
      ...(language ? { language } : { detect_language: true }),
      smart_format: true,
      punctuate: true,
    })
    return result.results?.channels?.[0]?.alternatives?.[0]?.transcript?.trim() ?? ""
  },
} satisfies Record<string, Transcriber>

export type SpeechModel = keyof typeof TRANSCRIBERS

function isSpeechModel(model: string): model is SpeechModel {
  return Object.hasOwn(TRANSCRIBERS, model)
}

interface TranscribeEnv {
  readonly TRANSCRIBE_MODEL?: string
  readonly TRANSCRIBE_LANGUAGE?: string
}

/** The configuration, null when transcription is off, or a message that says what to fix. */
export function readTranscribeConfig(env: TranscribeEnv): TranscribeConfig | null | string {
  const model = env.TRANSCRIBE_MODEL?.trim() ?? ""
  if (model === "" || model === "off") return null
  if (!isSpeechModel(model)) {
    return `TRANSCRIBE_MODEL must be one of ${Object.keys(TRANSCRIBERS).join(", ")} or "off", not "${model}".`
  }
  return { model, language: env.TRANSCRIBE_LANGUAGE?.trim() || null }
}

/** Workers AI speech to text as configured, or what readTranscribeConfig said instead. */
export function speechFromEnv(env: TranscribeEnv & { readonly AI: Ai }): Speech | null | string {
  const config = readTranscribeConfig(env)
  if (config === null || typeof config === "string") return config
  return (audio) => TRANSCRIBERS[config.model](env.AI, audio, config.language)
}

// Whisper learned subtitle credits from video captions and writes them over
// silence and noise. A transcript that is only a credit is no speech.
const CAPTION_CREDITS = [
  /amara\.org/i,
  /legendas? (pela|por|da) comunidade/i,
  /subt[ií]tulos (realizados )?por la comunidad/i,
  /subtitles by/i,
  /sous-titres/i,
  /obrigad[oa] por assistir/i,
  /thanks? (you )?for watching/i,
]
const CREDIT_MAX_LENGTH = 120

export function withoutCaptionCredits(text: string): string {
  const trimmed = text.trim()
  if (
    trimmed.length <= CREDIT_MAX_LENGTH &&
    CAPTION_CREDITS.some((credit) => credit.test(trimmed))
  ) {
    return ""
  }
  return trimmed
}
