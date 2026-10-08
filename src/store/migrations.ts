/**
 * Forward-only schema migrations for a Durable Object's SQLite. Each module
 * owns a named list; applied steps are recorded by position, so steps are only
 * ever appended, never edited or reordered, once a version has been deployed.
 */
import type { SqlDatabase } from "./sql"

export function migrate(storage: SqlDatabase, name: string, steps: readonly string[]): void {
  const sql = storage.sql
  sql.exec(
    "CREATE TABLE IF NOT EXISTS _migrations (name TEXT PRIMARY KEY, applied INTEGER NOT NULL)",
  )
  const row = sql
    .exec<{ applied: number }>("SELECT applied FROM _migrations WHERE name = ?", name)
    .next()
  const applied = row.done ? 0 : row.value.applied
  if (applied >= steps.length) return
  storage.transactionSync(() => {
    for (const step of steps.slice(applied)) sql.exec(step)
    sql.exec(
      "INSERT INTO _migrations (name, applied) VALUES (?, ?) ON CONFLICT (name) DO UPDATE SET applied = excluded.applied",
      name,
      steps.length,
    )
  })
}
