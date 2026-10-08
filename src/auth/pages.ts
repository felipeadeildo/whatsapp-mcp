// OAuth clients choose their own name and redirect address, so escape everything.
import type { ConsentDescription } from "@cloudflare/workers-oauth-provider"

import { STYLE } from "../pairing-page"
import { type Lang, LOCALE, TEXT } from "../text"

type Message = keyof (typeof TEXT)["en"]["auth"]

function escapeHtml(text: string): string {
  return text.replace(/[&<>"']/g, (char) => `&#${char.charCodeAt(0)};`)
}

type Values = Record<string, string>

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

export function signInPage(options: {
  lang: Lang
  accountId: string
  next: string
  error?: "wrong" | "tooMany"
}): string {
  const { lang, accountId, next, error } = options
  return shell(
    lang,
    plain(lang, "signIn"),
    `
    <h1>${say(lang, "signIn")}</h1>
    <p class="lead">${say(lang, "signInLead", { account: accountId })}</p>
    ${error ? `<p class="detail" role="alert">${say(lang, error)}</p>` : ""}
    <form method="post" action="/accounts/${accountId}/login" class="form">
      <input type="hidden" name="next" value="${escapeHtml(next)}">
      <label for="secret">${say(lang, "password")}</label>
      <input id="secret" name="secret" type="password" autocomplete="current-password" required autofocus>
      <div class="actions"><button class="primary" type="submit">${say(lang, "submit")}</button></div>
    </form>`,
  )
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
  return shell(
    lang,
    plain(lang, "allowTitle", { client: details.clientName }),
    `
    <h1>${say(lang, "allowTitle", { client: details.clientName })}</h1>
    <p class="lead">${say(lang, "allowLead", { account: accountId })}</p>
    <p class="note">${origin} ${say(lang, "sendsTo", { host: details.redirectHost })}</p>
    ${details.redirectIsLoopback ? `<p class="detail">${say(lang, "loopback")}</p>` : ""}
    <form method="post" class="form">
      <input type="hidden" name="handle" value="${escapeHtml(handle)}">
      <div class="actions">
        <button class="primary" name="decision" value="approve" type="submit">${say(lang, "allow")}</button>
        <button class="quiet" name="decision" value="deny" type="submit">${say(lang, "deny")}</button>
      </div>
    </form>`,
  )
}

export function failurePage(lang: Lang, description: string): string {
  return shell(
    lang,
    plain(lang, "failed"),
    `
    <h1>${say(lang, "failed")}</h1>
    <p class="detail">${escapeHtml(description)}</p>`,
  )
}

function shell(lang: Lang, title: string, body: string): string {
  return `<!doctype html>
<html lang="${LOCALE[lang]}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<meta name="theme-color" content="#0a1512">
<title>whatsapp-mcp / ${escapeHtml(title)}</title>
<style>${STYLE}</style>
</head>
<body>
<main class="narrow">
  <header class="top"><div class="brand"><strong>whatsapp-mcp</strong></div></header>
  <section class="card">${body}</section>
</main>
</body>
</html>`
}
