"use client"

import {
  Binary,
  Braces,
  Container,
  Database,
  FileArchive,
  FileAudio,
  FileCode,
  FileCog,
  FileImage,
  FileKey,
  FileSpreadsheet,
  FileText,
  FileTerminal,
  FileType,
  FileVideo,
  Folder,
  FolderCog,
  FolderGit2,
  FolderOpen,
  Globe,
  Home,
  Link as LinkIcon,
  Lock,
  Package,
  Palette,
  Server,
  type LucideIcon,
} from "lucide-react"
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
  icon: LucideIcon
  tone: FileTone
  /** What this is, in the words somebody would use out loud. */
  label: string
}

const CODE: FileKind = { icon: FileCode, tone: "blue", label: "Source code" }
const MARKUP: FileKind = { icon: Globe, tone: "violet", label: "Markup" }
const STYLE: FileKind = { icon: Palette, tone: "violet", label: "Stylesheet" }
const DATA: FileKind = { icon: Braces, tone: "amber", label: "Structured data" }
const SHEET: FileKind = { icon: FileSpreadsheet, tone: "green", label: "Tabular data" }
const SQL: FileKind = { icon: Database, tone: "amber", label: "SQL" }
const DOC: FileKind = { icon: FileText, tone: "slate", label: "Document" }
const IMAGE: FileKind = { icon: FileImage, tone: "pink", label: "Image" }
const VIDEO: FileKind = { icon: FileVideo, tone: "violet", label: "Video" }
const AUDIO: FileKind = { icon: FileAudio, tone: "cyan", label: "Audio" }
const ARCHIVE: FileKind = { icon: FileArchive, tone: "amber", label: "Archive" }
const SHELL: FileKind = { icon: FileTerminal, tone: "green", label: "Shell script" }
const CONFIG: FileKind = { icon: FileCog, tone: "cyan", label: "Configuration" }
const SECRET: FileKind = { icon: FileKey, tone: "green", label: "Key or certificate" }
const BINARY: FileKind = { icon: Binary, tone: "red", label: "Binary" }
const FONT: FileKind = { icon: FileType, tone: "violet", label: "Font" }
const LOG: FileKind = { icon: FileText, tone: "amber", label: "Log" }
const PACKAGE: FileKind = { icon: Package, tone: "red", label: "Package" }
const DOCKER: FileKind = { icon: Container, tone: "blue", label: "Container build" }
const PLAIN: FileKind = { icon: FileText, tone: "slate", label: "File" }

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
  html: MARKUP, htm: MARKUP, xml: MARKUP, svg: IMAGE,
  css: STYLE, scss: STYLE, sass: STYLE, less: STYLE,
  // Data
  json: DATA, jsonc: DATA, json5: DATA, yaml: DATA, yml: DATA, toml: DATA,
  proto: DATA, graphql: DATA, gql: DATA, ndjson: DATA,
  csv: SHEET, tsv: SHEET, xlsx: SHEET, xls: SHEET, ods: SHEET,
  sql: SQL, db: SQL, sqlite: SQL, sqlite3: SQL, dump: SQL,
  // Documents
  md: DOC, mdx: DOC, txt: DOC, rst: DOC, adoc: DOC, pdf: DOC, doc: DOC, docx: DOC,
  log: LOG,
  // Media
  png: IMAGE, jpg: IMAGE, jpeg: IMAGE, gif: IMAGE, webp: IMAGE, avif: IMAGE,
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
  makefile: { icon: FileCog, tone: "cyan", label: "Makefile" },
  gnumakefile: { icon: FileCog, tone: "cyan", label: "Makefile" },
  caddyfile: CONFIG,
  vagrantfile: CONFIG,
  procfile: CONFIG,
  gemfile: CODE,
  rakefile: CODE,
  "package.json": { icon: Package, tone: "red", label: "npm manifest" },
  "package-lock.json": { icon: Lock, tone: "slate", label: "Lockfile" },
  "bun.lock": { icon: Lock, tone: "slate", label: "Lockfile" },
  "bun.lockb": { icon: Lock, tone: "slate", label: "Lockfile" },
  "yarn.lock": { icon: Lock, tone: "slate", label: "Lockfile" },
  "pnpm-lock.yaml": { icon: Lock, tone: "slate", label: "Lockfile" },
  "go.sum": { icon: Lock, tone: "slate", label: "Lockfile" },
  "cargo.lock": { icon: Lock, tone: "slate", label: "Lockfile" },
  "go.mod": { icon: Package, tone: "cyan", label: "Go module" },
  "cargo.toml": { icon: Package, tone: "red", label: "Cargo manifest" },
  "requirements.txt": { icon: Package, tone: "blue", label: "Python requirements" },
  license: { icon: FileText, tone: "slate", label: "Licence" },
  "license.md": { icon: FileText, tone: "slate", label: "Licence" },
  readme: { icon: FileText, tone: "cyan", label: "Readme" },
  "readme.md": { icon: FileText, tone: "cyan", label: "Readme" },
  changelog: DOC,
  "changelog.md": DOC,
  ".gitignore": { icon: FolderGit2, tone: "slate", label: "git exclusions" },
  ".gitconfig": CONFIG,
  ".env": { icon: FileKey, tone: "amber", label: "Environment file" },
  ".bashrc": SHELL,
  ".zshrc": SHELL,
  ".profile": SHELL,
  ".bash_profile": SHELL,
  ".editorconfig": CONFIG,
  authorized_keys: SECRET,
  known_hosts: SECRET,
  passwd: { icon: FileKey, tone: "red", label: "Account database" },
  shadow: { icon: FileKey, tone: "red", label: "Password hashes" },
  fstab: CONFIG,
  hosts: CONFIG,
  crontab: CONFIG,
}

/** Folders whose name says more than "folder" does. */
const FOLDERS_BY_NAME: Record<string, FileKind> = {
  ".git": { icon: FolderGit2, tone: "amber", label: "git repository" },
  node_modules: { icon: FolderCog, tone: "slate", label: "Installed packages" },
  vendor: { icon: FolderCog, tone: "slate", label: "Vendored dependencies" },
  ".next": { icon: FolderCog, tone: "slate", label: "Build output" },
  dist: { icon: FolderCog, tone: "slate", label: "Build output" },
  build: { icon: FolderCog, tone: "slate", label: "Build output" },
  target: { icon: FolderCog, tone: "slate", label: "Build output" },
  etc: { icon: FolderCog, tone: "cyan", label: "Configuration" },
  home: { icon: Home, tone: "primary", label: "Home directories" },
  root: { icon: Home, tone: "primary", label: "root's home" },
  var: { icon: Server, tone: "amber", label: "Variable data" },
  log: { icon: Server, tone: "amber", label: "Logs" },
  logs: { icon: Server, tone: "amber", label: "Logs" },
  www: { icon: Globe, tone: "violet", label: "Web root" },
  public: { icon: Globe, tone: "violet", label: "Public assets" },
  ssl: { icon: FileKey, tone: "green", label: "Certificates" },
  ssh: { icon: FileKey, tone: "green", label: "SSH configuration" },
  ".ssh": { icon: FileKey, tone: "green", label: "SSH keys" },
  backups: { icon: FileArchive, tone: "amber", label: "Backups" },
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
    return FOLDERS_BY_NAME[lower] ?? { icon: Folder, tone: "primary", label: "Folder" }
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
  const Icon = entry.isDir && open ? FolderOpen : kind.icon
  return (
    <span className={cn("relative inline-flex shrink-0", className)}>
      <Icon
        className={cn("size-full", entry.isDir && "fill-current/15")}
        style={{ color: entry.linkBroken ? "var(--tag-red)" : TONE_VAR[kind.tone] }}
        aria-hidden
      />
      {entry.isSymlink && (
        <LinkIcon
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
