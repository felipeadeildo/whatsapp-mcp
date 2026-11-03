// Speech to text through Workers AI. Whisper and Deepgram take different inputs,
// so each supported model has its own call; TRANSCRIBE_MODEL picks one.

export interface TranscribeConfig {
  readonly model: SpeechModel
  // ISO 639-1, e.g. "pt"; null lets the model detect it.
  readonly language: string | null
}

type Transcriber = (
  ai: Ai,
  audio: Uint8Array,
  mimetype: string,
  language: string | null,
) => Promise<string>

const WHISPER = "@cf/openai/whisper-large-v3-turbo"
const NOVA = "@cf/deepgram/nova-3"

const TRANSCRIBERS = {
  [WHISPER]: async (ai, audio, _mimetype, language) => {
    const result = await ai.run(WHISPER, {
      audio: toBase64(audio),
      ...(language ? { language } : {}),
      // Skipping silence and not chaining segments keeps Whisper from inventing text.
      vad_filter: true,
      condition_on_previous_text: false,
    })
    return withoutCaptionCredits(result.text)
  },
  [NOVA]: async (ai, audio, mimetype, language) => {
    const result = await ai.run(NOVA, {
      audio: { body: new Blob([audio]).stream(), contentType: mimetype },
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

/** The configuration, null when transcription is off, or a message that says what to fix. */
export function readTranscribeConfig(env: {
  readonly TRANSCRIBE_MODEL?: string
  readonly TRANSCRIBE_LANGUAGE?: string
}): TranscribeConfig | null | string {
  const model = env.TRANSCRIBE_MODEL?.trim() ?? ""
  if (model === "" || model === "off") return null
  if (!isSpeechModel(model)) {
    return `TRANSCRIBE_MODEL must be one of ${Object.keys(TRANSCRIBERS).join(", ")} or "off", not "${model}".`
  }
  return { model, language: env.TRANSCRIBE_LANGUAGE?.trim() || null }
}

// "" when nothing was said.
export function transcribe(
  ai: Ai,
  config: TranscribeConfig,
  audio: Uint8Array,
  mimetype: string,
): Promise<string> {
  return TRANSCRIBERS[config.model](ai, audio, mimetype, config.language)
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

// Chunked so large files do not overflow the argument list.
function toBase64(bytes: Uint8Array): string {
  let binary = ""
  for (let start = 0; start < bytes.length; start += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(start, start + 0x8000))
  }
  return btoa(binary)
}
