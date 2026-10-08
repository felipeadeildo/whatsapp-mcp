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
    senderName: null,
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

describe("people", () => {
  test("a phone JID resolves to the LID once they are known to be one person", () => {
    const archive = new Archive(memoryDatabase())
    expect(archive.canonical(PN)).toBe(PN)
    archive.learnPerson({ jid: LID, lid: LID, pn: PN })
    expect(archive.canonical(PN)).toBe(LID)
    expect(archive.canonical(LID)).toBe(LID)
  })

  test("learning the pair later moves a chat and its messages from the phone JID to the LID", () => {
    const archive = new Archive(memoryDatabase())
    archive.saveMessages([message({ chatJid: PN, id: "A" })])
    archive.saveReaction(PN, "A", PN, "👍", 1_000)
    archive.learnPerson({ jid: LID, lid: LID, pn: PN })

    expect(archive.getChat(PN)).toBeNull()
    expect(archive.getChat(LID)).not.toBeNull()
    const [moved] = archive.getMessages({ chatJid: LID, limit: 10 })
    expect(moved).toMatchObject({
      chatJid: LID,
      senderJid: LID,
      reactions: [{ emoji: "👍", senderJid: LID }],
    })
  })

  test("the same message stored under both JIDs ends up once", () => {
    const archive = new Archive(memoryDatabase())
    archive.saveMessages([message({ chatJid: PN, id: "A" }), message({ chatJid: LID, id: "A" })])
    archive.learnPerson({ jid: LID, lid: LID, pn: PN })
    expect(archive.getMessages({ chatJid: LID, limit: 10 })).toHaveLength(1)
    expect(archive.stats().chats).toBe(1)
  })

  test("an update that only names the phone JID keeps the person under the LID", () => {
    const archive = new Archive(memoryDatabase())
    archive.learnPerson({ jid: LID, lid: LID, pn: PN })
    archive.saveMessages([message({ chatJid: LID, id: "A" })])
    archive.learnPerson({ jid: PN, name: "Maria (agenda)" })

    expect(archive.canonical(PN)).toBe(LID)
    expect(archive.getPerson(PN)).toMatchObject({
      jid: LID,
      name: "Maria (agenda)",
      phone: "+5511999999999",
    })
    expect(archive.getChat(LID)?.name).toBe("Maria (agenda)")
    expect(archive.getMessages({ chatJid: LID, limit: 10 })).toHaveLength(1)
  })

  test("names rank: address book, WhatsApp name, business name, phone, masked phone", () => {
    const archive = new Archive(memoryDatabase())
    archive.learnPerson({ jid: LID, redactedPhone: "+55∙∙∙∙∙∙∙∙99" })
    expect(archive.getPerson(LID)?.name).toBe("+55∙∙∙∙∙∙∙∙99")
    archive.learnPerson({ jid: LID, pn: PN })
    expect(archive.getPerson(LID)?.name).toBe("+5511999999999")
    archive.learnPerson({ jid: LID, businessName: "Padaria" })
    expect(archive.getPerson(LID)?.name).toBe("Padaria")
    archive.learnPerson({ jid: LID, pushName: "Maria" })
    expect(archive.getPerson(LID)?.name).toBe("Maria")
    archive.learnPerson({ jid: PN, name: "Mãe" })
    expect(archive.getPerson(LID)).toMatchObject({ name: "Mãe", whatsappName: "Maria" })
  })

  test("merging two known identities prefers the phone side's names", () => {
    const archive = new Archive(memoryDatabase())
    archive.learnPerson({ jid: LID, pushName: "maria_lid" })
    archive.learnPerson({ jid: PN, pushName: "Maria", name: "Mãe" })
    archive.learnPerson({ jid: LID, lid: LID, pn: PN })
    expect(archive.getPerson(LID)).toMatchObject({ jid: LID, name: "Mãe", whatsappName: "Maria" })
    expect(archive.stats().people).toBe(1)
  })
})

describe("messages", () => {
  test("search ignores accents and case and reports the sender by name", () => {
    const archive = new Archive(memoryDatabase())
    archive.learnPerson({ jid: LID, lid: LID, pn: PN, pushName: "Maria" })
    archive.saveMessages([
      message({ chatJid: LID, id: "A", text: "Vamos tomar AÇAÍ amanhã?" }),
      message({ chatJid: LID, id: "B", text: "outra coisa" }),
    ])
    const hits = archive.searchMessages({ query: "acai", limit: 10 })
    expect(hits).toHaveLength(1)
    expect(hits[0]).toMatchObject({
      id: "A",
      senderName: "Maria",
      chatName: "Maria",
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
    archive.learnPerson({ jid: LID, lid: LID, pn: PN, pushName: "Maria" })
    archive.saveMessages([
      message({ chatJid: "1@g.us", id: "A", text: "oi @5511999999999", mentions: [PN] }),
    ])
    expect(archive.getMessage("1@g.us", "A")?.mentions).toEqual([
      { user: "5511999999999", jid: LID, name: "Maria" },
    ])
  })

  test("a chat is found by the person's phone number", () => {
    const archive = new Archive(memoryDatabase())
    archive.learnPerson({ jid: LID, lid: LID, pn: PN, pushName: "Maria" })
    archive.saveMessages([message({ chatJid: LID, id: "A" })])
    expect(archive.findChats("99999", 10).map((chat) => chat.jid)).toEqual([LID])
    expect(archive.findChats("mar", 10).map((chat) => chat.jid)).toEqual([LID])
  })
})
