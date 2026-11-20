// The REST API for programs that act on the account without an assistant, such as
// bots. It runs inside the account's Durable Object, so a refusal keeps its status
// and `code`. JSON follows the webhooks: snake_case, nulls kept, ISO 8601 UTC times.
import { isBoom } from "@oxidezap/baileyrs/lib/Utils/boom.js"
import { z } from "zod"

import type { GroupInfo, Me, Presence, SendRequest, SendResult } from "../account"
import { errorText, RequestError } from "../errors"
import type { ChatListQuery, ChatSummary, MessageQuery } from "../store/archive"
import type { ReadMessage } from "../transcribe"
import { isoTime } from "../webhooks/events"

export interface ApiAccount {
  me(): Me
  send(request: SendRequest): Promise<SendResult>
  editMessage(chatJid: string, messageId: string, text: string): Promise<void>
  deleteMessage(chatJid: string, messageId: string): Promise<void>
  getMessageContext(
    chatJid: string,
    id: string,
    around: number,
    transcribeVoice: boolean,
  ): Promise<ReadMessage[]>
  getMessages(query: MessageQuery, transcribeVoice: boolean): Promise<ReadMessage[]>
  listChats(query: ChatListQuery): ChatSummary[]
  markRead(chatJid: string): Promise<number>
  setPresence(chatJid: string, state: Presence): Promise<void>
  getGroupInfo(groupJid: string): Promise<GroupInfo>
  presentChat(jid: string): Record<string, unknown>
  presentMessages(messages: readonly ReadMessage[]): Record<string, unknown>[]
  resolve(to: string): string
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
    const route = routeOf(segments, request.method)
    if (route === "not_found") throw new RequestError(404, "not_found", `no route ${url.pathname}`)
    if (route === "method_not_allowed") {
      return Response.json(
        { error: `${request.method} is not allowed here`, code: "invalid_request" },
        { status: 405 },
      )
    }
    return await route(account, request, url)
  } catch (error) {
    return errorResponse(error)
  }
}

function routeOf(
  segments: readonly string[],
  method: string,
): Route | "not_found" | "method_not_allowed" {
  const [resource, a, b, c] = segments
  const pick = (routes: Record<string, Route>) => routes[method] ?? "method_not_allowed"
  switch (resource) {
    case "me":
      if (segments.length !== 1) return "not_found"
      return pick({ GET: async (account) => Response.json(presentMe(account.me())) })
    case "messages":
      if (segments.length === 1) return pick({ POST: sendMessage })
      if (segments.length === 3 && a && b) {
        return pick({
          GET: (account, _request, url) => getMessage(account, a, b, url),
          PATCH: (account, request) => editMessage(account, a, b, request),
          DELETE: async (account) => {
            await account.deleteMessage(a, b)
            return new Response(null, { status: 204 })
          },
        })
      }
      return "not_found"
    case "chats":
      if (segments.length === 1) return pick({ GET: listChats })
      if (segments.length === 3 && a) {
        if (b === "messages")
          return pick({ GET: (account, _r, url) => listMessages(account, a, url) })
        if (b === "presence")
          return pick({ POST: (account, request) => setPresence(account, a, request) })
        if (b === "read") {
          return pick({
            POST: async (account) => Response.json({ marked: await account.markRead(a) }),
          })
        }
      }
      return "not_found"
    case "groups":
      if (segments.length === 2 && a && c === undefined) {
        return pick({ GET: (account) => getGroup(account, a) })
      }
      return "not_found"
    default:
      return "not_found"
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
  const jid = account.resolve(chat)
  const [message] = await account.getMessageContext(jid, id, 0, query.transcribe_voice ?? false)
  if (!message)
    throw new RequestError(404, "message_not_found", `message ${id} not found in ${jid}`)
  const [data] = account.presentMessages([message])
  return Response.json({ chat: account.presentChat(jid), message: data })
}

async function editMessage(
  account: ApiAccount,
  chat: string,
  id: string,
  request: Request,
): Promise<Response> {
  const body = await parseBody(request, EditBody)
  const jid = account.resolve(chat)
  await account.editMessage(jid, id, body.text)
  return Response.json({ chat_jid: jid, id })
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
  const jid = account.resolve(chat)
  const messages = await account.getMessages(
    {
      chatJid: jid,
      limit: query.limit,
      after: query.since === undefined ? undefined : query.since - 1,
      before: query.until,
      senderJid: query.sender,
    },
    query.transcribe_voice ?? false,
  )
  return Response.json({
    chat: account.presentChat(jid),
    messages: account.presentMessages(messages),
  })
}

async function setPresence(account: ApiAccount, chat: string, request: Request): Promise<Response> {
  const body = await parseBody(request, PresenceBody)
  await account.setPresence(chat, body.state)
  return new Response(null, { status: 204 })
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
