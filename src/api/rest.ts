// The REST API for programs that act on the account without an assistant, such as
// bots. It runs inside the account's Durable Object, so a refusal keeps its status
// and `code`. JSON follows the webhooks: snake_case, nulls kept, ISO 8601 UTC times.
import { isBoom } from "@oxidezap/baileyrs/lib/Utils/boom.js"
import { z } from "zod"

import type {
  GroupInfo,
  Me,
  MessageData,
  MessagesData,
  Presence,
  SendRequest,
  SendResult,
} from "../account"
import { errorText, RequestError } from "../errors"
import type { ChatListQuery, ChatSummary, MessageQuery } from "../store/archive"
import { isoTime } from "../webhooks/events"

export interface ApiAccount {
  me(): Me
  send(request: SendRequest): Promise<SendResult>
  editMessage(chatJid: string, messageId: string, text: string): Promise<string>
  deleteMessage(chatJid: string, messageId: string): Promise<void>
  readMessage(chatJid: string, id: string, transcribeVoice: boolean): Promise<MessageData | null>
  readMessages(query: MessageQuery, transcribeVoice: boolean): Promise<MessagesData>
  listChats(query: ChatListQuery): ChatSummary[]
  markRead(chatJid: string): Promise<number>
  setPresence(chatJid: string, state: Presence): Promise<void>
  getGroupInfo(groupJid: string): Promise<GroupInfo>
}

const flag = z
  .enum(["true", "false"])
  .transform((value) => value === "true")
  .optional()

const time = z
  .string()
  .refine(
    (value) => !Number.isNaN(Date.parse(value)),
    "an ISO 8601 time, like 2026-11-19T00:00:00-03:00",
  )
  .transform((value) => Math.floor(Date.parse(value) / 1000))
  .optional()

const SendBody = z.object({
  to: z.string().min(1),
  text: z.string().min(1).max(65_536),
  reply_to: z.string().min(1).optional(),
  mentions: z.array(z.string().min(1)).max(256).optional(),
  idempotency_key: z.string().min(1).max(200).optional(),
})

const EditBody = z.object({ text: z.string().min(1).max(65_536) })

const PresenceBody = z.object({ state: z.enum(["typing", "recording", "paused"]) })

const ChatsQuery = z.object({
  type: z.enum(["group", "direct"]).optional(),
  unread: flag,
  include_archived: flag,
  limit: z.coerce.number().int().min(1).max(1000).default(200),
  offset: z.coerce.number().int().min(0).default(0),
})

const MessagesQuery = z.object({
  since: time,
  until: time,
  sender: z.string().min(1).optional(),
  limit: z.coerce.number().int().min(1).max(1000).default(100),
  transcribe_voice: flag,
})

const MessageQueryParams = z.object({ transcribe_voice: flag })

type Route = (account: ApiAccount, request: Request, url: URL) => Promise<Response>

export async function handleApi(request: Request, account: ApiAccount): Promise<Response> {
  const url = new URL(request.url)
  try {
    const segments = url.pathname
      .split("/")
      .filter((segment) => segment.length > 0)
      .map((segment) => decodeURIComponent(segment))
    const routes = routesFor(segments)
    if (!routes) throw new RequestError(404, "not_found", `no route ${url.pathname}`)
    const route = routes[request.method]
    if (!route) {
      throw new RequestError(405, "invalid_request", `${request.method} is not allowed here`)
    }
    return await route(account, request, url)
  } catch (error) {
    return errorResponse(error)
  }
}

// The handlers of a path, by HTTP method, or null when no resource has that path.
function routesFor(segments: readonly string[]): Record<string, Route> | null {
  const [resource, a, b] = segments
  switch (resource) {
    case "me":
      if (segments.length !== 1) return null
      return { GET: async (account) => Response.json(presentMe(account.me())) }
    case "messages":
      if (segments.length === 1) return { POST: sendMessage }
      if (segments.length !== 3 || !a || !b) return null
      return {
        GET: (account, _request, url) => getMessage(account, a, b, url),
        PATCH: (account, request) => editMessage(account, a, b, request),
        DELETE: (account) => deleteMessage(account, a, b),
      }
    case "chats":
      if (segments.length === 1) return { GET: listChats }
      if (segments.length !== 3 || !a) return null
      switch (b) {
        case "messages":
          return { GET: (account, _request, url) => listMessages(account, a, url) }
        case "presence":
          return { POST: (account, request) => setPresence(account, a, request) }
        case "read":
          return { POST: (account) => markRead(account, a) }
        default:
          return null
      }
    case "groups":
      if (segments.length !== 2 || !a) return null
      return { GET: (account) => getGroup(account, a) }
    default:
      return null
  }
}

async function parseBody<T extends z.ZodType>(request: Request, schema: T): Promise<z.output<T>> {
  const body = schema.safeParse(await request.json().catch(() => null))
  if (!body.success) throw new RequestError(400, "invalid_request", z.prettifyError(body.error))
  return body.data
}

function parseQuery<T extends z.ZodType>(url: URL, schema: T): z.output<T> {
  const query = schema.safeParse(Object.fromEntries(url.searchParams))
  if (!query.success) throw new RequestError(400, "invalid_request", z.prettifyError(query.error))
  return query.data
}

function presentMe(me: Me): Record<string, unknown> {
  return { jid: me.jid, lid: me.lid, phone: me.phone, name: me.name, state: me.state }
}

async function sendMessage(account: ApiAccount, request: Request): Promise<Response> {
  const body = await parseBody(request, SendBody)
  const sent = await account.send({
    to: body.to,
    text: body.text,
    replyTo: body.reply_to,
    mentions: body.mentions,
    idempotencyKey: body.idempotency_key,
  })
  return Response.json(
    { chat_jid: sent.chatJid, id: sent.id, deduplicated: sent.deduplicated },
    { status: sent.deduplicated ? 200 : 201 },
  )
}

async function getMessage(
  account: ApiAccount,
  chat: string,
  id: string,
  url: URL,
): Promise<Response> {
  const query = parseQuery(url, MessageQueryParams)
  const found = await account.readMessage(chat, id, query.transcribe_voice ?? false)
  if (!found) throw new RequestError(404, "message_not_found", `message ${id} not found in ${chat}`)
  return Response.json(found)
}

async function editMessage(
  account: ApiAccount,
  chat: string,
  id: string,
  request: Request,
): Promise<Response> {
  const body = await parseBody(request, EditBody)
  const jid = await account.editMessage(chat, id, body.text)
  return Response.json({ chat_jid: jid, id })
}

async function deleteMessage(account: ApiAccount, chat: string, id: string): Promise<Response> {
  await account.deleteMessage(chat, id)
  return new Response(null, { status: 204 })
}

async function listChats(account: ApiAccount, _request: Request, url: URL): Promise<Response> {
  const query = parseQuery(url, ChatsQuery)
  const chats = account.listChats({
    type: query.type === "group" ? "groups" : (query.type ?? "all"),
    unreadOnly: query.unread ?? false,
    includeArchived: query.include_archived ?? true,
    limit: query.limit,
    offset: query.offset,
  })
  return Response.json({
    chats: chats.map((chat) => ({
      jid: chat.jid,
      name: chat.name,
      type: chat.type,
      phone: chat.phone,
      unread_count: chat.unreadCount,
      last_message_at: chat.lastMessageAt === null ? null : isoTime(chat.lastMessageAt * 1000),
      archived: chat.archived,
      pinned: chat.pinned,
    })),
  })
}

// The newest `limit` messages in [since, until), oldest first.
async function listMessages(account: ApiAccount, chat: string, url: URL): Promise<Response> {
  const query = parseQuery(url, MessagesQuery)
  const found = await account.readMessages(
    {
      chatJid: chat,
      limit: query.limit,
      after: query.since === undefined ? undefined : query.since - 1,
      before: query.until,
      senderJid: query.sender,
    },
    query.transcribe_voice ?? false,
  )
  return Response.json(found)
}

async function setPresence(account: ApiAccount, chat: string, request: Request): Promise<Response> {
  const body = await parseBody(request, PresenceBody)
  await account.setPresence(chat, body.state)
  return new Response(null, { status: 204 })
}

async function markRead(account: ApiAccount, chat: string): Promise<Response> {
  return Response.json({ marked: await account.markRead(chat) })
}

async function getGroup(account: ApiAccount, jid: string): Promise<Response> {
  const group = await account.getGroupInfo(jid)
  return Response.json({
    group: {
      jid: group.jid,
      name: group.subject,
      description: group.description,
      owner: group.owner,
      created_at: group.createdAt === null ? null : isoTime(group.createdAt * 1000),
      size: group.size,
      announce_only: group.announceOnly,
      restricted: group.restricted,
      addressing_mode: group.addressingMode,
      participants: group.participants.map((participant) => ({
        jid: participant.jid,
        phone: participant.phone,
        name: participant.name,
        admin: participant.admin,
      })),
    },
  })
}

function errorResponse(error: unknown): Response {
  if (error instanceof RequestError) {
    return Response.json({ error: error.message, code: error.code }, { status: error.status })
  }
  if (isBoom(error)) {
    return Response.json({ error: errorText(error), code: "whatsapp_error" }, { status: 502 })
  }
  return Response.json({ error: errorText(error), code: "internal" }, { status: 500 })
}
