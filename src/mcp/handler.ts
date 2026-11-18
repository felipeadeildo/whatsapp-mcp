import { createMcpHandler } from "agents/mcp/server"

import { readWebhookTools } from "../webhooks/config"
import { createServer } from "./server"

export function serveMcp(
  request: Request,
  env: Env,
  ctx: ExecutionContext,
  accountId: string,
): Promise<Response> {
  const webhookTools = readWebhookTools(env.WEBHOOK_TOOLS)
  if (typeof webhookTools !== "string") {
    return Promise.resolve(new Response(webhookTools.error, { status: 500 }))
  }
  const account = env.ACCOUNT.getByName(accountId)
  const route = new URL(request.url).pathname
  const options = { accountId, timeZone: env.TIMEZONE, webhookTools }
  return createMcpHandler(() => createServer(account, options), { route })(request, env, ctx)
}
