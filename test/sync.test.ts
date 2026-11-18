import { describe, expect, test } from "bun:test"

import { Archive } from "../src/store/archive"
import type { Socket } from "../src/whatsapp/socket"
import { type ArchiveChange, syncArchive } from "../src/whatsapp/sync"
import { memoryDatabase } from "./memory-database"

const CHAT = "5511988887777@s.whatsapp.net"

function fakeSocket() {
  const listeners = new Map<string, ((arg: unknown) => void)[]>()
  const ev = {
    on: (event: string, listener: (arg: unknown) => void) => {
      listeners.set(event, [...(listeners.get(event) ?? []), listener])
    },
  }
  // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- only `ev.on` is used
  const socket = { ev, user: undefined } as unknown as Socket
  const emit = (event: string, arg: unknown) =>
    (listeners.get(event) ?? []).forEach((listener) => listener(arg))
  return { socket, emit }
}

const text = (id: string, conversation: string, fromMe = false) => ({
  key: { remoteJid: CHAT, id, fromMe },
  message: { conversation },
  messageTimestamp: 1_791_000_000,
})

function watched() {
  const archive = new Archive(memoryDatabase())
  const { socket, emit } = fakeSocket()
  const changes: ArchiveChange[] = []
  syncArchive(socket, archive, { onChanges: (batch) => changes.push(...batch) })
  return { emit, changes }
}

describe("sync", () => {
  test("events from a number that does not own the archive are dropped", () => {
    const archive = new Archive(memoryDatabase())
    const { socket, emit } = fakeSocket()
    syncArchive(socket, archive, { accepts: () => false })
    emit("chats.upsert", [{ id: CHAT, unreadCount: 3 }])
    expect(archive.stats().chats).toBe(0)
  })

  test("events from the owner reach the archive", () => {
    const archive = new Archive(memoryDatabase())
    const { socket, emit } = fakeSocket()
    syncArchive(socket, archive, { accepts: () => true })
    emit("chats.upsert", [{ id: CHAT, unreadCount: 3 }])
    expect(archive.stats().chats).toBe(1)
  })

  describe("changes", () => {
    test("a live message is news once, even when WhatsApp sends it again", () => {
      const { emit, changes } = watched()
      emit("messages.upsert", { type: "notify", messages: [text("A", "oi")] })
      emit("messages.upsert", { type: "notify", messages: [text("A", "oi")] })
      expect(changes).toEqual([{ type: "message", chatJid: CHAT, id: "A" }])
    })

    test("history is never news", () => {
      const { emit, changes } = watched()
      emit("messaging-history.set", {
        chats: [],
        contacts: [],
        messages: [text("A", "oi")],
        syncType: 0,
      })
      emit("messages.upsert", { type: "append", messages: [text("A", "oi")] })
      expect(changes).toEqual([])
    })

    test("an edit is news when the text changes, not when its echo arrives", () => {
      const { emit, changes } = watched()
      emit("messages.upsert", { type: "notify", messages: [text("A", "às 10")] })
      const edit = {
        key: { remoteJid: CHAT, id: "A" },
        update: {
          message: {
            protocolMessage: {
              type: 14,
              key: { id: "A" },
              editedMessage: { conversation: "às 11" },
            },
          },
        },
      }
      emit("messages.update", [edit])
      emit("messages.update", [edit])
      expect(changes.slice(1)).toEqual([{ type: "edit", chatJid: CHAT, id: "A" }])
    })

    test("a deletion is news once", () => {
      const { emit, changes } = watched()
      emit("messages.upsert", { type: "notify", messages: [text("A", "oi")] })
      const revoke = { key: { remoteJid: CHAT, id: "A" }, update: { messageStubType: 1 } }
      emit("messages.update", [revoke])
      emit("messages.update", [revoke])
      expect(changes.slice(1)).toEqual([{ type: "revoke", chatJid: CHAT, id: "A" }])
    })

    test("a reaction is news when it is added and when it is removed", () => {
      const { emit, changes } = watched()
      emit("messages.upsert", { type: "notify", messages: [text("A", "oi")] })
      const react = (emoji: string, at: number) => ({
        key: { remoteJid: CHAT, id: "A" },
        reaction: { key: { remoteJid: CHAT, fromMe: false }, text: emoji, senderTimestampMs: at },
      })
      emit("messages.reaction", [react("👍", 1_000)])
      emit("messages.reaction", [react("👍", 2_000)])
      emit("messages.reaction", [react("", 3_000)])
      expect(changes.slice(1)).toEqual([
        { type: "reaction", chatJid: CHAT, id: "A", senderJid: CHAT, emoji: "👍" },
        { type: "reaction", chatJid: CHAT, id: "A", senderJid: CHAT, emoji: "" },
      ])
    })
  })
})
