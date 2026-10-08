/**
 * HTTP entry point. Routes `/accounts/:id/...` to the account's Durable Object:
 * `/mcp` is the MCP endpoint, the rest is a small REST API used by the pairing
 * page and by scripts.
 *
 * Every API route requires `Authorization: Bearer <API_KEY>`. The pairing page
 * itself is public but holds no data: it reads the key from the URL fragment,
 * which browsers never send to the server, and calls the API with it.
 */
import { createMcpHandler } from "agents/mcp/server"
import { z } from "zod"

import { createServer } from "./mcp/server"
import { pairingPage } from "./pairing-page"

export { WhatsAppAccount } from "./account"

const SendBody = z.object({
  to: z.string().min(1),
  text: z.string().min(1),
  replyTo: z.string().min(1).optional(),
  idempotencyKey: z.string().min(1).max(200).optional(),
})

const Action = z.enum(["mcp", "status", "start", "send", "logout"])

const ACCOUNT_ROUTE = new RegExp(`^/accounts/([a-z0-9-]{1,64})(?:/(${Action.options.join("|")}))?$`)

export default {
  async fetch(request, env, ctx): Promise<Response> {
    const url = new URL(request.url)
    const match = ACCOUNT_ROUTE.exec(url.pathname)
    if (!match) return new Response("not found", { status: 404 })
    const [, accountId = "", rawAction] = match

    if (!rawAction) {
      if (request.method !== "GET") return new Response("method not allowed", { status: 405 })
      return new Response(pairingPage(accountId), {
        headers: { "content-type": "text/html; charset=utf-8" },
      })
    }

    if (!(await isAuthorized(request, env.API_KEY))) {
      return Response.json({ error: "unauthorized" }, { status: 401 })
    }

    const action = Action.parse(rawAction)
    const account = env.ACCOUNT.getByName(accountId)
    if (action === "mcp") {
      const timeZone = env.TIMEZONE
      return createMcpHandler(() => createServer(account, timeZone), { route: url.pathname })(
        request,
        env,
        ctx,
      )
    }

    const expectedMethod = action === "status" ? "GET" : "POST"
    if (request.method !== expectedMethod) {
      return Response.json({ error: "method not allowed" }, { status: 405 })
    }

    try {
      switch (action) {
        case "status":
          return Response.json(await account.status())
        case "start":
          return Response.json(await account.start())
        case "logout":
          return Response.json(await account.logout())
        case "send": {
          const body = SendBody.safeParse(await request.json().catch(() => null))
          if (!body.success) {
            return Response.json({ error: z.prettifyError(body.error) }, { status: 400 })
          }
          return Response.json(await account.send(body.data))
        }
        default:
          return action satisfies never
      }
    } catch (error) {
      return Response.json({ error: String(error) }, { status: 500 })
    }
  },
} satisfies ExportedHandler<Env>

async function isAuthorized(request: Request, apiKey: string): Promise<boolean> {
  const header = request.headers.get("authorization") ?? ""
  const encoder = new TextEncoder()
  // Hash both sides so the comparison runs on equal-length buffers.
  const [given, expected] = await Promise.all([
    crypto.subtle.digest("SHA-256", encoder.encode(header)),
    crypto.subtle.digest("SHA-256", encoder.encode(`Bearer ${apiKey}`)),
  ])
  return crypto.subtle.timingSafeEqual(given, expected)
}
