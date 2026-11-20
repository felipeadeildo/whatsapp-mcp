// What a thrown value says, for messages shown to the assistant.
export function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
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
