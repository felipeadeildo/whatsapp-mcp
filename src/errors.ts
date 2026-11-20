// What a thrown value says, for messages shown to the assistant.
export function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}

import { isBoom } from "@oxidezap/baileyrs/lib/Utils/boom.js"

/**
 * The status WhatsApp answered with, like 404 for a group this number isn't in, or
 * null when the error didn't come from WhatsApp. The bridge throws errors named
 * `WhatsAppError` that carry `serverCode`; older paths throw a Boom.
 */
export function whatsappStatus(error: unknown): number | null {
  if (isBoom(error)) return error.output.statusCode
  if (error instanceof Error && error.name === "WhatsAppError") {
    const code: unknown = Reflect.get(error, "serverCode")
    return typeof code === "number" ? code : 0
  }
  return null
}

export type ErrorCode =
  | "invalid_request"
  | "invalid_address"
  | "not_found"
  | "message_not_found"
  | "not_own_message"
  | "edit_window_closed"
  | "not_connected"
  | "whatsapp_error"
  | "internal"

/** A refusal the REST API answers with its own status and a stable `code`. */
export class RequestError extends Error {
  override readonly name = "RequestError"

  constructor(
    readonly status: number,
    readonly code: ErrorCode,
    message: string,
  ) {
    super(message)
  }
}
