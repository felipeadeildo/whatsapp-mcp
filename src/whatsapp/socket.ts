/**
 * Builds the baileyrs socket for the Workers runtime with its full typings.
 *
 * The `/host` and Node entrypoints return the same object: both call
 * `createWASocketFactory(runtime)` from `Socket/core.js` and differ only in the
 * runtime they pass. The host declarations are trimmed to stay free of Node
 * types, which leaves most operations typed `(...args: unknown[]) => unknown`
 * and only three events typed. Viewing the socket through the Node
 * declarations restores the real signatures and the full event map.
 *
 * What does differ at runtime: the host has no filesystem and no Node media
 * processors, so operations that take a `Buffer`, a stream or a file path
 * (media uploads from disk, thumbnails) are not available here.
 */
import type { WASocket } from "@oxidezap/baileyrs"
import makeHostSocket, { type HostSocketConfig } from "@oxidezap/baileyrs/host"

export type Socket = WASocket

export function makeSocket(config: HostSocketConfig): Socket {
  const socket = makeHostSocket(config)
  // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- same object, richer declarations; see the module comment
  return socket as unknown as Socket
}
