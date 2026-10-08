// The package exports the binary under a specifier without `.wasm`, which
// Wrangler's compiled-WASM rule never matches, so it is imported by path.
import { initSync } from "@oxidezap/whatsapp-rust-bridge/host"

import bridge from "../node_modules/@oxidezap/whatsapp-rust-bridge/dist/whatsapp_rust_bridge_bg.wasm"

initSync({ module: bridge })
