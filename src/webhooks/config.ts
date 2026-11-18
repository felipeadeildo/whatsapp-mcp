// WEBHOOK_TOOLS decides what MCP clients can do with webhooks. The REST API and the
// pairing page always manage them, behind SECRET.
import { z } from "zod"

const WebhookTools = z
  .string()
  .trim()
  .toLowerCase()
  .pipe(z.enum(["manage", "read", "off"]))
export type WebhookTools = z.output<typeof WebhookTools>

/** The mode, `manage` when unset, or a message that says what to fix. */
export function readWebhookTools(value: string | undefined): WebhookTools | { error: string } {
  if (value === undefined || value.trim() === "") return "manage"
  const mode = WebhookTools.safeParse(value)
  return mode.success
    ? mode.data
    : { error: `WEBHOOK_TOOLS must be manage, read or off, not "${value}".` }
}
