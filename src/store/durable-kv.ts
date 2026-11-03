// The session engine's state, as opaque bytes in one table.
import type { HostStoreCallbacks } from "@oxidezap/baileyrs/host"
import { type AtomicRows, atomicCallbacks } from "@oxidezap/baileyrs/lib/Utils/atomic-store.js"

import type { SqlDatabase } from "./sql"

type Entry = [key: string, value: Uint8Array]

const SCHEMA = `
  CREATE TABLE IF NOT EXISTS wa_store (
    store TEXT NOT NULL,
    key TEXT NOT NULL,
    value BLOB NOT NULL,
    PRIMARY KEY (store, key)
  ) WITHOUT ROWID
`

// Binds (store, prefix, prefix).
const IN_STORE_WITH_PREFIX = "store = ? AND substr(key, 1, length(?)) = ?"

// The inverse of baileyrs' rowKey, which is JSON.stringify([store, key]).
function parseRowKey(row: string): [store: string, key: string] {
  const parsed: unknown = JSON.parse(row)
  if (
    !Array.isArray(parsed) ||
    parsed.length !== 2 ||
    typeof parsed[0] !== "string" ||
    typeof parsed[1] !== "string"
  ) {
    throw new TypeError(`not a store row key: ${row}`)
  }
  return [parsed[0], parsed[1]]
}

// The SQL API binds whole ArrayBuffers, not views.
function toBlob(value: Uint8Array): ArrayBuffer {
  return value.slice().buffer
}

export function durableKvStore(storage: SqlDatabase): HostStoreCallbacks & {
  clear(): void
} {
  const sql = storage.sql
  sql.exec(SCHEMA)

  // The bridge calls these callbacks detached from the object, so none of them
  // may use `this`; shared logic lives in these closures instead.
  const read = (store: string, key: string): Uint8Array | null => {
    const first = sql
      .exec<{ value: ArrayBuffer }>(
        "SELECT value FROM wa_store WHERE store = ? AND key = ?",
        store,
        key,
      )
      .next()
    return first.done ? null : new Uint8Array(first.value.value)
  }
  const put = (store: string, key: string, value: Uint8Array): void => {
    sql.exec(
      "INSERT OR REPLACE INTO wa_store (store, key, value) VALUES (?, ?, ?)",
      store,
      key,
      toBlob(value),
    )
  }
  const remove = (store: string, key: string): void => {
    sql.exec("DELETE FROM wa_store WHERE store = ? AND key = ?", store, key)
  }
  // The bridge's atomic writes (app state patches, trusted-contact tokens) work on rows
  // keyed by rowKey(store, key). This map reads and writes wa_store directly, so a
  // change inside transactionSync sees and commits every row in one transaction.
  class StoreRows extends Map<string, Uint8Array> {
    override get(row: string): Uint8Array | undefined {
      const [store, key] = parseRowKey(row)
      return read(store, key) ?? undefined
    }
    override set(row: string, value: Uint8Array): this {
      const [store, key] = parseRowKey(row)
      put(store, key, value)
      return this
    }
    override delete(row: string): boolean {
      const [store, key] = parseRowKey(row)
      remove(store, key)
      return true
    }
  }
  const transaction = async (change: (rows: AtomicRows) => void): Promise<void> => {
    storage.transactionSync(() => change(new StoreRows()))
  }
  const entries = (store: string, prefix = ""): Entry[] =>
    Array.from(
      sql.exec<{ key: string; value: ArrayBuffer }>(
        `SELECT key, value FROM wa_store WHERE ${IN_STORE_WITH_PREFIX}`,
        store,
        prefix,
        prefix,
      ),
      (row) => [row.key, new Uint8Array(row.value)],
    )

  return {
    capabilities: { batch: true, enumerate: true, prefixDelete: true },
    ...atomicCallbacks(transaction),

    async get(store, key) {
      return read(store, key)
    },
    async set(store, key, value) {
      put(store, key, value)
    },
    async delete(store, key) {
      remove(store, key)
    },

    async getMany(store, keys) {
      const found: Entry[] = []
      for (const key of keys) {
        const value = read(store, key)
        if (value) found.push([key, value])
      }
      return found
    },
    async setMany(store, batch) {
      storage.transactionSync(() => {
        for (const [key, value] of batch) put(store, key, value)
      })
    },
    async deleteMany(store, keys) {
      storage.transactionSync(() => {
        for (const key of keys) remove(store, key)
      })
    },

    async listKeys(store, prefix) {
      return entries(store, prefix).map(([key]) => key)
    },
    async listEntries(store, prefix) {
      return entries(store, prefix)
    },
    async deletePrefix(store, prefix) {
      return sql.exec(`DELETE FROM wa_store WHERE ${IN_STORE_WITH_PREFIX}`, store, prefix, prefix)
        .rowsWritten
    },

    clear() {
      sql.exec("DELETE FROM wa_store")
    },
  }
}
