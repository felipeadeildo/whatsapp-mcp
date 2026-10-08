import { describe, expect, test } from "bun:test"

import { Archive, type MessageInput } from "../src/store/archive"
import { memoryDatabase } from "./memory-database"

const PN = "5511999999999@s.whatsapp.net"
const LID = "123456789@lid"

function message(
  overrides: Partial<MessageInput> & Pick<MessageInput, "chatJid" | "id">,
): MessageInput {
  return {
    fromMe: false,
    senderJid: overrides.chatJid,
    sentAt: 1_791_000_000,
    kind: "text",
    text: "oi",
    quotedId: null,
    mentions: [],
    forwarded: false,
    media: null,
    raw: null,
    ...overrides,
  }
}

describe("identity", () => {
  test("a phone JID resolves to the LID once they are known to be one person", () => {
    const archive = new Archive(memoryDatabase())
    expect(archive.canonical(PN)).toBe(PN)
    archive.learnPair(PN, LID)
    expect(archive.canonical(PN)).toBe(LID)
    expect(archive.canonical(LID)).toBe(LID)
  })

  test("learning the pair moves chat, messages, reactions and names from the phone JID to the LID", () => {
    const archive = new Archive(memoryDatabase())
    archive.saveMessages([message({ chatJid: PN, id: "A" })])
    archive.saveReaction(PN, "A", PN, "👍", 1_000)
    archive.learnNames(PN, [{ source: "contact", name: "Ada Lovelace" }])
    archive.learnPair(PN, LID)

    expect(archive.getChat(PN)).toBeNull()
    expect(archive.getChat(LID)).toMatchObject({ name: "Ada Lovelace", phone: "+5511999999999" })
    const [moved] = archive.getMessages({ chatJid: LID, limit: 10 })
    expect(moved).toMatchObject({
      chatJid: LID,
      senderJid: LID,
      senderName: "Ada Lovelace",
      reactions: [{ emoji: "👍", senderJid: LID }],
    })
  })

  test("the same message stored under both JIDs ends up once", () => {
    const archive = new Archive(memoryDatabase())
    archive.saveMessages([message({ chatJid: PN, id: "A" }), message({ chatJid: LID, id: "A" })])
    archive.learnPair(PN, LID)
    expect(archive.getMessages({ chatJid: LID, limit: 10 })).toHaveLength(1)
    expect(archive.stats().chats).toBe(1)
  })

  test("names learned under the phone JID after the pair land on the LID", () => {
    const archive = new Archive(memoryDatabase())
    archive.learnPair(PN, LID)
    archive.learnNames(PN, [{ source: "contact", name: "Ada Lovelace" }])
    expect(archive.getPerson(PN)).toMatchObject({ jid: LID, name: "Ada Lovelace" })
  })

  test("both sides' names survive a merge", () => {
    const archive = new Archive(memoryDatabase())
    archive.learnNames(LID, [{ source: "push", name: "ada_l", seenAt: 1 }])
    archive.learnNames(PN, [
      { source: "push", name: "Ada", seenAt: 2 },
      { source: "contact", name: "Ada Lovelace" },
    ])
    archive.learnPair(PN, LID)
    expect(archive.getPerson(LID)).toMatchObject({
      name: "Ada Lovelace",
      aliases: ["Ada", "ada_l"],
    })
  })
})

describe("names", () => {
  test("display prefers the address book, then the WhatsApp name, then the phone, then the mask", () => {
    const archive = new Archive(memoryDatabase())
    archive.learnNames(LID, [{ source: "redacted", name: "+55∙∙∙∙∙∙∙∙99" }])
    expect(archive.getPerson(LID).name).toBe("+55∙∙∙∙∙∙∙∙99")
    archive.learnPair(PN, LID)
    expect(archive.getPerson(LID)).toMatchObject({ name: "+5511999999999", aliases: [] })
    archive.learnNames(LID, [{ source: "username", name: "@ada" }])
    expect(archive.getPerson(LID).name).toBe("@ada")
    archive.learnNames(LID, [{ source: "push", name: "Ada" }])
    expect(archive.getPerson(LID).name).toBe("Ada")
    archive.learnNames(LID, [{ source: "contact", name: "Ada Lovelace" }])
    expect(archive.getPerson(LID)).toMatchObject({ name: "Ada Lovelace", aliases: ["Ada", "@ada"] })
  })

  test("a newer name of the same source wins, and the old one stays searchable", () => {
    const archive = new Archive(memoryDatabase())
    archive.saveMessages([message({ chatJid: LID, id: "A" })])
    archive.learnNames(LID, [{ source: "push", name: "Augusta", seenAt: 100 }])
    archive.learnNames(LID, [{ source: "push", name: "Ada 🌻", seenAt: 200 }])
    expect(archive.getPerson(LID).name).toBe("Ada 🌻")
    expect(archive.findPeople("augusta", 10)).toEqual([
      { jid: LID, name: "Ada 🌻", phone: null, aliases: ["Augusta"], matched: "Augusta" },
    ])
  })

  test("search ignores accents and case, and finds a chat by its phone", () => {
    const archive = new Archive(memoryDatabase())
    archive.learnPair(PN, LID)
    archive.learnNames(LID, [{ source: "contact", name: "Zoë Ångström" }])
    archive.saveMessages([message({ chatJid: LID, id: "A" })])
    expect(archive.findChats("zoe", 10)).toMatchObject([
      { jid: LID, name: "Zoë Ångström", matched: null },
    ])
    expect(archive.findChats("angstrom", 10).map((chat) => chat.jid)).toEqual([LID])
    expect(archive.findChats("99999", 10).map((chat) => chat.jid)).toEqual([LID])
    expect(archive.findChats("pedro", 10)).toEqual([])
  })

  test("groups are named by their subject", () => {
    const archive = new Archive(memoryDatabase())
    archive.saveChats([{ jid: "1@g.us" }])
    archive.learnNames("1@g.us", [{ source: "subject", name: "Book club" }])
    expect(archive.getChat("1@g.us")).toMatchObject({
      name: "Book club",
      type: "group",
      phone: null,
    })
  })
})

describe("messages", () => {
  test("search ignores accents and case and reports the sender by name", () => {
    const archive = new Archive(memoryDatabase())
    archive.learnNames(LID, [{ source: "push", name: "Ada" }])
    archive.saveMessages([
      message({ chatJid: LID, id: "A", text: "Vamos tomar AÇAÍ amanhã?" }),
      message({ chatJid: LID, id: "B", text: "outra coisa" }),
    ])
    const hits = archive.searchMessages({ query: "acai", limit: 10 })
    expect(hits).toHaveLength(1)
    expect(hits[0]).toMatchObject({
      id: "A",
      senderName: "Ada",
      chatName: "Ada",
      snippet: "Vamos tomar [AÇAÍ] amanhã?",
    })
  })

  test("an edit replaces the searchable text and survives a redelivery of the original", () => {
    const archive = new Archive(memoryDatabase())
    archive.saveMessages([message({ chatJid: LID, id: "A", text: "reunião às 10" })])
    archive.applyEdit(LID, "A", "reunião às 11")
    archive.saveMessages([message({ chatJid: LID, id: "A", text: "reunião às 10" })])
    expect(archive.getMessage(LID, "A")).toMatchObject({ text: "reunião às 11", edited: true })
    expect(archive.searchMessages({ query: "11", limit: 10 })).toHaveLength(1)
    expect(archive.searchMessages({ query: "10", limit: 10 })).toHaveLength(0)
  })

  test("mentions resolve to names", () => {
    const archive = new Archive(memoryDatabase())
    archive.learnPair(PN, LID)
    archive.learnNames(LID, [{ source: "push", name: "Ada" }])
    archive.saveMessages([
      message({ chatJid: "1@g.us", id: "A", text: "oi @5511999999999", mentions: [PN] }),
    ])
    expect(archive.getMessage("1@g.us", "A")?.mentions).toEqual([
      { user: "5511999999999", jid: LID, name: "Ada" },
    ])
  })
})
