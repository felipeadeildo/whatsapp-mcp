// What the archive needs from Durable Object storage, so tests can use bun:sqlite.
export interface SqlCursor<T> extends Iterable<T> {
  next(): { done?: false; value: T } | { done: true; value?: never }
  toArray(): T[]
  one(): T
  readonly rowsWritten: number
}

export interface SqlDatabase {
  readonly sql: {
    exec<T extends Record<string, SqlStorageValue>>(
      query: string,
      ...bindings: SqlStorageValue[]
    ): SqlCursor<T>
  }
  transactionSync<T>(closure: () => T): T
}
