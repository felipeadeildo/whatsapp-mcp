/**
 * The slice of Durable Object storage the archive uses. `DurableObjectStorage`
 * satisfies it as is; tests provide the same surface over an in-memory SQLite.
 */
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
