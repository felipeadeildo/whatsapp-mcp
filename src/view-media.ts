// Why images are shown as sent and described only on the first view: docs/adr/0002.
import { type MediaFile, toBase64 } from "./bytes"
import { withDeadline } from "./deadline"
import { errorText } from "./errors"
import type { ArchivedMessage } from "./store/archive"
import type { ReadMessage, VoiceTranscripts } from "./transcribe"

// Text that stands in for an image. `pending` is still running, `unavailable` failed
// with `descriptionError`.
export type DescriptionStatus = "pending" | "unavailable"

export interface ShownImage extends MediaFile {
  // The file the message carries, or the small preview inside the message.
  readonly source: "original" | "thumbnail"
}

export interface MediaView {
  readonly message: ReadMessage
  readonly image: ShownImage | null
  // Why the original image is not shown.
  readonly notice?: string
  readonly descriptionStatus?: DescriptionStatus
  readonly descriptionError?: string
}

// What an image shows and the text in it. "" when there is nothing to say.
export type Vision = (image: MediaFile) => Promise<string>

export interface MediaViewerDeps {
  // Null when descriptions are off, or a message that says how to fix the configuration.
  readonly vision: Vision | null | string
  readonly transcripts: VoiceTranscripts
  fetchMedia(message: ArchivedMessage): Promise<MediaFile>
  // The JPEG preview inside the message, null when it has none.
  thumbnail(message: ArchivedMessage): Uint8Array | null
  saveDescription(message: ArchivedMessage, description: string): void
  // Keeps a description the view stopped waiting for alive until it saves.
  waitUntil(work: Promise<unknown>): void
  readonly waitMs?: number
}

export interface MediaViewer {
  view(message: ArchivedMessage): Promise<MediaView>
}

const WAIT_MS = 15_000
// Claude takes images up to 5 MB once base64 encoded; larger ones show the preview.
const MAX_SHOWN_BYTES = 3_750_000
// Durable Object RPC carries at most 32 MiB.
const MAX_DOWNLOAD_BYTES = 20 * 1024 * 1024

type Described = Pick<MediaView, "message" | "descriptionStatus" | "descriptionError">

function megabytes(bytes: number): string {
  return `${(bytes / 1_000_000).toFixed(1)} MB`
}

function isImageDocument(message: ArchivedMessage): boolean {
  return message.media?.mimetype?.startsWith("image/") === true
}

export function mediaViewer(deps: MediaViewerDeps): MediaViewer {
  const { vision, waitMs = WAIT_MS } = deps

  function preview(message: ArchivedMessage): ShownImage | null {
    const bytes = deps.thumbnail(message)
    return bytes ? { bytes, mimetype: "image/jpeg", source: "thumbnail" } : null
  }

  async function describe(
    message: ArchivedMessage,
    file: MediaFile,
    toText: Vision | string,
  ): Promise<Described> {
    function unavailable(error: unknown): Described {
      return { message, descriptionStatus: "unavailable", descriptionError: errorText(error) }
    }
    if (typeof toText === "string") return unavailable(toText)
    const job = toText(file).then((text) => {
      const description = tidyDescription(text)
      deps.saveDescription(message, description)
      return description
    })
    deps.waitUntil(job.catch(() => undefined))
    const described = job.then(
      (description): Described => ({ message: { ...message, description } }),
      unavailable,
    )
    return withDeadline(described, waitMs, { message, descriptionStatus: "pending" })
  }

  async function download(message: ArchivedMessage): Promise<MediaFile> {
    const size = message.media?.bytes ?? 0
    if (size > MAX_DOWNLOAD_BYTES) throw new Error(`the file is too large (${megabytes(size)})`)
    return deps.fetchMedia(message)
  }

  async function viewImage(message: ArchivedMessage, describable: boolean): Promise<MediaView> {
    let file: MediaFile
    try {
      file = await download(message)
    } catch (error) {
      return {
        message,
        image: preview(message),
        notice: `the original image is unavailable: ${errorText(error)}`,
      }
    }
    const described =
      describable && message.description === null && vision !== null
        ? await describe(message, file, vision)
        : { message }
    if (file.bytes.length <= MAX_SHOWN_BYTES) {
      return { ...described, image: { ...file, source: "original" } }
    }
    return {
      ...described,
      image: preview(message),
      notice: `the image is too large to show (${megabytes(file.bytes.length)})`,
    }
  }

  return {
    async view(message) {
      if (message.deleted) throw new Error("the message was deleted")
      switch (message.kind) {
        case "voice":
        case "audio": {
          const [read] = await deps.transcripts.read([message], true)
          return { message: read ?? message, image: null }
        }
        case "video":
        case "video_note": {
          const image = preview(message)
          if (image) return { message, image }
          return { message, image: null, notice: "the video has no preview" }
        }
        case "image":
          return viewImage(message, true)
        case "sticker":
          return viewImage(message, false)
        case "document":
          if (isImageDocument(message)) return viewImage(message, true)
          throw new Error("only image documents can be viewed")
        default:
          throw new Error(`a ${message.kind} message has no media to view`)
      }
    },
  }
}

// Vision models sometimes repeat a line until they run out of tokens.
export function tidyDescription(text: string): string {
  const lines: string[] = []
  for (const line of text.trim().split("\n")) {
    if (line.trim() === "" || line !== lines.at(-1)) lines.push(line)
  }
  return lines.join("\n").replace(/\n{3,}/g, "\n\n")
}

const MISTRAL = "@cf/mistralai/mistral-small-3.1-24b-instruct"
const SCOUT = "@cf/meta/llama-4-scout-17b-16e-instruct"
// Enough for a dense receipt or screenshot; a model stuck repeating stops here.
const MAX_TOKENS = 700

interface VisionInput {
  readonly messages: { role: string; content: string | object[] }[]
  readonly max_tokens: number
}

const VISION_MODELS = {
  [MISTRAL]: async (ai, input) => (await ai.run(MISTRAL, input)).response ?? "",
  [SCOUT]: async (ai, input) => (await ai.run(SCOUT, input)).response ?? "",
} satisfies Record<string, (ai: Ai, input: VisionInput) => Promise<string>>

export type VisionModel = keyof typeof VISION_MODELS

function isVisionModel(model: string): model is VisionModel {
  return Object.hasOwn(VISION_MODELS, model)
}

function languageName(code: string): string {
  try {
    return new Intl.DisplayNames(["en"], { type: "language" }).of(code) ?? code
  } catch {
    return code
  }
}

export function describePrompt(language: string | null): string {
  // Told to follow the language of the text in the image, the models still wrote English.
  const write = `Write in ${language ? languageName(language) : "English"}.`
  return [
    "You describe images sent on WhatsApp so they can be found by search later.",
    write,
    "Start with one or two sentences saying what the image is and what it shows.",
    "Then copy every readable piece of text exactly as written. No preamble, no markdown.",
  ].join(" ")
}

interface DescribeEnv {
  readonly DESCRIBE_MODEL?: string
  readonly DESCRIBE_LANGUAGE?: string
}

/** Workers AI image descriptions as configured, null when off, or a message that says what to fix. */
export function visionFromEnv(env: DescribeEnv & { readonly AI: Ai }): Vision | null | string {
  const model = env.DESCRIBE_MODEL?.trim() ?? ""
  if (model === "" || model === "off") return null
  if (!isVisionModel(model)) {
    return `DESCRIBE_MODEL must be one of ${Object.keys(VISION_MODELS).join(", ")} or "off", not "${model}".`
  }
  const system = describePrompt(env.DESCRIBE_LANGUAGE?.trim() || null)
  return (image) =>
    VISION_MODELS[model](env.AI, {
      messages: [
        { role: "system", content: system },
        {
          role: "user",
          content: [
            { type: "text", text: "Describe this image." },
            {
              type: "image_url",
              image_url: { url: `data:${image.mimetype};base64,${toBase64(image.bytes)}` },
            },
          ],
        },
      ],
      max_tokens: MAX_TOKENS,
    })
}
