// Copies the editor's runtime into public/ so the dashboard serves it itself.
//
// @monaco-editor/react resolves Monaco at *runtime*, and its default source is
// a CDN — a hardcoded https://cdn.jsdelivr.net/npm/monaco-editor@x/min/vs, at a
// version that need not even match the one installed here. That made the SQL
// editor the one part of this product needing the operator's browser to reach
// the public internet, in a panel whose entire security story is a closed
// network perimeter. With the CDN unreachable the Query tab was a permanent
// "Loading…" — no error, no fallback, and no way to type a statement.
//
// Copied rather than bundled: min/vs ships its own AMD loader and its web
// workers and resolves both relative to the path it was loaded from, so serving
// the directory as it stands needs no bundler or worker configuration to get
// wrong. It is gitignored and rebuilt from node_modules by `bun run build`, so
// the tree carries a version number rather than 24 MB of vendored editor.
import { cp, rm, mkdir } from "node:fs/promises"
import { createRequire } from "node:module"
import { dirname, join, sep } from "node:path"

// The package root is found by resolving the main entry and walking back to
// the directory the package occupies. monaco-editor's exports map rewrites
// every subpath into esm/vs, so neither "monaco-editor/package.json" nor
// "monaco-editor/min/vs/loader.js" resolves — min/vs is simply not something
// the map admits exists, and it is the half being shipped.
const require = createRequire(import.meta.url)
const entry = require.resolve("monaco-editor")
const marker = `${sep}monaco-editor${sep}`
const root = entry.slice(0, entry.lastIndexOf(marker) + marker.length)
const src = join(root, "min", "vs")
const dest = join(process.cwd(), "public", "monaco", "vs")

await rm(dest, { recursive: true, force: true })
await mkdir(dirname(dest), { recursive: true })
await cp(src, dest, { recursive: true })
console.log(`monaco: ${src} -> ${dest}`)
