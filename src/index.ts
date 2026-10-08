import { AuthorizationError, CimdFetchError } from "@cloudflare/workers-oauth-provider"
import { z } from "zod"

import { type AuthConfig, allowsApiKey, allowsOAuth, readAuthConfig } from "./auth/config"
import { type AccountOAuth, SCOPE, accountOAuth } from "./auth/oauth"
import { consentPage, failurePage, signInPage } from "./auth/pages"
import { bearerToken, sameSecret } from "./auth/secret"
import { hasSession, sessionCookie } from "./auth/session"
import { serveMcp } from "./mcp/handler"
import { pairingPage } from "./pairing-page"
import { requestLang } from "./text"

export { WhatsAppAccount } from "./account"

const SendBody = z.object({
  to: z.string().min(1),
  text: z.string().min(1),
  replyTo: z.string().min(1).optional(),
  idempotencyKey: z.string().min(1).max(200).optional(),
})

const ACCOUNT_ID = "[a-z0-9-]{1,64}"
const ACCOUNT_ROUTE = new RegExp(`^/accounts/(${ACCOUNT_ID})(?:/([a-z/]+))?$`)
const WELL_KNOWN_ROUTE = new RegExp(
  `^/\\.well-known/(oauth-protected-resource|oauth-authorization-server)/accounts/(${ACCOUNT_ID})(/mcp)?$`,
)

const REST_METHODS: Record<string, string> = {
  status: "GET",
  start: "POST",
  send: "POST",
  logout: "POST",
}

export default {
  async fetch(request, env, ctx): Promise<Response> {
    const config = readAuthConfig(env)
    if (typeof config === "string") return new Response(config, { status: 500 })
    const url = new URL(request.url)

    const wellKnown = WELL_KNOWN_ROUTE.exec(url.pathname)
    if (wellKnown) {
      const [, document, accountId = "", mcpSuffix] = wellKnown
      const isResource = document === "oauth-protected-resource"
      if (!allowsOAuth(config.mode) || isResource !== Boolean(mcpSuffix)) return notFound()
      const oauth = accountOAuth(url.origin, accountId, config)
      return (isResource ? oauth.resource : oauth.authorization).fetch(request, env, ctx)
    }

    const match = ACCOUNT_ROUTE.exec(url.pathname)
    if (!match) return notFound()
    const [, accountId = "", action = ""] = match
    const oauth = allowsOAuth(config.mode) ? accountOAuth(url.origin, accountId, config) : null

    switch (action) {
      case "":
        if (request.method !== "GET") return methodNotAllowed()
        if (!(await hasSession(request, config.secret))) {
          return html(signInPage({ lang: requestLang(request), accountId, next: url.pathname }))
        }
        return html(pairingPage(accountId, config.mode, requestLang(request)))
      case "login":
        return signIn(request, env, config, accountId)
      case "mcp":
        if (oauth) return oauth.resource.fetch(request, env, ctx)
        if (!(await hasBearerSecret(request, config))) return unauthorized()
        return serveMcp(request, env, ctx, accountId)
      case "authorize":
        if (!oauth) return notFound()
        return authorize(request, env, config, accountId, oauth)
      case "oauth/token":
      case "oauth/register":
        if (!oauth) return notFound()
        return oauth.authorization.fetch(request, env, ctx)
      default:
        return rest(request, env, config, accountId, action)
    }
  },
} satisfies ExportedHandler<Env>

async function rest(
  request: Request,
  env: Env,
  config: AuthConfig,
  accountId: string,
  action: string,
): Promise<Response> {
  const expectedMethod = REST_METHODS[action]
  if (!expectedMethod) return notFound()
  const allowed =
    (await hasSession(request, config.secret)) || (await hasBearerSecret(request, config))
  if (!allowed) return unauthorized()
  if (request.method !== expectedMethod) return methodNotAllowed()

  const account = env.ACCOUNT.getByName(accountId)
  try {
    switch (action) {
      case "status":
        return Response.json(await account.overview())
      case "start":
        return Response.json(await account.start())
      case "logout":
        return Response.json(await account.logout())
      default: {
        const body = SendBody.safeParse(await request.json().catch(() => null))
        if (!body.success) {
          return Response.json({ error: z.prettifyError(body.error) }, { status: 400 })
        }
        return Response.json(await account.send(body.data))
      }
    }
  } catch (error) {
    return Response.json({ error: String(error) }, { status: 500 })
  }
}

async function hasBearerSecret(request: Request, config: AuthConfig): Promise<boolean> {
  const token = bearerToken(request)
  return allowsApiKey(config.mode) && token !== null && (await sameSecret(token, config.secret))
}

// The only form that takes the secret, so it is rate limited per IP.
async function signIn(
  request: Request,
  env: Env,
  config: AuthConfig,
  accountId: string,
): Promise<Response> {
  if (request.method !== "POST") return methodNotAllowed()
  const form = await request.formData()
  const next = accountPath(request, accountId, formText(form, "next"))
  const lang = requestLang(request)

  const ip = request.headers.get("cf-connecting-ip") ?? "unknown"
  const { success } = await env.SIGN_IN_LIMIT.limit({ key: ip })
  if (!success) return html(signInPage({ lang, accountId, next, error: "tooMany" }), 429)
  if (!(await sameSecret(formText(form, "secret"), config.secret))) {
    return html(signInPage({ lang, accountId, next, error: "wrong" }), 401)
  }
  return new Response(null, {
    status: 303,
    headers: { location: next, "set-cookie": await sessionCookie(config.secret) },
  })
}

// Only paths of this account, so the sign-in form cannot redirect to another site.
function accountPath(request: Request, accountId: string, next: string): string {
  const origin = new URL(request.url).origin
  const base = `/accounts/${accountId}`
  const target = new URL(next || base, origin)
  const inside = target.pathname === base || target.pathname.startsWith(`${base}/`)
  return target.origin === origin && inside ? target.pathname + target.search : base
}

async function authorize(
  request: Request,
  env: Env,
  config: AuthConfig,
  accountId: string,
  oauth: AccountOAuth,
): Promise<Response> {
  const lang = requestLang(request)
  const url = new URL(request.url)
  if (!(await hasSession(request, config.secret))) {
    return html(signInPage({ lang, accountId, next: url.pathname + url.search }))
  }
  const api = oauth.authorization.getOAuthApi(env)

  try {
    if (request.method === "GET") {
      const authRequest = await api.parseAuthRequest(request)
      const details = await api.describeConsent(authRequest)
      const consent = await api.beginConsent(authRequest)
      consent.headers.set("content-type", "text/html; charset=utf-8")
      const page = consentPage({ lang, accountId, details, handle: consent.handle })
      return new Response(page, { headers: consent.headers })
    }
    if (request.method !== "POST") return methodNotAllowed()

    const form = await request.formData()
    const handle = formText(form, "handle")
    if (form.get("decision") !== "approve") {
      const denied = await api.denyConsent(request, handle)
      return new Response(null, { status: 302, headers: denied.headers })
    }
    const approved = await api.approveConsent(request, handle, { scope: [SCOPE] })
    const { redirectTo } = await api.completeAuthorization({
      request: approved.request,
      userId: "owner",
      metadata: {},
      scope: [SCOPE],
      props: { accountId },
    })
    approved.headers.set("location", redirectTo)
    return new Response(null, { status: 302, headers: approved.headers })
  } catch (error) {
    if (error instanceof AuthorizationError) {
      // redirectTo is set only once the library has checked the client's redirect URI.
      if (error.redirectTo) return Response.redirect(error.redirectTo, 302)
      return html(failurePage(lang, error.description), 400)
    }
    if (error instanceof CimdFetchError) {
      return html(failurePage(lang, "This app could not be verified."), 400)
    }
    throw error
  }
}

function formText(form: FormData, name: string): string {
  const value = form.get(name)
  return typeof value === "string" ? value : ""
}

function html(body: string, status = 200): Response {
  return new Response(body, { status, headers: { "content-type": "text/html; charset=utf-8" } })
}

function notFound(): Response {
  return new Response("not found", { status: 404 })
}

function methodNotAllowed(): Response {
  return Response.json({ error: "method not allowed" }, { status: 405 })
}

function unauthorized(): Response {
  return Response.json({ error: "unauthorized" }, { status: 401 })
}
