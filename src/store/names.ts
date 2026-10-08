// Lower ranks win. `contact` is the address book, `push` the name people set on
// WhatsApp, `history` what history sync called a conversation, `redacted` a masked phone.
const RANK = {
  contact: 0,
  push: 1,
  business: 2,
  subject: 3,
  history: 4,
  username: 5,
  redacted: 6,
} as const satisfies Record<string, number>

export type NameSource = keyof typeof RANK

export interface NameRecord {
  readonly source: NameSource
  readonly name: string
  readonly seenAt: number
}

export interface Identity {
  readonly jid: string
  readonly name: string | null
  readonly phone: string | null
  readonly aliases: readonly string[]
}

export function isNameSource(value: string): value is NameSource {
  return Object.hasOwn(RANK, value)
}

// Case, accents and spacing do not make a different name.
export function foldName(name: string): string {
  return name.normalize("NFD").replace(/\p{M}/gu, "").toLowerCase().replace(/\s+/g, " ").trim()
}

function byRank(a: NameRecord, b: NameRecord): number {
  return RANK[a.source] - RANK[b.source] || b.seenAt - a.seenAt
}

export function resolveIdentity(
  jid: string,
  records: readonly NameRecord[],
  phone: string | null,
): Identity {
  const ranked = records.toSorted(byRank)
  const real = ranked.filter((record) => record.source !== "redacted")
  const name = real[0]?.name ?? phone ?? ranked[0]?.name ?? null

  const seen = new Set(name ? [foldName(name)] : [])
  const aliases: string[] = []
  // A masked phone says nothing a known phone does not.
  for (const record of phone ? real : ranked) {
    const folded = foldName(record.name)
    if (seen.has(folded)) continue
    seen.add(folded)
    aliases.push(record.name)
  }
  return { jid, name, phone, aliases }
}
