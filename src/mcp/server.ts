/**
 * The MCP surface of one WhatsApp account. Each request builds a fresh server
 * (the handler is stateless); every tool is a thin call into the account's
 * Durable Object plus a compact, model-friendly rendering of the result.
 */
import { McpServer } from "@modelcontextprotocol/server"
import { z } from "zod"

import type { WhatsAppAccount } from "../account"
import type { ArchivedMessage, ChatSummary, SearchHit } from "../store/archive"
import type { Identity } from "../store/names"
import { formatLocal, parseLocal } from "../time"

type Account = DurableObjectStub<WhatsAppAccount>

const INSTRUCTIONS = `Read and act on one WhatsApp account.

Chats and people are identified by JID: "<id>@lid" or "<number>@s.whatsapp.net" for people (the same person can have both; results always use one), "<id>@g.us" for groups. Tools that take a chat or person also accept a phone number with country code. When you only have a name, call find_chats or find_people first.

Reading comes from the account's archive, which holds what WhatsApp synced at pairing plus everything since. If a chat's history stops too early, load_older_messages asks the phone for more.

Times are shown in the account's time zone as "YYYY-MM-DD HH:mm". Date filters accept the same format, a bare date, or ISO 8601 with an offset.

Sending is visible to other people and cannot be taken back silently: confirm intent before send_message, edit_message, delete_message or remove_contact unless the user was explicit.`

/** Drops absent fields, which only cost the model tokens. */
function withoutNulls<V>(record: Record<string, V | null>): Record<string, V> {
  const out: Record<string, V> = {}
  for (const [key, value] of Object.entries(record)) if (value !== null) out[key] = value
  return out
}

/** Writes `@<number>` mentions as `@<name>`, as the WhatsApp app shows them. */
function withMentionNames(text: string, mentions: ArchivedMessage["mentions"]): string {
  let out = text
  for (const mention of mentions) {
    if (mention.name) out = out.replaceAll(`@${mention.user}`, `@${mention.name}`)
  }
  return out
}

/** A message as the model sees it: no nulls, local time, sender resolved. */
function presentMessage(
  message: ArchivedMessage,
  timeZone: string,
): Record<string, string | number | boolean | object> {
  const out: Record<string, string | number | boolean | object> = {
    id: message.id,
    at: formatLocal(message.sentAt, timeZone),
    from: message.fromMe ? "me" : (message.senderName ?? message.senderJid ?? message.chatJid),
  }
  if (!message.fromMe && message.senderJid && message.senderName) out.from_jid = message.senderJid
  if (message.kind !== "text") out.kind = message.kind
  if (message.forwarded) out.forwarded = true
  if (message.text) out.text = withMentionNames(message.text, message.mentions)
  if (message.media) {
    const { mimetype, fileName, bytes, seconds } = message.media
    out.media = withoutNulls({ mimetype, fileName, bytes, seconds })
  }
  if (message.quotedId) out.reply_to = message.quotedId
  if (message.reactions.length > 0)
    out.reactions = message.reactions.map((reaction) => reaction.emoji).join("")
  if (message.edited) out.edited = true
  if (message.deleted) out.deleted = true
  return out
}

function presentChat(
  chat: ChatSummary & { readonly matched?: string | null },
  timeZone: string,
): Record<string, string | number | boolean> {
  const out: Record<string, string | number | boolean> = {
    jid: chat.jid,
    name: chat.name ?? chat.jid,
    type: chat.type,
  }
  if (chat.matched) out.matched = chat.matched
  if (chat.phone && chat.phone !== chat.name) out.phone = chat.phone
  if (chat.lastMessageAt) out.last_message_at = formatLocal(chat.lastMessageAt, timeZone)
  if (chat.unreadCount > 0) out.unread = chat.unreadCount
  if (chat.pinned) out.pinned = true
  if (chat.archived) out.archived = true
  return out
}

function presentHit(
  hit: SearchHit,
  timeZone: string,
): Record<string, string | number | boolean | object> {
  return {
    chat: hit.chatName ?? hit.chatJid,
    chat_jid: hit.chatJid,
    ...presentMessage(hit, timeZone),
    match: hit.snippet,
  }
}

function json(value: object): { content: { type: "text"; text: string }[] } {
  return { content: [{ type: "text", text: JSON.stringify(value) }] }
}

const chat = z.string().min(1).describe("Chat JID, or a phone number with country code")
const messageId = z.string().min(1).describe("Message id, as returned by the reading tools")
const when = z.string().min(1)
const PHONE = /^\+?\d{8,15}$/
const phoneNumber = z.string().regex(PHONE, "digits with country code, e.g. +5511999999999")

const personFields = {
  jid: z.string().min(1).optional().describe("The person's JID, @lid or @s.whatsapp.net"),
  phone: phoneNumber.optional().describe("The person's phone number with country code"),
}
const ONE_PERSON = { message: "pass either jid or phone, not both" }

function onePerson(input: {
  readonly jid?: string | undefined
  readonly phone?: string | undefined
}): boolean {
  return (input.jid === undefined) !== (input.phone === undefined)
}

function targetOf(input: {
  readonly jid?: string | undefined
  readonly phone?: string | undefined
}): string {
  const target = input.jid ?? input.phone
  if (target === undefined) throw new Error(ONE_PERSON.message)
  return target
}

function presentPerson(
  person: Identity & { readonly matched?: string | null },
): Record<string, string | readonly string[]> {
  return withoutNulls({
    jid: person.jid,
    name: person.name,
    phone: person.phone,
    matched: person.matched ?? null,
    also_known_as: person.aliases.length > 0 ? person.aliases : null,
  })
}

const READ = { readOnlyHint: true, openWorldHint: false } as const
const LIVE_READ = { readOnlyHint: true, openWorldHint: true } as const

export function createServer(account: Account, timeZone: string): McpServer {
  const server = new McpServer(
    { name: "whatsapp-mcp", version: "2.0.0" },
    { instructions: INSTRUCTIONS },
  )
  const toUnix = (value: string | undefined) =>
    value === undefined ? undefined : parseLocal(value, timeZone)

  server.registerTool(
    "get_status",
    {
      title: "Account status",
      description:
        "Connection state, the account's own JID and name, the time zone, and how much the archive holds (chats, contacts, messages, date range).",
      inputSchema: z.object({}),
      annotations: READ,
    },
    async () => {
      const overview = await account.overview()
      const { oldestMessageAt, newestMessageAt, ...counts } = overview.archive
      return json({
        state: overview.state,
        me: overview.me,
        name: overview.myName,
        time_zone: timeZone,
        last_error: overview.lastError,
        archive: {
          ...counts,
          oldest: oldestMessageAt ? formatLocal(oldestMessageAt, timeZone) : null,
          newest: newestMessageAt ? formatLocal(newestMessageAt, timeZone) : null,
        },
      })
    },
  )

  server.registerTool(
    "list_chats",
    {
      title: "List chats",
      description: "Chats ordered like the phone does: pinned first, then by latest activity.",
      inputSchema: z.object({
        limit: z.number().int().min(1).max(200).default(30),
        offset: z.number().int().min(0).default(0),
        unread_only: z.boolean().default(false),
        type: z.enum(["all", "groups", "direct"]).default("all"),
        include_archived: z.boolean().default(false),
      }),
      annotations: READ,
    },
    async (input) => {
      const chats = await account.listChats({
        limit: input.limit,
        offset: input.offset,
        unreadOnly: input.unread_only,
        type: input.type,
        includeArchived: input.include_archived,
      })
      return json({ chats: chats.map((entry) => presentChat(entry, timeZone)) })
    },
  )

  server.registerTool(
    "find_chats",
    {
      title: "Find chats",
      description:
        "Chats any of whose names (contact, WhatsApp, group subject, past names) or phone matches the text, accents and case ignored, most recently active first. `matched` shows the name that matched when it is not the display name.",
      inputSchema: z.object({
        query: z.string().min(1),
        limit: z.number().int().min(1).max(100).default(20),
      }),
      annotations: READ,
    },
    async ({ query, limit }) => {
      const chats = await account.findChats(query, limit)
      return json({ chats: chats.map((entry) => presentChat(entry, timeZone)) })
    },
  )

  server.registerTool(
    "find_people",
    {
      title: "Find people",
      description:
        "People any of whose names (saved contact name, WhatsApp name, business name, username, past names) or phone matches the text, accents and case ignored, including people you have no chat with (e.g. fellow group members). `matched` shows the name that matched when it is not the display name.",
      inputSchema: z.object({
        query: z.string().min(1),
        limit: z.number().int().min(1).max(100).default(20),
      }),
      annotations: READ,
    },
    async ({ query, limit }) => {
      const people = await account.findPeople(query, limit)
      return json({
        people: people.map(presentPerson),
      })
    },
  )

  server.registerTool(
    "get_messages",
    {
      title: "Read a chat",
      description:
        "The latest messages of a chat, oldest first. Page back with `before` set to the oldest `at` you have. Filter by sender in groups with `from`.",
      inputSchema: z.object({
        chat,
        limit: z.number().int().min(1).max(200).default(50),
        before: when.optional().describe("Only messages before this time"),
        after: when.optional().describe("Only messages after this time"),
        from: z.string().min(1).optional().describe("Sender JID (see `from_jid` in results)"),
      }),
      annotations: READ,
    },
    async (input) => {
      const messages = await account.getMessages({
        chatJid: input.chat,
        limit: input.limit,
        before: toUnix(input.before),
        after: toUnix(input.after),
        senderJid: input.from,
      })
      return json({ messages: messages.map((message) => presentMessage(message, timeZone)) })
    },
  )

  server.registerTool(
    "get_message_context",
    {
      title: "Message in context",
      description:
        "A message with the messages around it, e.g. to read the conversation behind a search hit.",
      inputSchema: z.object({
        chat,
        message_id: messageId,
        around: z.number().int().min(1).max(50).default(8),
      }),
      annotations: READ,
    },
    async (input) => {
      const messages = await account.getMessageContext(input.chat, input.message_id, input.around)
      return json({ messages: messages.map((message) => presentMessage(message, timeZone)) })
    },
  )

  server.registerTool(
    "search_messages",
    {
      title: "Search messages",
      description:
        "Full-text search across all chats, best matches first. Every word must appear (prefix match, accents and case ignored). Narrow with `chat`, `from`, `after`, `before`.",
      inputSchema: z.object({
        query: z.string().min(1),
        chat: chat.optional(),
        from: z.string().min(1).optional().describe("Sender JID"),
        after: when.optional(),
        before: when.optional(),
        limit: z.number().int().min(1).max(100).default(25),
      }),
      annotations: READ,
    },
    async (input) => {
      const hits = await account.searchMessages({
        query: input.query,
        chatJid: input.chat,
        senderJid: input.from,
        after: toUnix(input.after),
        before: toUnix(input.before),
        limit: input.limit,
      })
      return json({ results: hits.map((hit) => presentHit(hit, timeZone)) })
    },
  )

  server.registerTool(
    "send_message",
    {
      title: "Send a message",
      description:
        "Sends a text message. Retries are safe: an identical request (same chat, text, reply and mentions) within two minutes, or with the same `idempotency_key` within a day, returns the first message instead of sending again.",
      inputSchema: z.object({
        to: chat,
        text: z.string().min(1).max(65_536),
        reply_to: messageId.optional().describe("Quote this message"),
        mentions: z
          .array(z.string().min(1))
          .max(50)
          .optional()
          .describe("JIDs or numbers to @mention in a group; also write @<number> in the text"),
        idempotency_key: z.string().min(1).max(200).optional(),
      }),
      annotations: {
        readOnlyHint: false,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: true,
      },
    },
    async (input) =>
      json(
        await account.send({
          to: input.to,
          text: input.text,
          replyTo: input.reply_to,
          mentions: input.mentions,
          idempotencyKey: input.idempotency_key,
        }),
      ),
  )

  server.registerTool(
    "react_to_message",
    {
      title: "React to a message",
      description:
        "Adds an emoji reaction to a message, replacing this account's previous one. An empty emoji removes it.",
      inputSchema: z.object({ chat, message_id: messageId, emoji: z.string().max(16) }),
      annotations: {
        readOnlyHint: false,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: true,
      },
    },
    async (input) => {
      await account.react(input.chat, input.message_id, input.emoji)
      return json({ ok: true })
    },
  )

  server.registerTool(
    "edit_message",
    {
      title: "Edit a message",
      description:
        "Replaces the text of a message this account sent. WhatsApp only allows it for about 15 minutes after sending.",
      inputSchema: z.object({ chat, message_id: messageId, text: z.string().min(1).max(65_536) }),
      annotations: {
        readOnlyHint: false,
        destructiveHint: true,
        idempotentHint: true,
        openWorldHint: true,
      },
    },
    async (input) => {
      await account.editMessage(input.chat, input.message_id, input.text)
      return json({ ok: true })
    },
  )

  server.registerTool(
    "delete_message",
    {
      title: "Delete a message for everyone",
      description:
        "Revokes a message for everyone. Works on this account's messages, and on others' in groups this account administers.",
      inputSchema: z.object({ chat, message_id: messageId }),
      annotations: {
        readOnlyHint: false,
        destructiveHint: true,
        idempotentHint: true,
        openWorldHint: true,
      },
    },
    async (input) => {
      await account.deleteMessage(input.chat, input.message_id)
      return json({ ok: true })
    },
  )

  server.registerTool(
    "mark_as_read",
    {
      title: "Mark a chat as read",
      description:
        "Marks a chat read up to its latest message (blue ticks for the sender, when they are enabled).",
      inputSchema: z.object({ chat }),
      annotations: {
        readOnlyHint: false,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: true,
      },
    },
    async (input) => json({ marked: await account.markRead(input.chat) }),
  )

  server.registerTool(
    "save_contact",
    {
      title: "Save a contact",
      description:
        "Saves a person to this account's address book, or renames them if they are already saved. WhatsApp syncs it to the phone and the other linked devices. Pass the person's JID or phone number; first_name defaults to the first word of full_name.",
      inputSchema: z
        .object({
          ...personFields,
          full_name: z.string().trim().min(1).max(100),
          first_name: z.string().trim().min(1).max(100).optional(),
        })
        .refine(onePerson, ONE_PERSON),
      annotations: {
        readOnlyHint: false,
        destructiveHint: false,
        idempotentHint: true,
        openWorldHint: true,
      },
    },
    async (input) => {
      const saved = await account.saveContact(targetOf(input), {
        fullName: input.full_name,
        firstName: input.first_name,
      })
      return json(presentPerson(saved))
    },
  )

  server.registerTool(
    "remove_contact",
    {
      title: "Remove a contact",
      description:
        "Removes a person from this account's address book, on the phone and the other linked devices. Their chat and messages stay, shown under the name they chose on WhatsApp or their number.",
      inputSchema: z.object(personFields).refine(onePerson, ONE_PERSON),
      annotations: {
        readOnlyHint: false,
        destructiveHint: true,
        idempotentHint: true,
        openWorldHint: true,
      },
    },
    async (input) => json(presentPerson(await account.removeContact(targetOf(input)))),
  )

  server.registerTool(
    "check_numbers",
    {
      title: "Check numbers on WhatsApp",
      description: "Which phone numbers have a WhatsApp account. Nothing is sent to them.",
      inputSchema: z.object({
        phones: z.array(phoneNumber).min(1).max(50),
      }),
      annotations: LIVE_READ,
    },
    async ({ phones }) =>
      json({ results: await account.checkNumbers(phones.map((phone) => phone.replace("+", ""))) }),
  )

  server.registerTool(
    "get_group_info",
    {
      title: "Group details",
      description:
        "Live group metadata: subject, description, owner, creation date, settings and participants with admin roles.",
      inputSchema: z.object({ group: z.string().regex(/@g\.us$/, "a group JID ending in @g.us") }),
      annotations: LIVE_READ,
    },
    async ({ group }) => {
      const info = await account.getGroupInfo(group)
      return json({
        ...info,
        createdAt: info.createdAt ? formatLocal(info.createdAt, timeZone) : null,
      })
    },
  )

  server.registerTool(
    "get_profile",
    {
      title: "Profile",
      description:
        "A person's name as known here, their About text and profile picture URL, when their privacy settings allow it.",
      inputSchema: z.object({ jid: chat }),
      annotations: LIVE_READ,
    },
    async ({ jid }) => json(await account.getProfile(jid)),
  )

  server.registerTool(
    "load_older_messages",
    {
      title: "Load older messages",
      description:
        "Asks the phone for messages older than the oldest one archived for a chat, waits up to 20 seconds, and reports how many arrived. The phone must be online. Read them with get_messages.",
      inputSchema: z.object({ chat, count: z.number().int().min(1).max(200).default(50) }),
      annotations: {
        readOnlyHint: false,
        destructiveHint: false,
        idempotentHint: false,
        openWorldHint: true,
      },
    },
    async (input) => json({ loaded: await account.loadOlderMessages(input.chat, input.count) }),
  )

  return server
}
