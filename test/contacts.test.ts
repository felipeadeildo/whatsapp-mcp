import { describe, expect, test } from "bun:test"

import { Archive } from "../src/store/archive"
import {
  type AddressBook,
  contactTarget,
  NotAPersonError,
  PhoneUnknownError,
  removeContact,
  saveContact,
} from "../src/whatsapp/contacts"
import { memoryDatabase } from "./memory-database"

const PN = "15550100000@s.whatsapp.net"
const LID = "123456789@lid"

type ContactAction = Parameters<AddressBook["addOrEditContact"]>[1]
type BookCall = { op: "save"; jid: string; contact: ContactAction } | { op: "remove"; jid: string }

function fakeBook(): { book: AddressBook; calls: BookCall[] } {
  const calls: BookCall[] = []
  const book: AddressBook = {
    async addOrEditContact(jid, contact) {
      calls.push({ op: "save", jid, contact })
    },
    async removeContact(jid) {
      calls.push({ op: "remove", jid })
    },
  }
  return { book, calls }
}

function pairedArchive(): Archive {
  const archive = new Archive(memoryDatabase())
  archive.learnPair(PN, LID)
  return archive
}

describe("contactTarget", () => {
  test("a LID resolves to its phone JID through the learned pair", () => {
    expect(contactTarget(pairedArchive(), LID)).toEqual({ jid: LID, pn: PN })
  })

  test("a formatted phone number resolves to the person's LID and phone JID", () => {
    expect(contactTarget(pairedArchive(), "+1 555-010-0000")).toEqual({ jid: LID, pn: PN })
  })

  test("a phone JID with a device suffix becomes the bare phone JID", () => {
    expect(contactTarget(pairedArchive(), "15550100000:3@s.whatsapp.net")).toEqual({
      jid: LID,
      pn: PN,
    })
  })

  test("a phone with no known LID stays the phone JID", () => {
    expect(contactTarget(new Archive(memoryDatabase()), "+15550100000")).toEqual({
      jid: PN,
      pn: PN,
    })
  })

  test("a LID with no known phone is refused", () => {
    expect(() => contactTarget(new Archive(memoryDatabase()), LID)).toThrow(PhoneUnknownError)
  })

  test("a group is refused", () => {
    expect(() => contactTarget(pairedArchive(), "120363000000000000@g.us")).toThrow(NotAPersonError)
  })
})

describe("saveContact", () => {
  test("sends the phone JID, not the LID, and names the person in the archive", async () => {
    const archive = pairedArchive()
    archive.learnNames(LID, [{ source: "push", name: "Ada" }])
    const { book, calls } = fakeBook()

    const saved = await saveContact(book, archive, LID, { fullName: " Ada Lovelace " })

    expect(calls).toEqual([
      {
        op: "save",
        jid: PN,
        contact: { fullName: "Ada Lovelace", firstName: "Ada", saveOnPrimaryAddressbook: true },
      },
    ])
    expect(saved).toEqual({
      jid: LID,
      name: "Ada Lovelace",
      phone: "+15550100000",
      aliases: ["Ada"],
    })
    expect(archive.findPeople("lovelace", 10).map((person) => person.name)).toEqual([
      "Ada Lovelace",
    ])
  })

  test("an explicit first name is sent as given", async () => {
    const { book, calls } = fakeBook()
    await saveContact(book, pairedArchive(), PN, { fullName: "Ada Lovelace", firstName: "Augusta" })
    expect(calls[0]).toMatchObject({ contact: { firstName: "Augusta" } })
  })

  test("a name saved by phone alone follows the person once the LID is learned", async () => {
    const archive = new Archive(memoryDatabase())
    const { book } = fakeBook()
    await saveContact(book, archive, "+15550100000", { fullName: "Ada Lovelace" })
    archive.learnPair(PN, LID)
    expect(archive.getPerson(LID).name).toBe("Ada Lovelace")
  })

  test("nothing is sent when the phone is unknown", async () => {
    const { book, calls } = fakeBook()
    const outcome = await saveContact(book, new Archive(memoryDatabase()), LID, {
      fullName: "Ada Lovelace",
    }).catch((error: Error) => error)
    expect(outcome).toBeInstanceOf(PhoneUnknownError)
    expect(calls).toEqual([])
  })
})

describe("removeContact", () => {
  test("sends the phone JID and drops only the address book name", async () => {
    const archive = pairedArchive()
    archive.learnNames(LID, [
      { source: "contact", name: "Ada Lovelace" },
      { source: "push", name: "Ada" },
    ])
    const { book, calls } = fakeBook()

    const removed = await removeContact(book, archive, LID)

    expect(calls).toEqual([{ op: "remove", jid: PN }])
    expect(removed).toEqual({ jid: LID, name: "Ada", phone: "+15550100000", aliases: [] })
  })
})
