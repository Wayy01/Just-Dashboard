"use client"

import {
  AcronymCsv,
  AcronymGif,
  AcronymJpg,
  AcronymJson,
  AcronymMarkdown,
  AcronymSvg,
  Archive,
  BlendMode,
  Box,
  Code,
  CodeBracket,
  Cpu,
  Database,
  File,
  FileText,
  FileZip,
  FolderClosed,
  FolderMinus,
  FolderOpen,
  GitBranch,
  Globe,
  Home,
  Image,
  Key,
  Link,
  LockClosed,
  Logs,
  Music,
  Puzzle,
  Servers,
  SettingsGear,
  Terminal,
  TextFormat,
  Video,
  type Icon,
} from "@/components/icons"
import { cn } from "@/lib/utils"
import type { FileEntry } from "@/lib/types"

/**
 * What a file *is*, drawn.
 *
 * Every row in this page used to carry one of three glyphs — folder, link, or
 * the generic sheet of paper — which meant a directory of forty files was
 * forty identical icons and the only way to tell a config from a certificate
 * from a database dump was to read the extension off the end of the name. An
 * icon that says nothing is worse than no icon: it costs the same space and
 * trains you to ignore the column.
 *
 * The colours are the terminal rail's `--tag-*` tokens rather than anything
 * computed from the palette, for the same reason they are there: a category
 * is a label, and a label whose hue changes with the theme stops being the
 * same label. They are fixed hues that hold up on a near-black card and a
 * near-white one alike.
 *
 * The mapping is deliberately by *category* rather than by language. Twenty
 * distinct icons is a legend to memorise; eight categories with one hue each
 * is something the eye picks up in a directory or two — code is blue, data is
 * amber, media is pink, keys and certificates are green because they are the
 * ones you must not paste into a chat window.
 *
 * **The acronym badges are the one exception, and they do not break that
 * rule — they sidestep it.** Geist draws a small lettered plate for a handful
 * of formats (`JSON`, `CSV`, `MD`, `SVG`, `JPG`, `GIF`), and a badge that
 * spells the extension is the opposite of a legend: there is nothing to
 * memorise, it says what it is. They keep their category's hue, so the colour
 * system is untouched and a `.csv` is still the green of tabular data. The
 * language-branded plates in the same family — the solid `TS` and `JS` — are
 * deliberately *not* used: they are filled where every other glyph here is an
 * outline, so in a directory of source files they shout over their neighbours
 * and undo the even rhythm the categories exist to produce. Source is `Code`
 * in code blue, whatever language it is written in.
 *
 * The glyphs are Geist and drawn on a 16px grid, which is the size a row
 * renders them at — see `components/icons.tsx`.
 */
export type FileTone =
  | "slate"
  | "red"
  | "amber"
  | "green"
  | "cyan"
  | "blue"
  | "violet"
  | "pink"
  | "primary"

export type FileKind = {
  icon: Icon
  tone: FileTone
  /** What this is, in the words somebody would use out loud. */
  label: string
}

const CODE: FileKind = { icon: Code, tone: "blue", label: "Source code" }
const MARKUP: FileKind = { icon: Globe, tone: "violet", label: "Markup" }
const STYLE: FileKind = { icon: BlendMode, tone: "violet", label: "Stylesheet" }
const DATA: FileKind = { icon: CodeBracket, tone: "amber", label: "Structured data" }
const JSON_: FileKind = { icon: AcronymJson, tone: "amber", label: "JSON" }
const SHEET: FileKind = { icon: AcronymCsv, tone: "green", label: "Tabular data" }
const SQL: FileKind = { icon: Database, tone: "amber", label: "SQL" }
const DOC: FileKind = { icon: FileText, tone: "slate", label: "Document" }
const MARKDOWN: FileKind = { icon: AcronymMarkdown, tone: "slate", label: "Markdown" }
const IMAGE: FileKind = { icon: Image, tone: "pink", label: "Image" }
const SVG: FileKind = { icon: AcronymSvg, tone: "pink", label: "Vector image" }
const JPEG: FileKind = { icon: AcronymJpg, tone: "pink", label: "Image" }
const GIF: FileKind = { icon: AcronymGif, tone: "pink", label: "Animation" }
const VIDEO: FileKind = { icon: Video, tone: "violet", label: "Video" }
const AUDIO: FileKind = { icon: Music, tone: "cyan", label: "Audio" }
const ARCHIVE: FileKind = { icon: FileZip, tone: "amber", label: "Archive" }
const SHELL: FileKind = { icon: Terminal, tone: "green", label: "Shell script" }
const CONFIG: FileKind = { icon: SettingsGear, tone: "cyan", label: "Configuration" }
const SECRET: FileKind = { icon: Key, tone: "green", label: "Key or certificate" }
const BINARY: FileKind = { icon: Cpu, tone: "red", label: "Binary" }
const FONT: FileKind = { icon: TextFormat, tone: "violet", label: "Font" }
const LOG: FileKind = { icon: Logs, tone: "amber", label: "Log" }
const PACKAGE: FileKind = { icon: Puzzle, tone: "red", label: "Package" }
const DOCKER: FileKind = { icon: Box, tone: "blue", label: "Container build" }
const PLAIN: FileKind = { icon: File, tone: "slate", label: "File" }

const BY_EXTENSION: Record<string, FileKind> = {
  // Code
  go: CODE, rs: CODE, py: CODE, rb: CODE, php: CODE, java: CODE, kt: CODE,
  swift: CODE, c: CODE, h: CODE, cpp: CODE, cc: CODE, hpp: CODE, cs: CODE,
  ts: CODE, tsx: CODE, js: CODE, jsx: CODE, mjs: CODE, cjs: CODE, mts: CODE,
  lua: CODE, pl: CODE, ex: CODE, exs: CODE, erl: CODE, hs: CODE, scala: CODE,
  clj: CODE, dart: CODE, r: CODE, zig: CODE, vue: CODE, svelte: CODE, astro: CODE,
  // Shell and automation
  sh: SHELL, bash: SHELL, zsh: SHELL, fish: SHELL, ps1: SHELL, bat: SHELL, cmd: SHELL,
  // Markup and style
  html: MARKUP, htm: MARKUP, xml: MARKUP, svg: SVG,
  css: STYLE, scss: STYLE, sass: STYLE, less: STYLE,
  // Data
  json: JSON_, jsonc: JSON_, json5: JSON_, yaml: DATA, yml: DATA, toml: DATA,
  proto: DATA, graphql: DATA, gql: DATA, ndjson: JSON_,
  csv: SHEET, tsv: SHEET, xlsx: SHEET, xls: SHEET, ods: SHEET,
  sql: SQL, db: SQL, sqlite: SQL, sqlite3: SQL, dump: SQL,
  // Documents
  md: MARKDOWN, mdx: MARKDOWN, txt: DOC, rst: DOC, adoc: DOC, pdf: DOC, doc: DOC, docx: DOC,
  log: LOG,
  // Media
  png: IMAGE, jpg: JPEG, jpeg: JPEG, gif: GIF, webp: IMAGE, avif: IMAGE,
  bmp: IMAGE, ico: IMAGE, tiff: IMAGE, heic: IMAGE, psd: IMAGE,
  mp4: VIDEO, webm: VIDEO, mkv: VIDEO, mov: VIDEO, avi: VIDEO, ogv: VIDEO,
  mp3: AUDIO, wav: AUDIO, flac: AUDIO, ogg: AUDIO, m4a: AUDIO, aac: AUDIO,
  woff: FONT, woff2: FONT, ttf: FONT, otf: FONT, eot: FONT,
  // Archives and packages
  zip: ARCHIVE, tar: ARCHIVE, gz: ARCHIVE, tgz: ARCHIVE, bz2: ARCHIVE,
  xz: ARCHIVE, zst: ARCHIVE, "7z": ARCHIVE, rar: ARCHIVE, jar: ARCHIVE,
  deb: PACKAGE, rpm: PACKAGE, apk: PACKAGE, whl: PACKAGE, iso: PACKAGE,
  // Configuration
  conf: CONFIG, cfg: CONFIG, ini: CONFIG, env: CONFIG, properties: CONFIG,
  service: CONFIG, socket: CONFIG, timer: CONFIG, mount: CONFIG, rules: CONFIG,
  tf: CONFIG, tfvars: CONFIG, hcl: CONFIG, nginx: CONFIG, list: CONFIG,
  // Secrets
  pem: SECRET, key: SECRET, crt: SECRET, cer: SECRET, csr: SECRET, p12: SECRET,
  pfx: SECRET, pub: SECRET, gpg: SECRET, asc: SECRET, kdbx: SECRET,
  // Binary
  so: BINARY, o: BINARY, a: BINARY, dll: BINARY, exe: BINARY, bin: BINARY,
  dat: BINARY, pyc: BINARY, wasm: BINARY, img: BINARY, swp: BINARY,
}

/** Files a server keeps that have no extension to key off at all. */
const BY_NAME: Record<string, FileKind> = {
  dockerfile: DOCKER,
  containerfile: DOCKER,
  "docker-compose.yml": DOCKER,
  "docker-compose.yaml": DOCKER,
  "compose.yml": DOCKER,
  "compose.yaml": DOCKER,
  ".dockerignore": DOCKER,
  makefile: { icon: SettingsGear, tone: "cyan", label: "Makefile" },
  gnumakefile: { icon: SettingsGear, tone: "cyan", label: "Makefile" },
  caddyfile: CONFIG,
  vagrantfile: CONFIG,
  procfile: CONFIG,
  gemfile: CODE,
  rakefile: CODE,
  "package.json": { icon: Puzzle, tone: "red", label: "npm manifest" },
  "package-lock.json": { icon: LockClosed, tone: "slate", label: "Lockfile" },
  "bun.lock": { icon: LockClosed, tone: "slate", label: "Lockfile" },
  "bun.lockb": { icon: LockClosed, tone: "slate", label: "Lockfile" },
  "yarn.lock": { icon: LockClosed, tone: "slate", label: "Lockfile" },
  "pnpm-lock.yaml": { icon: LockClosed, tone: "slate", label: "Lockfile" },
  "go.sum": { icon: LockClosed, tone: "slate", label: "Lockfile" },
  "cargo.lock": { icon: LockClosed, tone: "slate", label: "Lockfile" },
  "go.mod": { icon: Puzzle, tone: "cyan", label: "Go module" },
  "cargo.toml": { icon: Puzzle, tone: "red", label: "Cargo manifest" },
  "requirements.txt": { icon: Puzzle, tone: "blue", label: "Python requirements" },
  license: { icon: FileText, tone: "slate", label: "Licence" },
  "license.md": { icon: FileText, tone: "slate", label: "Licence" },
  readme: { icon: FileText, tone: "cyan", label: "Readme" },
  "readme.md": { icon: AcronymMarkdown, tone: "cyan", label: "Readme" },
  changelog: DOC,
  "changelog.md": MARKDOWN,
  ".gitignore": { icon: GitBranch, tone: "slate", label: "git exclusions" },
  ".gitconfig": CONFIG,
  ".env": { icon: Key, tone: "amber", label: "Environment file" },
  ".bashrc": SHELL,
  ".zshrc": SHELL,
  ".profile": SHELL,
  ".bash_profile": SHELL,
  ".editorconfig": CONFIG,
  authorized_keys: SECRET,
  known_hosts: SECRET,
  passwd: { icon: Key, tone: "red", label: "Account database" },
  shadow: { icon: Key, tone: "red", label: "Password hashes" },
  fstab: CONFIG,
  hosts: CONFIG,
  crontab: CONFIG,
}

/**
 * Folders whose name says more than "folder" does.
 *
 * The build and dependency directories take `FolderMinus` rather than a
 * folder-with-a-gear: what they have in common is not that they are
 * configured, it is that nothing in them is yours to edit, and a folder with
 * a minus in it says "walk past this one" at a glance.
 */
const FOLDERS_BY_NAME: Record<string, FileKind> = {
  ".git": { icon: GitBranch, tone: "amber", label: "git repository" },
  node_modules: { icon: FolderMinus, tone: "slate", label: "Installed packages" },
  vendor: { icon: FolderMinus, tone: "slate", label: "Vendored dependencies" },
  ".next": { icon: FolderMinus, tone: "slate", label: "Build output" },
  dist: { icon: FolderMinus, tone: "slate", label: "Build output" },
  build: { icon: FolderMinus, tone: "slate", label: "Build output" },
  target: { icon: FolderMinus, tone: "slate", label: "Build output" },
  etc: { icon: SettingsGear, tone: "cyan", label: "Configuration" },
  home: { icon: Home, tone: "primary", label: "Home directories" },
  root: { icon: Home, tone: "primary", label: "root's home" },
  var: { icon: Servers, tone: "amber", label: "Variable data" },
  log: { icon: Logs, tone: "amber", label: "Logs" },
  logs: { icon: Logs, tone: "amber", label: "Logs" },
  www: { icon: Globe, tone: "violet", label: "Web root" },
  public: { icon: Globe, tone: "violet", label: "Public assets" },
  ssl: { icon: Key, tone: "green", label: "Certificates" },
  ssh: { icon: Key, tone: "green", label: "SSH configuration" },
  ".ssh": { icon: Key, tone: "green", label: "SSH keys" },
  backups: { icon: Archive, tone: "amber", label: "Backups" },
}

const TONE_VAR: Record<FileTone, string> = {
  slate: "var(--tag-slate)",
  red: "var(--tag-red)",
  amber: "var(--tag-amber)",
  green: "var(--tag-green)",
  cyan: "var(--tag-cyan)",
  blue: "var(--tag-blue)",
  violet: "var(--tag-violet)",
  pink: "var(--tag-pink)",
  primary: "var(--primary)",
}

/**
 * The kind of a name.
 *
 * Order matters: the whole name first (a `docker-compose.yml` is not merely
 * YAML), then the extension, then the extension *under* a backup suffix — an
 * `nginx.conf.bak` is still a config file, and the pass that forgets this is
 * how a directory of saved configs turns into a wall of blank sheets.
 */
export function fileKind(name: string, isDir = false): FileKind {
  const lower = name.toLowerCase()
  if (isDir) {
    return FOLDERS_BY_NAME[lower] ?? { icon: FolderClosed, tone: "primary", label: "Folder" }
  }
  if (BY_NAME[lower]) return BY_NAME[lower]

  const parts = lower.split(".")
  if (parts.length > 1) {
    const ext = parts[parts.length - 1]
    if (BY_EXTENSION[ext]) return BY_EXTENSION[ext]
    if (parts.length > 2 && BACKUP_SUFFIXES.has(ext)) {
      const under = BY_EXTENSION[parts[parts.length - 2]]
      if (under) return under
    }
    // `.tar.gz` and friends: the archive is the pair, not the last word.
    if (parts.length > 2 && parts[parts.length - 2] === "tar") return ARCHIVE
  }
  return PLAIN
}

const BACKUP_SUFFIXES = new Set(["bak", "old", "orig", "save", "disabled", "dpkg-old", "rpmsave"])

export function kindOfEntry(entry: Pick<FileEntry, "name" | "isDir">): FileKind {
  return fileKind(entry.name, entry.isDir)
}

/** The CSS colour for a kind, for anything that draws its own glyph. */
export function toneColour(tone: FileTone): string {
  return TONE_VAR[tone]
}

/**
 * The icon for one entry.
 *
 * A symlink keeps its target's icon and gains the link glyph as a corner
 * badge rather than replacing it: what a link points at is the useful fact,
 * and the old listing threw it away to draw a chain on every one of them.
 *
 * **The colour is a fallback, not a fixed value.** A row that selects itself
 * with a solid `bg-primary` — the places rail does — is white in dark mode and
 * black in light, and a tone painted straight onto the glyph is then a folder
 * the same colour as the thing behind it: `primary` on `bg-primary` is
 * invisible in *both* themes, which is the one way to get it wrong twice. The
 * tone is therefore the fallback of `--file-icon-colour`, so any ancestor can
 * say "in here, icons are my foreground" by setting that one property. It has
 * to be a custom property rather than a class because this is an inline style,
 * and nothing but inline beats inline.
 *
 * A directory used to be tinted with `fill-current/15` to set it apart from a
 * file. That worked on a stroked set, whose paths carry no fill of their own
 * and so inherit one; every path in this set paints itself `currentColor`, so
 * the class is now a no-op that only looks like it does something. It is gone,
 * and nothing replaces it — `FolderClosed` and `FolderOpen` are already a
 * different silhouette from every file glyph, which is the distinction the
 * tint was standing in for.
 */
export function FileIcon({
  entry,
  open,
  className,
  badgeClassName,
}: {
  entry: Pick<FileEntry, "name" | "isDir" | "isSymlink" | "linkBroken">
  /** Draw an opened folder — the row the listing is currently inside. */
  open?: boolean
  className?: string
  badgeClassName?: string
}) {
  const kind = kindOfEntry(entry)
  const Glyph = entry.isDir && open ? FolderOpen : kind.icon
  return (
    <span className={cn("relative inline-flex shrink-0", className)}>
      <Glyph
        className="size-full"
        style={{
          color: `var(--file-icon-colour, ${
            entry.linkBroken ? "var(--tag-red)" : TONE_VAR[kind.tone]
          })`,
        }}
        aria-hidden
      />
      {entry.isSymlink && (
        <Link
          className={cn(
            "absolute -right-0.5 -bottom-0.5 size-1/2 rounded-[2px] bg-card text-muted-foreground",
            badgeClassName,
          )}
          aria-hidden
        />
      )}
    </span>
  )
}
