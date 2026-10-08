// Blobs come back as ArrayBuffer, as they do from the Durable Object.
import { Database, type SQLQueryBindings } from "bun:sqlite"

import type { SqlCursor, SqlDatabase } from "../src/store/sql"

type Row = Record<string, SqlStorageValue>

function fromSqlite(row: Record<string, SqlStorageValue | Uint8Array>): Row {
  const out: Row = {}
  for (const [key, value] of Object.entries(row)) {
    out[key] = value instanceof Uint8Array ? value.slice().buffer : value
  }
  return out
}

function toSqlite(value: SqlStorageValue): SQLQueryBindings {
  return value instanceof ArrayBuffer ? new Uint8Array(value) : value
}

function cursor<T>(rows: T[], rowsWritten: number): SqlCursor<T> {
  let index = 0
  return {
    next: () => {
      const value = rows[index]
      index += 1
      return value === undefined ? { done: true } : { value }
    },
    toArray: () => rows,
    one: () => {
      const [only] = rows
      if (rows.length !== 1 || only === undefined)
        throw new Error(`expected one row, got ${rows.length}`)
      return only
    },
    rowsWritten,
    [Symbol.iterator]: () => rows[Symbol.iterator](),
  }
}

export function memoryDatabase(): SqlDatabase {
  const db = new Database(":memory:")
  return {
    sql: {
      exec<T extends Row>(query: string, ...bindings: SqlStorageValue[]): SqlCursor<T> {
        const statement = db.prepare<
          Record<string, SqlStorageValue | Uint8Array>,
          SQLQueryBindings[]
        >(query)
        const rows = statement.all(...bindings.map(toSqlite)).map(fromSqlite)
        const written =
          statement.columnNames.length === 0
            ? db.query<{ n: number }, []>("SELECT changes() AS n").get()?.n
            : 0
        // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- the caller names its query's row shape, as with the Durable Object API
        return cursor(rows as T[], written ?? 0)
      },
    },
    transactionSync: <T>(closure: () => T): T => db.transaction(closure)(),
  }
}
