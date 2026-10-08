import { describe, expect, test } from "bun:test"

import { foldName, resolveIdentity } from "../src/store/names"

describe("resolveIdentity", () => {
  test("ranks by source, then by recency within a source", () => {
    const identity = resolveIdentity(
      "1@lid",
      [
        { source: "push", name: "Maria", seenAt: 100 },
        { source: "push", name: "Mah", seenAt: 200 },
        { source: "business", name: "Padaria", seenAt: 300 },
      ],
      null,
    )
    expect(identity).toEqual({
      jid: "1@lid",
      name: "Mah",
      phone: null,
      aliases: ["Maria", "Padaria"],
    })
  })

  test("aliases skip spellings of names already listed", () => {
    const identity = resolveIdentity(
      "1@lid",
      [
        { source: "contact", name: "João", seenAt: 1 },
        { source: "push", name: "joao", seenAt: 1 },
        { source: "history", name: "JOÃO ", seenAt: 1 },
      ],
      null,
    )
    expect(identity.aliases).toEqual([])
  })

  test("the phone comes before a masked phone, which is dropped once the phone is known", () => {
    const masked = [{ source: "redacted" as const, name: "+55∙∙∙∙99", seenAt: 1 }]
    expect(resolveIdentity("1@lid", masked, null).name).toBe("+55∙∙∙∙99")
    expect(resolveIdentity("1@lid", masked, "+5511999999999")).toMatchObject({
      name: "+5511999999999",
      aliases: [],
    })
  })
})

describe("foldName", () => {
  test("ignores case, accents and spacing", () => {
    expect(foldName("  Antônio   JOSÉ ")).toBe("antonio jose")
  })
})
