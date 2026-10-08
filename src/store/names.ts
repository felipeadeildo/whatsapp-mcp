/**
 * Picks what to call a person, group or channel from every name ever seen for
 * it. Names are kept per source (the address book, the name people chose on
 * WhatsApp, a group's subject...) instead of overwriting one another, so a
 * rename or a phone/LID merge loses nothing and search can match any of them.
 */

/**
 * Where a name came from, and its rank (lower is better):
 * - `contact`: the account owner's address book
 * - `push`: the name the person set on WhatsApp
 * - `business`: a verified business name
 * - `subject`: a group's or channel's subject
 * - `history`: what history sync called the conversation when it was none of the above
 * - `username`: a WhatsApp username
 * - `redacted`: a masked phone ("+55∙∙∙∙∙∙∙∙08"), all WhatsApp shows of non-contacts in groups
 */
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
  /** Unix seconds; among names of the same source, the latest wins. */
  readonly seenAt: number
}

export interface Identity {
  readonly jid: string
  /** What to call them: the best name, else their phone, else the masked phone. */
  readonly name: string | null
  readonly phone: string | null
  /** Their other names, deduplicated, best first. */
  readonly aliases: readonly string[]
}

export function isNameSource(value: string): value is NameSource {
  return Object.hasOwn(RANK, value)
}

/** Case, accents and spacing do not make a different name. */
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
