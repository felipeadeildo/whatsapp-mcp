// What a thrown value says, for messages shown to the assistant.
export function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error)
}
