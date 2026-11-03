export interface MediaFile {
  readonly bytes: Uint8Array
  readonly mimetype: string
}

// Chunked so large files do not overflow the argument list.
export function toBase64(bytes: Uint8Array): string {
  let binary = ""
  for (let start = 0; start < bytes.length; start += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(start, start + 0x8000))
  }
  return btoa(binary)
}
