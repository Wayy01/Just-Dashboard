// Copy Monaco's own build into public/, so the editor is served by this
// dashboard rather than fetched from a CDN.
//
// @monaco-editor/react loads the editor at runtime from
// cdn.jsdelivr.net unless it is told otherwise. For a panel that drives the
// Docker socket, systemd and a root shell, that is third-party JavaScript
// executing in the same origin as a root-equivalent session — and it means
// every editor in the product (files, compose, nginx, the site preview) is a
// spinner that never resolves on a machine reached over Tailscale from a
// workstation with no egress. Both are answered by shipping the files.
//
// Run from predev/prebuild and from the Dockerfile, because the image builds
// by invoking next's entrypoint directly and never sees the npm hooks.

import { cp, mkdir, rm, stat } from "node:fs/promises"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"

const root = join(dirname(fileURLToPath(import.meta.url)), "..")
const src = join(root, "node_modules", "monaco-editor", "min", "vs")
const dest = join(root, "public", "monaco", "vs")

try {
  await stat(src)
} catch {
  console.error("monaco-editor is not installed; run the package manager first")
  process.exit(1)
}

// The version is part of the path Monaco resolves its workers against, so a
// stale copy is worse than none: replace rather than merge.
await rm(dest, { recursive: true, force: true })
await mkdir(dirname(dest), { recursive: true })
await cp(src, dest, { recursive: true })
console.log(`monaco: ${src} → ${dest}`)
