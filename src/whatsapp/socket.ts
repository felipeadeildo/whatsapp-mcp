// The host and Node entrypoints build the same socket, but the host declarations
// drop Node types and leave most operations untyped. The Node declarations restore
// them. Inputs that need Node (Buffer, streams, file paths) still fail here.
import type { WASocket } from "@oxidezap/baileyrs"
import makeHostSocket, { type HostSocketConfig } from "@oxidezap/baileyrs/host"

export type Socket = WASocket

export function makeSocket(config: HostSocketConfig): Socket {
  const socket = makeHostSocket(config)
  // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- same object, see above
  return socket as unknown as Socket
}
