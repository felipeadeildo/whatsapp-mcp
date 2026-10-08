// SECRET is typed on the sign-in page in every mode. AUTH_MODE says whether MCP
// clients get in through OAuth, by sending SECRET as a Bearer token, or both.
import { z } from "zod"

const MIN_SECRET_LENGTH = 16

const AuthMode = z
  .string()
  .trim()
  .toLowerCase()
  .pipe(z.enum(["oauth", "api_key", "both"]))
export type AuthMode = z.output<typeof AuthMode>

export interface AuthConfig {
  mode: AuthMode
  secret: string
}

/** The configuration, or a message that says what to fix. */
export function readAuthConfig(env: Env): AuthConfig | string {
  const mode = AuthMode.safeParse(env.AUTH_MODE)
  if (!mode.success) return `AUTH_MODE must be oauth, api_key or both, not "${env.AUTH_MODE}".`
  if ((env.SECRET ?? "").length < MIN_SECRET_LENGTH) {
    return `SECRET must have at least ${MIN_SECRET_LENGTH} characters. Generate one with \`openssl rand -hex 32\`.`
  }
  return { mode: mode.data, secret: env.SECRET }
}

export function allowsOAuth(mode: AuthMode): boolean {
  return mode !== "api_key"
}

export function allowsApiKey(mode: AuthMode): boolean {
  return mode !== "oauth"
}
