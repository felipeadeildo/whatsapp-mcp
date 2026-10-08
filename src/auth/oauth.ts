// Each account is its own authorization server, with issuer /accounts/{id} and
// resource /accounts/{id}/mcp, so a token for one account does not open another.
// Claude wants the resource to be the exact URL the user pasted, and we only know
// the host on the first request, so the servers are built then.
import {
  OAuthAuthorizationServer,
  OAuthResourceServer,
  insufficientScope,
} from "@cloudflare/workers-oauth-provider"

import { serveMcp } from "../mcp/handler"
import { type AuthConfig, allowsApiKey } from "./config"
import { sameSecret } from "./secret"

export const SCOPE = "whatsapp"

interface AccessProps {
  accountId: string
}

export interface AccountOAuth {
  authorization: OAuthAuthorizationServer<Env>
  resource: OAuthResourceServer<Env, AccessProps>
}

// Account ids come from the URL, so the cache needs a cap.
const MAX_CACHED = 64
const cache = new Map<string, AccountOAuth>()

export function accountOAuth(origin: string, accountId: string, config: AuthConfig): AccountOAuth {
  const key = `${origin} ${accountId}`
  let entry = cache.get(key)
  if (!entry) {
    if (cache.size >= MAX_CACHED) cache.clear()
    entry = build(origin, accountId, config)
    cache.set(key, entry)
  }
  return entry
}

function build(origin: string, accountId: string, config: AuthConfig): AccountOAuth {
  const issuer = `${origin}/accounts/${accountId}`
  const resourceUrl = `${issuer}/mcp`

  const authorization = new OAuthAuthorizationServer<Env>({
    issuer,
    resources: [resourceUrl],
    clientIdMetadataDocumentEnabled: true,
    clientRegistrationEndpoint: `/accounts/${accountId}/oauth/register`,
    scopesSupported: [SCOPE, "offline_access"],
    // A connector expires after 30 days without use.
    refreshTokenIdleTTL: 30 * 24 * 60 * 60,
  })

  const resource = new OAuthResourceServer<Env, AccessProps>({
    resourceMetadata: {
      resource: resourceUrl,
      authorization_servers: [issuer],
      bearer_methods_supported: ["header"],
      resource_name: `WhatsApp (${accountId})`,
    },
    requiredScopes: [SCOPE],
    validateToken: (env) => async (audience, token) => {
      if (allowsApiKey(config.mode) && (await sameSecret(token, config.secret))) {
        return { props: { accountId }, audience, scope: [SCOPE] }
      }
      return authorization.validateToken<AccessProps>(audience, token, env)
    },
    handler: {
      fetch(request, env, ctx) {
        if (!ctx.auth.scope.includes(SCOPE)) return insufficientScope(ctx.auth, [SCOPE])
        return serveMcp(request, env, ctx, accountId)
      },
    },
  })

  return { authorization, resource }
}
