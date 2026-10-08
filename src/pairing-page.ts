// Self-contained, so nothing on the page can leak the API key it reads from the
// URL fragment. The script avoids backticks and `${` to live inside a template.
const REPOSITORY_URL = "https://github.com/felipeadeildo/whatsapp-mcp"

const GITHUB_MARK = `<svg viewBox="0 0 16 16" fill="currentColor" aria-hidden="true"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"/></svg>`

const STYLE = `
:root {
  --paper: #0a1512;
  --surface: #11201b;
  --ink: #dce9e3;
  --muted: #8ba69d;
  --line: #24372f;
  --signal: #3cc488;
  --wait: #e0a43a;
  --alert: #f0766a;
  --display: ui-rounded, "SF Pro Rounded", "Nunito", system-ui, sans-serif;
  --text: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
  --code: ui-monospace, "SF Mono", "Cascadia Code", Menlo, monospace;
  color-scheme: dark;
}
* { box-sizing: border-box; }
body {
  margin: 0;
  min-height: 100vh;
  background: var(--paper);
  color: var(--ink);
  font: 1rem/1.55 var(--text);
}
main { max-width: 60rem; margin: 0 auto; padding: 2rem 1.5rem 4rem; }
.top { display: flex; align-items: center; justify-content: space-between; gap: 1rem; flex-wrap: wrap; }
.brand { display: flex; align-items: baseline; gap: 0.5rem; color: var(--muted); font-size: 0.95rem; }
.brand strong { color: var(--ink); font-weight: 600; }
.source {
  display: inline-flex;
  align-items: center;
  gap: 0.5rem;
  color: var(--muted);
  font-size: 0.92rem;
  text-decoration: none;
  border-radius: 0.4rem;
}
.source svg { width: 1.15rem; height: 1.15rem; }
.source:hover { color: var(--ink); }
.source:focus-visible { outline: 3px solid var(--signal); outline-offset: 3px; }
.hero {
  display: grid;
  gap: 2.5rem;
  margin-top: 3.5rem;
  align-items: center;
}
@media (min-width: 48rem) {
  .hero { grid-template-columns: minmax(0, 1fr) 19rem; gap: 4rem; }
}
h1 {
  margin: 0;
  font: 650 clamp(1.9rem, 4.2vw, 2.7rem)/1.1 var(--display);
  letter-spacing: -0.015em;
  text-wrap: balance;
}
.who { margin: 0.6rem 0 0; color: var(--muted); font-variant-numeric: tabular-nums; }
.lead { margin: 1.1rem 0 0; max-width: 34rem; font-size: 1.08rem; text-wrap: pretty; }
.lead strong { font-variant-numeric: tabular-nums; }
.steps { margin: 1.6rem 0 0; padding: 0; list-style: none; counter-reset: step; max-width: 32rem; }
.steps li {
  counter-increment: step;
  display: grid;
  grid-template-columns: 1.9rem 1fr;
  gap: 0.6rem;
  padding: 0.55rem 0;
  border-top: 1px solid var(--line);
}
.steps li::before {
  content: counter(step);
  font: 650 1.05rem/1.5 var(--display);
  color: var(--signal);
}
.note { margin: 1rem 0 0; color: var(--muted); font-size: 0.92rem; }
.detail {
  margin: 1.1rem 0 0;
  color: var(--alert);
  font-size: 0.92rem;
  overflow-wrap: anywhere;
}
.actions { display: flex; flex-wrap: wrap; gap: 0.75rem; margin-top: 1.75rem; }
.actions:empty { display: none; }
button {
  font: 600 1rem/1 var(--text);
  border-radius: 0.6rem;
  padding: 0.85rem 1.3rem;
  border: 1.5px solid transparent;
  cursor: pointer;
}
button:focus-visible { outline: 3px solid var(--signal); outline-offset: 2px; }
button:disabled { cursor: progress; opacity: 0.7; }
.primary { background: var(--ink); color: var(--paper); }
.primary:hover:not(:disabled) { background: var(--signal); }
.quiet { background: transparent; color: var(--alert); border-color: var(--line); }
.quiet:hover:not(:disabled) { border-color: var(--alert); }

.viewfinder {
  --corner: var(--line);
  position: relative;
  aspect-ratio: 1;
  width: min(100%, 19rem);
  justify-self: center;
  display: grid;
  place-items: center;
  padding: 1.4rem;
  background:
    linear-gradient(var(--corner) 0 0) top left / 2.2rem 4px,
    linear-gradient(var(--corner) 0 0) top left / 4px 2.2rem,
    linear-gradient(var(--corner) 0 0) top right / 2.2rem 4px,
    linear-gradient(var(--corner) 0 0) top right / 4px 2.2rem,
    linear-gradient(var(--corner) 0 0) bottom left / 2.2rem 4px,
    linear-gradient(var(--corner) 0 0) bottom left / 4px 2.2rem,
    linear-gradient(var(--corner) 0 0) bottom right / 2.2rem 4px,
    linear-gradient(var(--corner) 0 0) bottom right / 4px 2.2rem,
    var(--surface);
  background-repeat: no-repeat;
  border-radius: 0.35rem;
}
.viewfinder[data-mode="qr"] { --corner: var(--ink); }
.viewfinder[data-mode="wait"] { --corner: var(--wait); }
.viewfinder[data-mode="done"] { --corner: var(--signal); }
.viewfinder[data-mode="alert"] { --corner: var(--alert); }
.visual { width: 100%; height: 100%; display: grid; place-items: center; }
.visual svg { width: 100%; height: auto; display: block; }
.visual .qr { background: #fff; padding: 0.4rem; border-radius: 0.25rem; }
.visual .glyph { width: 42%; color: var(--corner); }
.viewfinder[data-mode="idle"] .glyph { color: var(--muted); }
.fresh { animation: fresh 0.35s ease-out; }
@keyframes fresh { from { opacity: 0.25; } }
.spinner {
  width: 3.2rem;
  height: 3.2rem;
  border-radius: 50%;
  border: 4px solid var(--line);
  border-top-color: var(--wait);
  animation: spin 0.9s linear infinite;
}
@keyframes spin { to { transform: rotate(1turn); } }
.check path { stroke-dasharray: 60; stroke-dashoffset: 0; animation: draw 0.5s ease-out; }
@keyframes draw { from { stroke-dashoffset: 60; } }

.connect { margin-top: 4rem; padding-top: 2rem; border-top: 1px solid var(--line); }
.connect h2 { margin: 0 0 0.4rem; font: 650 1.35rem/1.2 var(--display); }
.connect > p { margin: 0 0 1.4rem; color: var(--muted); max-width: 40rem; }
.field { margin-top: 1rem; }
.field-label { margin: 0 0 0.35rem; font-weight: 600; }
.copyable { display: flex; gap: 0.6rem; align-items: stretch; }
.copyable code {
  flex: 1;
  min-width: 0;
  padding: 0.7rem 0.85rem;
  background: var(--surface);
  border: 1px solid var(--line);
  border-radius: 0.5rem;
  font: 0.88rem/1.5 var(--code);
  overflow-wrap: anywhere;
}
.copyable button { padding: 0.6rem 1rem; background: transparent; color: var(--ink); border-color: var(--line); }
.copyable button:hover { border-color: var(--ink); }
[hidden] { display: none !important; }
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after { animation: none !important; transition: none !important; }
}
`

const SCRIPT = String.raw`
const TEXT = {
  en: {
    loading: ["Checking the connection", "One moment."],
    missingKey: ["Add your API key to the address", "Open this page as /accounts/{id}#YOUR_API_KEY. Everything after # stays in your browser and is never sent to the server."],
    unauthorized: ["This API key was not accepted", "Check the key after # in the address. It must match the API_KEY secret of this deployment."],
    offline: ["The server is not answering", "Trying again in a few seconds."],
    notLinked: ["Not linked to WhatsApp yet", "Link this account so your agents can read and send its messages. Have your phone at hand."],
    unlinked: ["Your phone unlinked this device", "Its archive was cleared. Link it again to continue."],
    connecting: ["Connecting to WhatsApp", "This takes a few seconds."],
    pairing: ["Scan the code with your phone", "The code changes every few seconds, so keep this page open until your phone confirms."],
    steps: ["Open WhatsApp on your phone.", "Go to Settings, then Linked devices.", "Tap Link a device and point the camera at the code."],
    phoneNote: "On a phone? Open this page on a computer and scan the code with your phone.",
    linked: "Linked as {name}",
    archived: "Your agents can read <strong>{messages}</strong> messages from <strong>{chats}</strong> chats, back to {date}.",
    syncing: "Your phone is sending the history now. Keep WhatsApp open on it; this can take a few minutes.",
    reconnecting: ["Reconnecting to WhatsApp", "The connection dropped. It comes back on its own within a minute."],
    lastError: "Last error: ",
    link: "Link WhatsApp",
    linking: "Linking…",
    unlink: "Unlink this device",
    unlinking: "Unlinking…",
    confirmUnlink: "Unlink WhatsApp from this server? Its archive is deleted, and linking again means scanning a new code.",
    copy: "Copy",
    copied: "Copied",
    connect: ["Connect your agent", "Any MCP client works: point it at the endpoint and send the API key as a Bearer token in the Authorization header."],
    endpoint: "MCP endpoint",
    claude: "Claude Code",
  },
  pt: {
    loading: ["Verificando a conexão", "Um instante."],
    missingKey: ["Coloque sua chave de API no endereço", "Abra esta página como /accounts/{id}#SUA_CHAVE. O que vem depois do # fica no seu navegador e nunca vai para o servidor."],
    unauthorized: ["Esta chave de API não foi aceita", "Confira a chave depois do # no endereço. Ela precisa ser igual ao secret API_KEY deste deploy."],
    offline: ["O servidor não está respondendo", "Tentando de novo em alguns segundos."],
    notLinked: ["Ainda não conectado ao WhatsApp", "Conecte esta conta para que seus agentes leiam e enviem mensagens por ela. Tenha o celular em mãos."],
    unlinked: ["Seu celular desconectou este aparelho", "O arquivo dele foi apagado. Conecte de novo para continuar."],
    connecting: ["Conectando ao WhatsApp", "Isso leva alguns segundos."],
    pairing: ["Escaneie o código com o celular", "O código muda a cada poucos segundos, então deixe esta página aberta até o celular confirmar."],
    steps: ["Abra o WhatsApp no celular.", "Vá em Configurações e depois em Aparelhos conectados.", "Toque em Conectar um aparelho e aponte a câmera para o código."],
    phoneNote: "Está no celular? Abra esta página num computador e escaneie o código com o celular.",
    linked: "Conectado como {name}",
    archived: "Seus agentes podem ler <strong>{messages}</strong> mensagens de <strong>{chats}</strong> conversas, desde {date}.",
    syncing: "Seu celular está enviando o histórico agora. Deixe o WhatsApp aberto nele; pode levar alguns minutos.",
    reconnecting: ["Reconectando ao WhatsApp", "A conexão caiu. Ela volta sozinha em até um minuto."],
    lastError: "Último erro: ",
    link: "Conectar WhatsApp",
    linking: "Conectando…",
    unlink: "Desconectar este aparelho",
    unlinking: "Desconectando…",
    confirmUnlink: "Desconectar o WhatsApp deste servidor? O arquivo é apagado, e conectar de novo exige escanear um novo código.",
    copy: "Copiar",
    copied: "Copiado",
    connect: ["Conecte seu agente", "Qualquer cliente MCP serve: aponte para o endpoint e envie a chave de API como token Bearer no cabeçalho Authorization."],
    endpoint: "Endpoint MCP",
    claude: "Claude Code",
  },
}

const GLYPHS = {
  done: '<svg class="glyph check" viewBox="0 0 48 48" fill="none" aria-hidden="true"><path d="M10 25l9 9 19-20" stroke="currentColor" stroke-width="5" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  alert: '<svg class="glyph" viewBox="0 0 48 48" fill="none" aria-hidden="true"><path d="M24 12v15" stroke="currentColor" stroke-width="5" stroke-linecap="round"/><circle cx="24" cy="36" r="3.2" fill="currentColor"/></svg>',
  idle: '<svg class="glyph" viewBox="0 0 48 48" fill="none" aria-hidden="true"><rect x="14" y="6" width="20" height="36" rx="4" stroke="currentColor" stroke-width="3.5"/><path d="M21 35h6" stroke="currentColor" stroke-width="3.5" stroke-linecap="round"/></svg>',
  spin: '<div class="spinner" role="presentation"></div>',
}

const lang = (navigator.language || "en").toLowerCase().startsWith("pt") ? "pt" : "en"
const t = TEXT[lang]
const account = document.body.dataset.account
const key = decodeURIComponent(location.hash.slice(1))
const base = location.pathname.endsWith("/") ? location.pathname.slice(0, -1) : location.pathname
const number = new Intl.NumberFormat(lang === "pt" ? "pt-BR" : "en")
const day = new Intl.DateTimeFormat(lang === "pt" ? "pt-BR" : "en", { dateStyle: "long" })
const $ = (id) => document.getElementById(id)
let timer = null
let lastQr = null
let busy = false

document.documentElement.lang = lang === "pt" ? "pt-BR" : "en"
$("account").textContent = account

function fill(template, values) {
  return template.replace(/\{(\w+)\}/g, (_, name) => values[name])
}

async function call(path, method) {
  const response = await fetch(base + "/" + path, {
    method: method || "GET",
    headers: { authorization: "Bearer " + key },
  })
  if (response.status === 401) throw new Error("unauthorized")
  const body = await response.json()
  if (!response.ok) throw new Error(body.error || response.statusText)
  return body
}

function show(view) {
  $("title").textContent = view.title
  $("lead").innerHTML = view.lead
  $("who").hidden = !view.who
  $("who").textContent = view.who || ""
  $("steps").hidden = !view.steps
  $("note").hidden = !view.steps
  $("detail").hidden = !view.detail
  $("detail").textContent = view.detail ? t.lastError + view.detail : ""
  $("connect").hidden = !view.connect
  $("unlink").hidden = !view.connect
  $("link").hidden = !view.link

  const finder = $("viewfinder")
  finder.dataset.mode = view.mode
  const visual = $("visual")
  if (view.qr) {
    if (view.qr !== lastQr) {
      visual.innerHTML = '<div class="qr fresh">' + view.qr + "</div>"
      lastQr = view.qr
    }
  } else {
    lastQr = null
    const glyph = GLYPHS[view.glyph]
    if (visual.dataset.glyph !== view.glyph) {
      visual.innerHTML = glyph
      visual.dataset.glyph = view.glyph
    }
  }
  if (view.qr) visual.dataset.glyph = ""
}

function escapeHtml(text) {
  const span = document.createElement("span")
  span.textContent = text
  return span.innerHTML
}

function viewFor(status) {
  switch (status.state) {
    case "open": {
      const archive = status.archive
      const lead = archive.messages > 0
        ? fill(t.archived, {
            messages: number.format(archive.messages),
            chats: number.format(archive.chats),
            date: day.format(new Date(archive.oldestMessageAt * 1000)),
          })
        : escapeHtml(t.syncing)
      const phone = status.me ? "+" + status.me.split("@")[0] : null
      return {
        title: fill(t.linked, { name: status.myName || phone || account }),
        who: status.myName ? phone : null,
        lead,
        mode: "done",
        glyph: "done",
        connect: true,
      }
    }
    case "pairing":
      return { title: t.pairing[0], lead: escapeHtml(t.pairing[1]), mode: "qr", qr: status.qrSvg, steps: true }
    case "connecting":
      return { title: t.connecting[0], lead: escapeHtml(t.connecting[1]), mode: "wait", glyph: "spin" }
    case "closed":
      return {
        title: t.reconnecting[0],
        lead: escapeHtml(t.reconnecting[1]),
        mode: "wait",
        glyph: "spin",
        detail: status.lastError,
      }
    case "logged_out":
      if (status.lastError) {
        return { title: t.unlinked[0], lead: escapeHtml(t.unlinked[1]), mode: "alert", glyph: "alert", link: true }
      }
      return { title: t.notLinked[0], lead: escapeHtml(t.notLinked[1]), mode: "idle", glyph: "idle", link: true }
    default:
      return { title: t.notLinked[0], lead: escapeHtml(t.notLinked[1]), mode: "idle", glyph: "idle", link: true }
  }
}

function schedule(ms) {
  clearTimeout(timer)
  if (!document.hidden) timer = setTimeout(refresh, ms)
}

async function refresh() {
  try {
    const status = await call("status")
    if (!busy) show(viewFor(status))
    schedule(status.state === "open" ? 5000 : 2000)
  } catch (error) {
    if (error.message === "unauthorized") {
      show({ title: t.unauthorized[0], lead: escapeHtml(t.unauthorized[1]), mode: "alert", glyph: "alert" })
      return
    }
    show({ title: t.offline[0], lead: escapeHtml(t.offline[1]), mode: "alert", glyph: "alert", detail: error.message })
    schedule(5000)
  }
}

async function act(button, path, pendingLabel) {
  const label = button.textContent
  busy = true
  button.disabled = true
  button.textContent = pendingLabel
  try {
    await call(path, "POST")
  } catch (error) {
    $("detail").hidden = false
    $("detail").textContent = t.lastError + error.message
  } finally {
    busy = false
    button.disabled = false
    button.textContent = label
    refresh()
  }
}

function copyButton(button, getText) {
  button.textContent = t.copy
  button.addEventListener("click", async () => {
    await navigator.clipboard.writeText(getText())
    button.textContent = t.copied
    setTimeout(() => { button.textContent = t.copy }, 1600)
  })
}

const endpoint = location.origin + base + "/mcp"
const command = "claude mcp add --transport http whatsapp " + endpoint + ' --header "Authorization: Bearer KEY"'
$("endpoint").textContent = endpoint
$("command").textContent = command.replace("KEY", "••••••••")
copyButton($("copy-endpoint"), () => endpoint)
copyButton($("copy-command"), () => command.replace("KEY", key))
$("connect-title").textContent = t.connect[0]
$("connect-lead").textContent = t.connect[1]
$("endpoint-label").textContent = t.endpoint
$("command-label").textContent = t.claude
$("note").textContent = t.phoneNote
t.steps.forEach((step) => {
  const item = document.createElement("li")
  item.textContent = step
  $("steps").append(item)
})

$("link").textContent = t.link
$("link").addEventListener("click", () => act($("link"), "start", t.linking))
$("unlink").textContent = t.unlink
$("unlink").addEventListener("click", () => {
  if (confirm(t.confirmUnlink)) act($("unlink"), "logout", t.unlinking)
})
document.addEventListener("visibilitychange", () => {
  if (!document.hidden) refresh()
})

if (key) {
  show({ title: t.loading[0], lead: escapeHtml(t.loading[1]), mode: "wait", glyph: "spin" })
  refresh()
} else {
  show({
    title: t.missingKey[0],
    lead: escapeHtml(fill(t.missingKey[1], { id: account })),
    mode: "alert",
    glyph: "alert",
  })
}
`

export function pairingPage(accountId: string): string {
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<meta name="theme-color" content="#0a1512">
<title>whatsapp-mcp / ${accountId}</title>
<style>${STYLE}</style>
</head>
<body data-account="${accountId}">
<main>
  <header class="top">
    <div class="brand"><strong>whatsapp-mcp</strong><span>/</span><span id="account"></span></div>
    <a class="source" href="${REPOSITORY_URL}" target="_blank" rel="noopener noreferrer">${GITHUB_MARK}<span>felipeadeildo/whatsapp-mcp</span></a>
  </header>
  <section class="hero">
    <div>
      <h1 id="title" aria-live="polite"></h1>
      <p id="who" class="who" hidden></p>
      <p id="lead" class="lead"></p>
      <ol id="steps" class="steps" hidden></ol>
      <p id="note" class="note" hidden></p>
      <p id="detail" class="detail" hidden></p>
      <div class="actions">
        <button id="link" class="primary" type="button" hidden></button>
        <button id="unlink" class="quiet" type="button" hidden></button>
      </div>
    </div>
    <div id="viewfinder" class="viewfinder" data-mode="wait">
      <div id="visual" class="visual"></div>
    </div>
  </section>
  <section id="connect" class="connect" hidden>
    <h2 id="connect-title"></h2>
    <p id="connect-lead"></p>
    <div class="field">
      <p id="endpoint-label" class="field-label"></p>
      <div class="copyable"><code id="endpoint"></code><button id="copy-endpoint" type="button"></button></div>
    </div>
    <div class="field">
      <p id="command-label" class="field-label"></p>
      <div class="copyable"><code id="command"></code><button id="copy-command" type="button"></button></div>
    </div>
  </section>
</main>
<script>${SCRIPT}</script>
</body>
</html>`
}
