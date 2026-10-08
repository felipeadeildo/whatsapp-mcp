/**
 * Loads the whatsapp-rust bridge WASM once per isolate, before any socket exists.
 *
 * The package exports the binary as `@oxidezap/whatsapp-rust-bridge/wasm`, a
 * specifier without the `.wasm` suffix, so Wrangler's compiled-WASM rule never
 * matches it. Importing the file by path lets the rule apply.
 */
import { initSync } from "@oxidezap/whatsapp-rust-bridge/host"

import bridge from "../node_modules/@oxidezap/whatsapp-rust-bridge/dist/whatsapp_rust_bridge_bg.wasm"

initSync({ module: bridge })
