// WhatsApp rejects stale WA Web builds ("Client outdated"). Baileys reads the
// current build from WA Web's service worker, but that helper is not in /host.
type WaVersion = [number, number, number]

const SERVICE_WORKER_URL = "https://web.whatsapp.com/sw.js"
const CLIENT_REVISION = /\\?"client_revision\\?":\s*(\d+)/

// `undefined` keeps the library default.
export async function fetchWaWebVersion(): Promise<WaVersion | undefined> {
  const response = await fetch(SERVICE_WORKER_URL, {
    headers: {
      "sec-fetch-site": "none",
      "user-agent":
        "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
    },
  })
  if (!response.ok) return undefined
  const revision = CLIENT_REVISION.exec(await response.text())?.[1]
  return revision ? [2, 3000, Number(revision)] : undefined
}
