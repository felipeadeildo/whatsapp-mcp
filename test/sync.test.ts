import { describe, expect, test } from "bun:test"

import { Archive } from "../src/store/archive"
import type { Socket } from "../src/whatsapp/socket"
import { syncArchive } from "../src/whatsapp/sync"
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
})
