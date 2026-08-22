"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import {
  ChevronRight,
  Download,
  File as FileIcon,
  FilePlus,
  Folder,
  FolderOpen,
  FolderPlus,
  Link as LinkIcon,
  RefreshCw,
  Trash2,
} from "lucide-react"
import { toast } from "sonner"
import { del, downloadUrl, get, post } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { FileEntry, FileListing, GitFileChange } from "@/lib/types"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import { Spinner } from "@/components/state"

/** What the inline confirm surface needs; the tools panel renders it. */
export type ConfirmRequest = {
  title: string
  body?: React.ReactNode
  confirmLabel?: string
  danger?: boolean
  run: () => Promise<void> | void
}

/**
 * A lazy, git-aware file tree rooted at one directory.
 *
 * Everything here is deliberately inline — no portalled dialog, popover or
 * tooltip — because this panel is expected to be open while the terminal is in
 * the browser's real fullscreen, where a portal to `document.body` renders
 * outside the fullscreen element and is simply invisible. Create rows are
 * inputs woven into the tree, delete goes through the panel's own inline
 * confirm, and every label is a plain `title` attribute.
 *
 * Each folder fetches its children the first time it is opened and caches them;
 * `reload` drops one folder's cache so a change (a new file, a delete, a git
 * operation that rewrote the tree) is picked up without collapsing the rest.
 */
export function FileTree({
  root,
  statusMap,
  canWrite,
  canDelete,
  activeFile,
  activeDir,
  onOpenFile,
  onNavigate,
  onConfirm,
  onChanged,
  onOpenInFiles,
}: {
  root: string
  /** Absolute path → its git status, so a changed file can carry a badge. */
  statusMap: Record<string, GitFileChange>
  canWrite: boolean
  canDelete: boolean
  activeFile?: string
  /** Highlight this folder as the one currently open elsewhere on the page. */
  activeDir?: string
  onOpenFile: (path: string) => void
  /** When set, clicking a folder also reports it — the Files page navigates its
   *  main listing there while the tree keeps expanding as usual. */
  onNavigate?: (path: string) => void
  onConfirm: (req: ConfirmRequest) => void
  onChanged: () => void
  onOpenInFiles?: (path: string) => void
}) {
  const [showHidden, setShowHidden] = useState(false)
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set([root]))
  const [children, setChildren] = useState<Record<string, FileEntry[]>>({})
  const [loading, setLoading] = useState<Set<string>>(new Set())
  const [failed, setFailed] = useState<Record<string, string>>({})
  // The folder an inline create-row is attached to, and what it makes.
  const [creating, setCreating] = useState<{ parent: string; kind: "file" | "folder" } | null>(
    null,
  )

  const load = useCallback(
    async (path: string) => {
      setLoading((s) => new Set(s).add(path))
      try {
        const listing = await get<FileListing>("/files/list", { path, hidden: showHidden })
        setChildren((c) => ({ ...c, [path]: sortEntries(listing.entries) }))
        setFailed((f) => {
          if (!f[path]) return f
          const next = { ...f }
          delete next[path]
          return next
        })
      } catch (err) {
        setFailed((f) => ({ ...f, [path]: String(err) }))
      } finally {
        setLoading((s) => {
          const next = new Set(s)
          next.delete(path)
          return next
        })
      }
    },
    [showHidden],
  )

  // Load the root, and re-load every already-open folder, whenever the hidden
  // toggle changes — otherwise a "show hidden" flip would take effect at the
  // top level while the open subfolders kept their old contents. The tree is
  // keyed on `root` by its caller, so a new root arrives as a fresh mount with
  // only the root open; that mount lands here too and loads it.
  useEffect(() => {
    void load(root)
    for (const path of expanded) if (path !== root) void load(path)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [root, showHidden])

  const reload = useCallback(
    (path: string) => {
      if (expanded.has(path) || path === root) void load(path)
    },
    [expanded, root, load],
  )

  const toggle = (path: string) => {
    setExpanded((s) => {
      const next = new Set(s)
      if (next.has(path)) {
        next.delete(path)
      } else {
        next.add(path)
        if (!children[path]) void load(path)
      }
      return next
    })
  }

  const submitCreate = async (name: string) => {
    if (!creating) return
    const trimmed = name.trim()
    if (!trimmed) {
      setCreating(null)
      return
    }
    const target = `${creating.parent.replace(/\/$/, "")}/${trimmed}`
    try {
      if (creating.kind === "folder") await post("/files/mkdir", { path: target })
      else await post("/files/touch", { path: target })
      toast.success(`Created ${trimmed}`)
      const parent = creating.parent
      setCreating(null)
      setExpanded((s) => new Set(s).add(parent))
      await load(parent)
      onChanged()
      if (creating.kind === "file") onOpenFile(target)
    } catch (err) {
      toast.error("Could not create that", { description: String(err) })
    }
  }

  const requestDelete = (entry: FileEntry, parent: string) =>
    onConfirm({
      title: `Delete ${entry.name}`,
      danger: true,
      confirmLabel: "Delete",
      body: (
        <>
          <span className="font-mono break-all">{entry.path}</span> is deleted permanently
          {entry.isDir ? ", along with everything inside it." : "."}
        </>
      ),
      run: async () => {
        await del("/files/delete", {
          confirm: entry.name,
          query: { path: entry.path, recursive: entry.isDir },
        })
        toast.success(`Deleted ${entry.name}`)
        await load(parent)
        onChanged()
      },
    })

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex shrink-0 items-center gap-1 border-b border-hairline bg-surface-header/60 px-2 py-1.5">
        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-muted-foreground" title={root}>
          {root}
        </span>
        {canWrite && (
          <>
            <TreeButton
              label="New file in this folder"
              onClick={() => {
                setExpanded((s) => new Set(s).add(root))
                setCreating({ parent: root, kind: "file" })
              }}
            >
              <FilePlus className="size-3.5" />
            </TreeButton>
            <TreeButton
              label="New folder here"
              onClick={() => {
                setExpanded((s) => new Set(s).add(root))
                setCreating({ parent: root, kind: "folder" })
              }}
            >
              <FolderPlus className="size-3.5" />
            </TreeButton>
          </>
        )}
        <label className="flex cursor-pointer items-center gap-1 px-1 text-[10px] text-muted-foreground">
          <Checkbox
            checked={showHidden}
            onCheckedChange={(v) => setShowHidden(v === true)}
            className="size-3"
          />
          Hidden
        </label>
        {onOpenInFiles && (
          <TreeButton label="Reveal in the full file manager" onClick={() => onOpenInFiles(root)}>
            <FolderOpen className="size-3.5" />
          </TreeButton>
        )}
        <TreeButton label="Refresh" onClick={() => reload(root)}>
          <RefreshCw className="size-3.5" />
        </TreeButton>
      </div>

      <div className="min-h-0 flex-1 overflow-auto py-1">
        <TreeLevel
          parent={root}
          depth={0}
          entries={children[root]}
          loadingThis={loading.has(root)}
          error={failed[root]}
          expanded={expanded}
          entriesByPath={children}
          loading={loading}
          failed={failed}
          statusMap={statusMap}
          activeFile={activeFile}
          activeDir={activeDir}
          canWrite={canWrite}
          canDelete={canDelete}
          creating={creating}
          onToggle={toggle}
          onOpenFile={onOpenFile}
          onNavigate={onNavigate}
          onStartCreate={(parent, kind) => {
            setExpanded((s) => new Set(s).add(parent))
            setCreating({ parent, kind })
          }}
          onCancelCreate={() => setCreating(null)}
          onSubmitCreate={submitCreate}
          onDelete={requestDelete}
        />
      </div>
    </div>
  )
}

type LevelProps = {
  parent: string
  depth: number
  entries: FileEntry[] | undefined
  loadingThis: boolean
  error?: string
  expanded: Set<string>
  entriesByPath: Record<string, FileEntry[]>
  loading: Set<string>
  failed: Record<string, string>
  statusMap: Record<string, GitFileChange>
  activeFile?: string
  activeDir?: string
  canWrite: boolean
  canDelete: boolean
  creating: { parent: string; kind: "file" | "folder" } | null
  onToggle: (path: string) => void
  onOpenFile: (path: string) => void
  onNavigate?: (path: string) => void
  onStartCreate: (parent: string, kind: "file" | "folder") => void
  onCancelCreate: () => void
  onSubmitCreate: (name: string) => void
  onDelete: (entry: FileEntry, parent: string) => void
}

/** One directory's contents, plus the create-row that belongs to it. */
function TreeLevel(props: LevelProps) {
  const { parent, depth, entries, loadingThis, error, creating } = props
  const indent = 8 + depth * 14

  return (
    <>
      {creating?.parent === parent && (
        <CreateRow
          indent={indent + 16}
          kind={creating.kind}
          onCancel={props.onCancelCreate}
          onSubmit={props.onSubmitCreate}
        />
      )}
      {loadingThis && !entries && (
        <div className="flex items-center gap-2 px-2 py-1 text-[11px] text-muted-foreground" style={{ paddingLeft: indent + 16 }}>
          <Spinner className="size-3" />
          Loading…
        </div>
      )}
      {error && (
        <div
          className="truncate px-2 py-1 text-[11px] text-destructive"
          style={{ paddingLeft: indent + 16 }}
          title={error}
        >
          {error}
        </div>
      )}
      {entries?.length === 0 && creating?.parent !== parent && (
        <div
          className="px-2 py-1 text-[11px] text-muted-foreground italic"
          style={{ paddingLeft: indent + 16 }}
        >
          empty
        </div>
      )}
      {entries?.map((entry) => (
        <TreeNode key={entry.path} entry={entry} {...props} />
      ))}
    </>
  )
}

function TreeNode({ entry, depth, ...props }: LevelProps & { entry: FileEntry }) {
  const { expanded, entriesByPath, statusMap, activeFile, activeDir } = props
  const indent = 8 + depth * 14
  const isOpen = expanded.has(entry.path)
  const status = statusMap[entry.path]
  const dirHasChanges =
    entry.isDir &&
    Object.keys(statusMap).some((p) => p.startsWith(entry.path + "/"))

  const onFolderClick = () => {
    props.onToggle(entry.path)
    props.onNavigate?.(entry.path)
  }

  return (
    <>
      <div
        className={cn(
          "group flex items-center gap-1 py-[3px] pr-1 text-[13px] hover:bg-[var(--row-hover)]",
          activeFile === entry.path && "bg-primary/10",
          entry.isDir && activeDir === entry.path && "bg-primary/10",
        )}
        style={{ paddingLeft: indent }}
      >
        <button
          type="button"
          className="flex min-w-0 flex-1 items-center gap-1 text-left"
          onClick={() => (entry.isDir ? onFolderClick() : props.onOpenFile(entry.path))}
          onDoubleClick={() => entry.isDir && props.onOpenFile(entry.path)}
          title={entry.path}
        >
          <span className="flex size-4 shrink-0 items-center justify-center text-muted-foreground">
            {entry.isDir ? (
              <ChevronRight className={cn("size-3.5 transition-transform", isOpen && "rotate-90")} />
            ) : null}
          </span>
          {entry.isDir ? (
            isOpen ? (
              <FolderOpen className="size-4 shrink-0 text-primary" />
            ) : (
              <Folder className="size-4 shrink-0 fill-primary/20 text-primary" />
            )
          ) : entry.isSymlink ? (
            <LinkIcon className="size-4 shrink-0 text-muted-foreground" />
          ) : (
            <FileIcon className="size-4 shrink-0 text-muted-foreground" />
          )}
          <span
            className={cn(
              "truncate",
              status?.label === "deleted" && "text-destructive line-through",
              status && status.label !== "deleted" && "text-warning",
            )}
          >
            {entry.name}
          </span>
          {status ? (
            <StatusMark change={status} />
          ) : dirHasChanges ? (
            <span className="size-1.5 shrink-0 rounded-full bg-warning" title="contains changes" />
          ) : null}
        </button>

        <div className="flex shrink-0 items-center opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100">
          {entry.isDir && props.canWrite && (
            <>
              <TreeButton
                label="New file here"
                onClick={() => props.onStartCreate(entry.path, "file")}
              >
                <FilePlus className="size-3" />
              </TreeButton>
              <TreeButton
                label="New folder here"
                onClick={() => props.onStartCreate(entry.path, "folder")}
              >
                <FolderPlus className="size-3" />
              </TreeButton>
            </>
          )}
          {!entry.isDir && (
            <TreeButton label="Download" asChild>
              <a href={downloadUrl("/files/download", { path: entry.path })} download>
                <Download className="size-3" />
              </a>
            </TreeButton>
          )}
          {props.canDelete && (
            <TreeButton
              label="Delete"
              className="text-destructive"
              onClick={() => props.onDelete(entry, props.parent)}
            >
              <Trash2 className="size-3" />
            </TreeButton>
          )}
        </div>
      </div>

      {entry.isDir && isOpen && (
        <TreeLevel
          {...props}
          parent={entry.path}
          depth={depth + 1}
          entries={entriesByPath[entry.path]}
          loadingThis={props.loading.has(entry.path)}
          error={props.failed[entry.path]}
        />
      )}
    </>
  )
}

function CreateRow({
  indent,
  kind,
  onCancel,
  onSubmit,
}: {
  indent: number
  kind: "file" | "folder"
  onCancel: () => void
  onSubmit: (name: string) => void
}) {
  const [value, setValue] = useState("")
  const ref = useRef<HTMLInputElement>(null)
  useEffect(() => ref.current?.focus(), [])
  return (
    <div className="flex items-center gap-1 py-[3px] pr-1" style={{ paddingLeft: indent }}>
      {kind === "folder" ? (
        <FolderPlus className="size-4 shrink-0 text-primary" />
      ) : (
        <FilePlus className="size-4 shrink-0 text-muted-foreground" />
      )}
      <Input
        ref={ref}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") onSubmit(value)
          if (e.key === "Escape") onCancel()
        }}
        onBlur={() => (value.trim() ? onSubmit(value) : onCancel())}
        placeholder={kind === "folder" ? "folder name" : "file name"}
        className="h-6 flex-1 font-mono text-[12px]"
      />
    </div>
  )
}

/** The one-letter git badge on a changed file. Colour matches the git page. */
function StatusMark({ change }: { change: GitFileChange }) {
  const letter = change.staged
    ? (change.index || "?").toUpperCase()
    : (change.worktree || change.index || "?").toUpperCase()
  return (
    <span
      className={cn(
        "ml-auto shrink-0 rounded px-1 font-mono text-[10px] leading-none",
        change.staged ? "bg-success/15 text-success" : "bg-warning/15 text-warning",
      )}
      title={`${change.label}${change.staged ? " · staged" : ""}`}
    >
      {change.label === "untracked" ? "U" : letter}
    </span>
  )
}

function TreeButton({
  label,
  onClick,
  asChild,
  className,
  children,
}: {
  label: string
  onClick?: () => void
  asChild?: boolean
  className?: string
  children: React.ReactNode
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant="ghost"
      asChild={asChild}
      title={label}
      aria-label={label}
      className={cn("size-6 shrink-0 p-0 text-muted-foreground hover:text-foreground", className)}
      onClick={onClick}
    >
      {children}
    </Button>
  )
}

function sortEntries(entries: FileEntry[]): FileEntry[] {
  return [...entries].sort((a, b) => {
    if (a.isDir !== b.isDir) return a.isDir ? -1 : 1
    return a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: "base" })
  })
}
