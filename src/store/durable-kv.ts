/**
 * The byte store baileyrs persists its Rust engine into (device record, Signal
 * sessions and keys, app-state), kept in one table of the Durable Object's
 * SQLite. Values are opaque bytes; only the engine reads them.
 *
 * The DO SQL API is synchronous, so the async contract is satisfied by plain
 * `async` methods, and batch writes run inside one `transactionSync`.
 */
import type { HostStoreCallbacks } from "@oxidezap/baileyrs/host"

type Entry = [key: string, value: Uint8Array]

const SCHEMA = `
  CREATE TABLE IF NOT EXISTS wa_store (
    store TEXT NOT NULL,
    key TEXT NOT NULL,
    value BLOB NOT NULL,
    PRIMARY KEY (store, key)
  ) WITHOUT ROWID
`

/** Rows of one store whose key starts with a prefix; binds (store, prefix, prefix). */
const IN_STORE_WITH_PREFIX = "store = ? AND substr(key, 1, length(?)) = ?"

/** Copies a view into a standalone buffer, which is what the SQL API binds as BLOB. */
function toBlob(value: Uint8Array): ArrayBuffer {
  return value.slice().buffer
}

export function durableKvStore(storage: DurableObjectStorage): HostStoreCallbacks & {
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

    /** Forgets the linked device, so the next start pairs from scratch. */
    clear() {
      sql.exec("DELETE FROM wa_store")
    },
  }
}
