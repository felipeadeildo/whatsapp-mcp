// The cookie holds its expiry and an HMAC of it keyed by SECRET. Changing the
// secret signs every browser out, and the server keeps no session table.
import { sameSecret, sign } from "./secret"

const COOKIE = "__Host-whatsapp-mcp-session"
const LIFETIME_SECONDS = 12 * 60 * 60

export async function sessionCookie(secret: string): Promise<string> {
  const expiresAt = Math.floor(Date.now() / 1000) + LIFETIME_SECONDS
  const value = `${expiresAt}.${await sign(secret, `session:${expiresAt}`)}`
  // Lax so the cookie still arrives when an AI app sends the browser here from its own site.
  return `${COOKIE}=${value}; Path=/; Max-Age=${LIFETIME_SECONDS}; Secure; HttpOnly; SameSite=Lax`
}

export async function hasSession(request: Request, secret: string): Promise<boolean> {
  const value = readCookie(request, COOKIE)
  if (!value) return false
  const [expiresAt = "", signature = ""] = value.split(".")
  if (!/^\d+$/.test(expiresAt) || Number(expiresAt) * 1000 < Date.now()) return false
  return sameSecret(signature, await sign(secret, `session:${expiresAt}`))
}

function readCookie(request: Request, name: string): string | null {
  for (const part of (request.headers.get("cookie") ?? "").split(";")) {
    const [key, ...value] = part.trim().split("=")
    if (key === name) return value.join("=")
  }
  return null
}
