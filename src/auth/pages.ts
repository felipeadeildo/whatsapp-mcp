// OAuth clients choose their own name and redirect address, so escape everything.
import type { ConsentDescription } from "@cloudflare/workers-oauth-provider"

import { escapeHtml, page } from "../page"
import { type Lang, TEXT } from "../text"

type Message = keyof (typeof TEXT)["en"]["auth"]
type Values = Record<string, string>

const LOCK = `<svg class="glyph" viewBox="0 0 48 48" fill="none" aria-hidden="true"><rect x="11" y="21" width="26" height="20" rx="4" stroke="currentColor" stroke-width="3.5"/><path d="M17 21v-5a7 7 0 0 1 14 0v5" stroke="currentColor" stroke-width="3.5" stroke-linecap="round"/><circle cx="24" cy="31" r="2.6" fill="currentColor"/></svg>`
const ALERT = `<svg class="glyph" viewBox="0 0 48 48" fill="none" aria-hidden="true"><path d="M24 12v15" stroke="currentColor" stroke-width="5" stroke-linecap="round"/><circle cx="24" cy="36" r="3.2" fill="currentColor"/></svg>`

/** The message as plain text, for the page title. */
function plain(lang: Lang, message: Message, values: Values = {}): string {
  return TEXT[lang].auth[message].replace(/\{(\w+)\}/g, (_, name: string) => values[name] ?? "")
}

/** The message as HTML, with each value escaped and in bold. */
function say(lang: Lang, message: Message, values: Values = {}): string {
  return escapeHtml(TEXT[lang].auth[message]).replace(/\{(\w+)\}/g, (_, name: string) => {
    return `<strong>${escapeHtml(values[name] ?? "")}</strong>`
  })
}

// The pairing page's viewfinder, as decoration beside the text; it hides on phones.
function viewfinder(mode: "idle" | "qr" | "alert", content: string): string {
  return `<div class="viewfinder aside" data-mode="${mode}"><div class="visual">${content}</div></div>`
}

export function signInPage(options: {
  lang: Lang
  accountId: string
  next: string
  error?: "wrong" | "tooMany"
}): string {
  const { lang, accountId, next, error } = options
  return page({
    lang,
    title: plain(lang, "signIn"),
    accountId,
    main: `
  <section class="hero">
    <div>
      <h1>${say(lang, "signIn")}</h1>
      <p class="lead">${say(lang, "signInLead", { account: accountId })}</p>
      <form method="post" action="/accounts/${accountId}/login" class="form">
        <input type="hidden" name="next" value="${escapeHtml(next)}">
        <label for="secret">${say(lang, "password")}</label>
        <input id="secret" name="secret" type="password" autocomplete="current-password" required autofocus>
        ${error ? `<p class="detail" role="alert">${say(lang, error)}</p>` : ""}
        <div class="actions"><button class="primary" type="submit">${say(lang, "submit")}</button></div>
      </form>
    </div>
    ${viewfinder("idle", LOCK)}
  </section>`,
  })
}

export function consentPage(options: {
  lang: Lang
  accountId: string
  details: ConsentDescription
  handle: string
}): string {
  const { lang, accountId, details, handle } = options
  const origin = details.clientDomain
    ? say(lang, "publishedBy", { domain: details.clientDomain })
    : say(lang, "selfRegistered")
  const initial = Array.from(details.clientName.trim())[0]?.toUpperCase() ?? "?"
  return page({
    lang,
    title: plain(lang, "allowTitle", { client: details.clientName }),
    accountId,
    main: `
  <section class="hero">
    <div>
      <h1>${say(lang, "allowTitle", { client: details.clientName })}</h1>
      <p class="lead">${say(lang, "allowLead", { account: accountId })}</p>
      <p class="note">${origin} ${say(lang, "sendsTo", { host: details.redirectHost })}</p>
      ${details.redirectIsLoopback ? `<p class="detail">${say(lang, "loopback")}</p>` : ""}
      <form method="post" class="form">
        <input type="hidden" name="handle" value="${escapeHtml(handle)}">
        <div class="actions">
          <button class="primary" name="decision" value="approve" type="submit">${say(lang, "allow")}</button>
          <button class="secondary" name="decision" value="deny" type="submit">${say(lang, "deny")}</button>
        </div>
      </form>
    </div>
    ${viewfinder("qr", `<span class="monogram" aria-hidden="true">${escapeHtml(initial)}</span>`)}
  </section>`,
  })
}

export function failurePage(lang: Lang, description: string): string {
  return page({
    lang,
    title: plain(lang, "failed"),
    main: `
  <section class="hero">
    <div>
      <h1>${say(lang, "failed")}</h1>
      <p class="detail">${escapeHtml(description)}</p>
    </div>
    ${viewfinder("alert", ALERT)}
  </section>`,
  })
}
