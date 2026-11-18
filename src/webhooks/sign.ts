// Standard Webhooks (https://www.standardwebhooks.com): receivers verify with the
// library for their language. The signed content is "{id}.{timestamp}.{body}".
import { toBase64 } from "../bytes"

const PREFIX = "whsec_"
const SECRET_BYTES = 24

export function newSecret(): string {
  return PREFIX + toBase64(crypto.getRandomValues(new Uint8Array(SECRET_BYTES)))
}

function keyBytes(secret: string): Uint8Array<ArrayBuffer> {
  const encoded = secret.startsWith(PREFIX) ? secret.slice(PREFIX.length) : secret
  return Uint8Array.from(atob(encoded), (char) => char.charCodeAt(0))
}

async function sign(secret: string, content: string): Promise<string> {
  const key = await crypto.subtle.importKey(
    "raw",
    keyBytes(secret),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  )
  const mac = await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(content))
  return toBase64(new Uint8Array(mac))
}

/** The `webhook-signature` header: one `v1,<base64>` per secret, space separated. */
export async function signatureHeader(
  secrets: readonly string[],
  id: string,
  timestampSeconds: number,
  body: string,
): Promise<string> {
  const content = `${id}.${timestampSeconds}.${body}`
  const signatures = await Promise.all(
    secrets.map(async (secret) => `v1,${await sign(secret, content)}`),
  )
  return signatures.join(" ")
}
