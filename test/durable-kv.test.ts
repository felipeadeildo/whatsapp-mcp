import { describe, expect, test } from "bun:test"

import { durableKvStore } from "../src/store/durable-kv"
import { memoryDatabase } from "./memory-database"

const bytes = (text: string): Uint8Array => new TextEncoder().encode(text)
const text = (value: Uint8Array | null): string | null =>
  value === null ? null : new TextDecoder().decode(value)

describe("session store", () => {
  test("a patch replaces the version, its MACs and the MAC index in one go", async () => {
    const store = durableKvStore(memoryDatabase())
    await store.commitPatch("regular", bytes("v1"), [], [["regular:a", bytes("A")]], true)
    await store.commitPatch(
      "regular",
      bytes("v2"),
      ["regular:a"],
      [["regular:b", bytes("B")]],
      true,
    )

    expect(text(await store.get("sync_version", "regular"))).toBe("v2")
    expect(await store.get("mutation_mac", "regular:a")).toBeNull()
    expect(text(await store.get("mutation_mac", "regular:b"))).toBe("B")
    expect(text(await store.get("meta", "mutation_mac_keys"))).toBe('["regular:b"]')
  })

  test("a failed patch leaves the old state", async () => {
    const store = durableKvStore(memoryDatabase())
    await store.set("meta", "mutation_mac_keys", bytes("not an index"))
    await store.commitPatch("regular", bytes("v1"), [], [["regular:a", bytes("A")]], false)

    const failure = await store
      .commitPatch("regular", bytes("v2"), ["regular:a"], [["regular:b", bytes("B")]], true)
      .catch((error: unknown) => error)
    expect(failure).toBeInstanceOf(Error)
    expect(text(await store.get("sync_version", "regular"))).toBe("v1")
    expect(text(await store.get("mutation_mac", "regular:a"))).toBe("A")
    expect(await store.get("mutation_mac", "regular:b")).toBeNull()
  })

  test("a received token keeps the newest bytes and the sender time keeps the largest", async () => {
    const store = durableKvStore(memoryDatabase())
    const jid = "1@lid"
    await store.touchTcTokenSenderTimestamp(jid, "9223372036854775806")
    await store.storeReceivedTcToken(jid, new Uint8Array([1, 2]), "100")
    await store.storeReceivedTcToken(jid, new Uint8Array([3]), "99")
    await store.touchTcTokenSenderTimestamp(jid, "5")

    expect(text(await store.get("tc_token", jid))).toBe(
      '{"token":[1,2],"token_timestamp":100,"sender_timestamp":9223372036854775806}',
    )
    expect(text(await store.get("meta", "tc_token_jids"))).toBe('["1@lid"]')
  })
})
