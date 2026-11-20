import { describe, expect, test } from "bun:test"
import { z } from "zod"

import { type ApiAccount, handleApi } from "../src/api/rest"
import { RequestError } from "../src/errors"

const GROUP = "120363000000000001@g.us"

function fakeAccount(overrides: Partial<ApiAccount> = {}) {
  const calls: { method: string; args: unknown[] }[] = []
  const record =
    <T>(method: string, result: T) =>
    (...args: unknown[]) => {
      calls.push({ method, args })
      return result
    }
  const account: ApiAccount = {
    me: record("me", {
      jid: "5511000000000@s.whatsapp.net",
      lid: "42@lid",
      phone: "+5511000000000",
      name: "Bob",
      state: "open",
    }),
    send: record("send", Promise.resolve({ chatJid: GROUP, id: "SENT", deduplicated: false })),
    editMessage: record("editMessage", Promise.resolve(GROUP)),
    deleteMessage: record("deleteMessage", Promise.resolve()),
    readMessage: record(
      "readMessage",
      Promise.resolve({ chat: { jid: GROUP }, message: { id: "A" } }),
    ),
    readMessages: record(
      "readMessages",
      Promise.resolve({ chat: { jid: GROUP }, messages: [{ id: "A" }] }),
    ),
    listChats: record("listChats", [
      {
        jid: GROUP,
        name: "Ranqia",
        phone: null,
        type: "group" as const,
        unreadCount: 2,
        lastMessageAt: 1_791_000_000,
        archived: false,
        pinned: true,
      },
    ]),
    markRead: record("markRead", Promise.resolve(3)),
    setPresence: record("setPresence", Promise.resolve()),
    getGroupInfo: record(
      "getGroupInfo",
      Promise.resolve({
        jid: GROUP,
        subject: "Ranqia",
        description: null,
        owner: null,
        createdAt: null,
        size: 1,
        announceOnly: false,
        restricted: false,
        addressingMode: "lid" as const,
        participants: [{ jid: "1@lid", name: "Ada", phone: "+5511999999999", admin: null }],
      }),
    ),
    ...overrides,
  }
  return { account, calls }
}

const Json = z.record(z.string(), z.unknown())

async function jsonOf(response: Response): Promise<Record<string, unknown>> {
  return Json.parse(await response.json())
}

function argsOf(calls: readonly { method: string; args: unknown[] }[], method: string): unknown[] {
  return calls.find((c) => c.method === method)?.args ?? []
}

function call(account: ApiAccount, method: string, path: string, body?: unknown) {
  const init: RequestInit = { method }
  if (body !== undefined) init.body = JSON.stringify(body)
  return handleApi(new Request(`https://account${path}`, init), account)
}

describe("REST API", () => {
  test("sends with mentions and answers with the chat and message id", async () => {
    const { account, calls } = fakeAccount()
    const response = await call(account, "POST", "/messages", {
      to: GROUP,
      text: "oi @5511999999999",
      mentions: ["+5511999999999"],
      reply_to: "Q",
    })
    expect(response.status).toBe(201)
    expect(await jsonOf(response)).toEqual({ chat_jid: GROUP, id: "SENT", deduplicated: false })
    expect(argsOf(calls, "send")[0]).toMatchObject({
      to: GROUP,
      mentions: ["+5511999999999"],
      replyTo: "Q",
    })
  })

  test("edits a message under an encoded JID", async () => {
    const { account, calls } = fakeAccount()
    const response = await call(account, "PATCH", `/messages/${encodeURIComponent(GROUP)}/SENT`, {
      text: "pronto",
    })
    expect(response.status).toBe(200)
    expect(await jsonOf(response)).toEqual({ chat_jid: GROUP, id: "SENT" })
    expect(argsOf(calls, "editMessage")).toEqual([GROUP, "SENT", "pronto"])
  })

  test("a refusal keeps its status and code", async () => {
    const { account } = fakeAccount({
      editMessage: () => Promise.reject(new RequestError(409, "edit_window_closed", "too late")),
    })
    const response = await call(account, "PATCH", `/messages/${GROUP}/OLD`, { text: "x" })
    expect(response.status).toBe(409)
    expect(await jsonOf(response)).toEqual({ error: "too late", code: "edit_window_closed" })
  })

  test("bad input is a 400 with invalid_request, unknown paths a 404, wrong methods a 405", async () => {
    const { account } = fakeAccount()
    const bad = await call(account, "POST", "/messages", { to: GROUP })
    expect(bad.status).toBe(400)
    expect((await jsonOf(bad)).code).toBe("invalid_request")
    expect((await call(account, "GET", "/nope")).status).toBe(404)
    expect((await call(account, "DELETE", "/chats")).status).toBe(405)
  })

  test("lists groups with ISO times", async () => {
    const { account, calls } = fakeAccount()
    const response = await call(account, "GET", "/chats?type=group")
    expect(await jsonOf(response)).toEqual({
      chats: [
        {
          jid: GROUP,
          name: "Ranqia",
          type: "group",
          phone: null,
          unread_count: 2,
          last_message_at: new Date(1_791_000_000_000).toISOString(),
          archived: false,
          pinned: true,
        },
      ],
    })
    expect(argsOf(calls, "listChats")[0]).toMatchObject({ type: "groups", includeArchived: true })
  })

  test("reads a time window, since inclusive and until exclusive", async () => {
    const { account, calls } = fakeAccount()
    const response = await call(
      account,
      "GET",
      `/chats/${GROUP}/messages?since=2026-10-03T00:00:00-03:00&until=2026-10-04T00:00:00-03:00`,
    )
    expect(await jsonOf(response)).toEqual({
      chat: { jid: GROUP },
      messages: [{ id: "A" }],
    })
    const since = Date.parse("2026-10-03T03:00:00Z") / 1000
    expect(argsOf(calls, "readMessages")[0]).toMatchObject({
      chatJid: GROUP,
      after: since - 1,
      before: since + 86_400,
    })
  })

  test("shows typing, and the group's members", async () => {
    const { account, calls } = fakeAccount()
    const typing = await call(account, "POST", `/chats/${GROUP}/presence`, { state: "typing" })
    expect(typing.status).toBe(204)
    expect(argsOf(calls, "setPresence")).toEqual([GROUP, "typing"])
    const group = await call(account, "GET", `/groups/${GROUP}`)
    expect(await jsonOf(group)).toMatchObject({
      group: {
        addressing_mode: "lid",
        participants: [{ jid: "1@lid", phone: "+5511999999999", name: "Ada", admin: null }],
      },
    })
  })

  test("me names the number by phone and by LID", async () => {
    const { account } = fakeAccount()
    expect(await jsonOf(await call(account, "GET", "/me"))).toEqual({
      jid: "5511000000000@s.whatsapp.net",
      lid: "42@lid",
      phone: "+5511000000000",
      name: "Bob",
      state: "open",
    })
  })
})
