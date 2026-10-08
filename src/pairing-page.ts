/**
 * Minimal page to link a WhatsApp account by QR code. Open it as
 * `/accounts/<id>#<API_KEY>`; the key stays in the fragment, out of server logs.
 */
export function pairingPage(accountId: string): string {
  return `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>WhatsApp MCP · ${accountId}</title>
<style>
  body { font: 16px/1.5 system-ui, sans-serif; max-width: 28rem; margin: 3rem auto; padding: 0 1rem; color: #111; }
  #qr svg { width: 100%; height: auto; }
  code { background: #f2f2f2; padding: .1rem .3rem; border-radius: .2rem; }
  button { font: inherit; padding: .4rem .9rem; }
</style>
<h1>Account <code>${accountId}</code></h1>
<p id="state">Loading...</p>
<div id="qr"></div>
<button id="start">Connect</button>
<script>
  const key = location.hash.slice(1)
  const base = location.pathname
  const stateEl = document.getElementById('state')
  const call = (path, method = 'GET') =>
    fetch(base + '/' + path, { method, headers: { authorization: 'Bearer ' + key } }).then((r) => r.json())

  async function refresh() {
    if (!key) { stateEl.textContent = 'Add #<API_KEY> to the URL.'; return }
    const s = await call('status')
    if (s.error) { stateEl.textContent = s.error; return }
    stateEl.textContent =
      s.state + (s.me ? ' as ' + s.me : '') + (s.lastError ? ' · ' + s.lastError : '')
    document.getElementById('qr').innerHTML = s.qrSvg ?? ''
  }

  document.getElementById('start').onclick = () => call('start', 'POST').then(refresh)
  refresh()
  setInterval(refresh, 3000)
</script>
</html>`
}
