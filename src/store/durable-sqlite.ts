/**
 * Adapts a Durable Object's SQLite storage to the connection shape that
 * `@zapo-js/store-sqlite` expects, so zapo keeps its own schema and queries
 * and we only swap the driver.
 *
 * Differences from a file-backed driver that this layer absorbs:
 * - `BEGIN`/`COMMIT` are not allowed; transactions go through `transactionSync`.
 * - BLOBs are bound and returned as `ArrayBuffer`, while zapo works with `Uint8Array`.
 * - Booleans and bigints are not bindable.
 */
import type { WaSqliteConnection } from "@zapo-js/store-sqlite"

/**
 * What zapo binds. Its stores are written against better-sqlite3, which accepts
 * exactly these, so narrowing zapo's `unknown[]` to this is sound in practice.
 */
type SqlParam = string | number | bigint | boolean | Uint8Array | null | undefined

/** A row as zapo reads it: BLOB columns as `Uint8Array`. */
type SqlRow = Record<string, string | number | Uint8Array | null>

function toBinding(value: SqlParam): SqlStorageValue {
  if (value === undefined || value === null) return null
  if (typeof value === "boolean") return value ? 1 : 0
  if (typeof value === "bigint") return Number(value)
  if (value instanceof Uint8Array) return value.slice().buffer
  return value
}

function toRow(row: Record<string, SqlStorageValue>): SqlRow {
  const out: SqlRow = {}
  for (const [key, value] of Object.entries(row)) {
    out[key] = value instanceof ArrayBuffer ? new Uint8Array(value) : value
  }
  return out
}

/**
 * zapo's contract lets each caller name the row type of its own query
 * (`get<T>`/`all<T>`), and SQLite cannot check that, so this is the one place a
 * row type is asserted. `T` appears once because the contract, not this
 * function, decides it.
 */
// oxlint-disable-next-line typescript/no-unnecessary-type-parameters -- see the doc comment above
function asCallerRow<T extends Record<string, unknown>>(row: SqlRow): T {
  // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- see the doc comment above
  return row as T
}

export function durableSqliteConnection(storage: DurableObjectStorage): WaSqliteConnection {
  const execute = (sql: string, params: readonly SqlParam[] = []) =>
    storage.sql.exec(sql, ...params.map(toBinding))

  return {
    // Not used to pick a driver because the connection is supplied pre-opened.
    driver: "node",
    exec(sql: string) {
      storage.sql.exec(sql)
    },
    run(sql: string, params?: readonly SqlParam[]) {
      execute(sql, params)
    },
    // oxlint-disable-next-line typescript/no-unnecessary-type-parameters -- required by WaSqliteConnection
    get<T extends Record<string, unknown>>(sql: string, params?: readonly SqlParam[]) {
      const first = execute(sql, params).next()
      return first.done ? null : asCallerRow<T>(toRow(first.value))
    },
    // oxlint-disable-next-line typescript/no-unnecessary-type-parameters -- required by WaSqliteConnection
    all<T extends Record<string, unknown>>(sql: string, params?: readonly SqlParam[]) {
      return Array.from(execute(sql, params), (row) => asCallerRow<T>(toRow(row)))
    },
    async runInTransaction(run) {
      return storage.transactionSync(run)
    },
    close() {},
  }
}
