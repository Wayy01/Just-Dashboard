"use client"

import { Fragment, useMemo, useRef, useState } from "react"
import { useSearchParams } from "next/navigation"
import {
  ArrowUp,
  ChevronDown,
  ChevronUp,
  ClipboardPaste,
  FileArchive,
  FilePlus,
  FolderInput,
  FolderPlus,
  FolderTree,
  Home,
  Link2,
  ListTree,
  Plus,
  Scissors,
  Search,
  Trash2,
  Upload,
  X,
} from "lucide-react"
import { toast } from "sonner"
import { API_BASE, del, downloadUrl, get, post } from "@/lib/api"
import { truncateMiddle } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { FileEntry, FileListing } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader } from "@/components/page"
import { Panel, PanelBody, PanelToolbar } from "@/components/panel"
import { FileEditorSheet } from "@/components/files/file-editor"
import { FileRow } from "@/components/files/file-row"
import { FileTree } from "@/components/files/file-tree"
import { PermissionsDialog } from "@/components/files/permissions-dialog"
import { usePrompt } from "@/components/files/prompt-dialog"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { Input } from "@/components/ui/input"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb"
import {
  stickyTableHeader,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

type SortKey = "name" | "size" | "modified" | "owner" | "mode"
type Clip = { mode: "cut" | "copy"; paths: string[] }

const clean = (p: string) => p.replace(/\/+$/, "") || "/"
const join = (dir: string, name: string) => `${clean(dir)}/${name}`.replace(/\/{2,}/g, "/")
const parentOf = (p: string) => {
  const c = clean(p)
  const i = c.lastIndexOf("/")
  return i <= 0 ? "/" : c.slice(0, i)
}
const baseOf = (p: string) => clean(p).split("/").pop() ?? p

/** A non-colliding "<name> copy" for duplicating into a directory. */
function uniqueName(name: string, taken: Set<string>): string {
  const dot = name.lastIndexOf(".")
  const stem = dot > 0 ? name.slice(0, dot) : name
  const ext = dot > 0 ? name.slice(dot) : ""
  let candidate = `${stem} copy${ext}`
  let n = 2
  while (taken.has(candidate)) candidate = `${stem} copy ${n++}${ext}`
  return candidate
}

export default function FilesPage() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const { prompt, dialog: promptDialog } = usePrompt()
  const canWrite = can("file.write")
  const canDestruct = can("destructive")
  const canAdmin = can("system.admin")

  const initialPath = useSearchParams().get("path")
  const [path, setPath] = useState(initialPath || "/")
  const [showHidden, setShowHidden] = useState(false)
  const [showTree, setShowTree] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)
  const [permsEntry, setPermsEntry] = useState<FileEntry | null>(null)
  const [symlinkOpen, setSymlinkOpen] = useState(false)
  const [clip, setClip] = useState<Clip | null>(null)
  const [sort, setSort] = useState<{ key: SortKey; dir: "asc" | "desc" }>({
    key: "name",
    dir: "asc",
  })
  const [dragOver, setDragOver] = useState(false)
  // The selection is scoped to the directory it was made in, so navigating away
  // discards it without a reset effect.
  const [selection, setSelection] = useState<{ dir: string; paths: Set<string> }>({
    dir: path,
    paths: new Set(),
  })
  const selected = selection.dir === path ? [...selection.paths] : []
  const uploadRef = useRef<HTMLInputElement>(null)

  const listing = usePoll(
    (signal) => get<FileListing>("/files/list", { path, hidden: showHidden }, signal),
    0,
    [path, showHidden],
  )
  const refresh = listing.refresh

  const crumbs = useMemo(() => {
    const parts = path.split("/").filter(Boolean)
    const out = [{ label: "/", href: "/" }]
    let acc = ""
    for (const part of parts) {
      acc += `/${part}`
      out.push({ label: part, href: acc })
    }
    return out
  }, [path])

  // Directories first, then the chosen column. Keeping folders grouped is what
  // every file manager does and what makes a long listing navigable.
  const entries = useMemo(() => {
    const list = [...(listing.data?.entries ?? [])]
    const factor = sort.dir === "asc" ? 1 : -1
    list.sort((a, b) => {
      if (a.isDir !== b.isDir) return a.isDir ? -1 : 1
      let cmp = 0
      switch (sort.key) {
        case "size":
          cmp = a.size - b.size
          break
        case "modified":
          cmp = new Date(a.modified).getTime() - new Date(b.modified).getTime()
          break
        case "owner":
          cmp = `${a.owner}:${a.group}`.localeCompare(`${b.owner}:${b.group}`)
          break
        case "mode":
          cmp = a.modeOctal.localeCompare(b.modeOctal)
          break
        default:
          cmp = a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: "base" })
      }
      return cmp * factor
    })
    return list
  }, [listing.data, sort])

  const clearSelection = () => setSelection({ dir: path, paths: new Set() })

  const upload = async (files: FileList | File[] | null) => {
    const arr = files ? Array.from(files) : []
    if (arr.length === 0) return
    const form = new FormData()
    for (const file of arr) form.append("file", file)
    try {
      const res = await fetch(
        `${API_BASE}/files/upload?path=${encodeURIComponent(path)}&overwrite=true`,
        { method: "POST", credentials: "include", body: form },
      )
      if (!res.ok) throw new Error((await res.json()).error?.message ?? res.statusText)
      toast.success(`Uploaded ${arr.length} file${arr.length === 1 ? "" : "s"}`)
      refresh()
    } catch (err) {
      toast.error("Upload failed", { description: String(err) })
    } finally {
      if (uploadRef.current) uploadRef.current.value = ""
    }
  }

  // --- operations ---

  const rename = (entry: FileEntry) =>
    prompt({
      title: `Rename ${entry.name}`,
      label: "New name",
      initial: entry.name,
      confirmLabel: "Rename",
      selectBasename: true,
      validate: (v) =>
        v.includes("/") ? "A name cannot contain a slash." : undefined,
      hint: (v) => join(path, v || "…"),
      submit: async (name) => {
        await post("/files/move", { from: entry.path, to: join(path, name) })
        toast.success(`Renamed to ${name}`)
        refresh()
      },
    })

  const duplicate = async (entry: FileEntry) => {
    const taken = new Set((listing.data?.entries ?? []).map((e) => e.name))
    const name = uniqueName(entry.name, taken)
    try {
      await post("/files/copy", { from: entry.path, to: join(path, name) })
      toast.success(`Duplicated as ${name}`)
      refresh()
    } catch (err) {
      toast.error("Could not duplicate", { description: String(err) })
    }
  }

  const extract = async (entry: FileEntry) => {
    try {
      const res = await post<{ extracted: number }>("/files/extract", {
        archive: entry.path,
        destination: path,
      })
      toast.success(`Extracted ${res.extracted} item${res.extracted === 1 ? "" : "s"}`)
      refresh()
    } catch (err) {
      toast.error("Could not extract", { description: String(err) })
    }
  }

  const deleteEntry = (entry: FileEntry) =>
    confirm({
      title: entry.isDir ? "Delete folder" : "Delete file",
      phrase: entry.name,
      confirmLabel: "Delete",
      description: (
        <p className="text-destructive">
          <b>{entry.path}</b> is deleted permanently
          {entry.isDir ? ", along with everything inside it." : "."}
        </p>
      ),
      action: async (c) => {
        await del("/files/delete", {
          confirm: c,
          query: { path: entry.path, recursive: entry.isDir },
        })
        refresh()
      },
    })

  const bulkDelete = () =>
    confirm({
      title: `Delete ${selected.length} item${selected.length === 1 ? "" : "s"}`,
      confirmLabel: "Delete all",
      description: (
        <p className="text-destructive">
          {selected.length} selected item{selected.length === 1 ? " is" : "s are"} deleted
          permanently, folders and everything inside them included.
        </p>
      ),
      action: async () => {
        let failed = 0
        for (const p of selected) {
          try {
            await del("/files/delete", { confirm: baseOf(p), query: { path: p, recursive: true } })
          } catch {
            failed++
          }
        }
        clearSelection()
        refresh()
        if (failed) toast.error(`${failed} item(s) could not be deleted`)
      },
    })

  const paste = async () => {
    if (!clip) return
    const taken = new Set((listing.data?.entries ?? []).map((e) => e.name))
    let ok = 0
    let failed = 0
    for (const src of clip.paths) {
      const base = baseOf(src)
      const srcParent = parentOf(src)
      try {
        if (clip.mode === "cut") {
          if (srcParent === path) continue // already here
          await post("/files/move", { from: src, to: path })
        } else {
          // Copy keeps the name, unless that would collide (same folder, or an
          // existing entry) — then it becomes a "copy".
          const name = taken.has(base) ? uniqueName(base, taken) : base
          await post("/files/copy", { from: src, to: join(path, name) })
          taken.add(name)
        }
        ok++
      } catch {
        failed++
      }
    }
    if (clip.mode === "cut") setClip(null)
    clearSelection()
    refresh()
    if (failed) toast.error(`${failed} item(s) could not be pasted`)
    else if (ok) toast.success(`${clip.mode === "cut" ? "Moved" : "Copied"} ${ok} item(s) here`)
  }

  const newFolder = () =>
    prompt({
      title: "New folder",
      placeholder: "Folder name",
      confirmLabel: "Create",
      hint: (v) => join(path, v || "…"),
      submit: async (name) => {
        await post("/files/mkdir", { path: join(path, name) })
        toast.success(`Created ${name}`)
        refresh()
      },
    })

  const newFile = () =>
    prompt({
      title: "New file",
      placeholder: "File name",
      confirmLabel: "Create",
      hint: (v) => join(path, v || "…"),
      submit: async (name) => {
        await post("/files/touch", { path: join(path, name) })
        toast.success(`Created ${name}`)
        refresh()
        setEditing(join(path, name))
      },
    })

  const cutCopy = (mode: "cut" | "copy", paths: string[]) => {
    if (paths.length === 0) return
    setClip({ mode, paths })
    toast.success(`${mode === "cut" ? "Cut" : "Copied"} ${paths.length} item(s) — paste in any folder`)
  }

  const clipPaths = useMemo(() => new Set(clip?.paths ?? []), [clip])

  return (
    <Page>
      <PageHeader
        eyebrow="Access"
        title="Files"
        description="Browse, edit, organise and transfer files on the host"
        actions={
          <>
            <SearchDialog path={path} onOpen={(p, isDir) => (isDir ? setPath(p) : setEditing(p))} />
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() =>
                    prompt({
                      title: "Go to path",
                      label: "Absolute path",
                      initial: path,
                      confirmLabel: "Go",
                      placeholder: "/var/www",
                      submit: (p) => setPath(clean(p)),
                    })
                  }
                >
                  <FolderInput className="size-4" />
                  Go to…
                </Button>
              </TooltipTrigger>
              <TooltipContent>Jump straight to an absolute path</TooltipContent>
            </Tooltip>
            {selected.length === 0 && (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant={showTree ? "secondary" : "outline"}
                    size="sm"
                    onClick={() => setShowTree((v) => !v)}
                  >
                    <ListTree className="size-4" />
                    Tree
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  {showTree ? "Hide the folder tree" : "Show the folder tree"}
                </TooltipContent>
              </Tooltip>
            )}
            {selected.length > 0 ? (
              <SelectionActions
                count={selected.length}
                canWrite={canWrite}
                canDestruct={canDestruct}
                archiveHref={
                  downloadUrl("/files/archive", { base: path, format: "tar.gz" }) +
                  selected.map((p) => `&path=${encodeURIComponent(p)}`).join("")
                }
                onCopy={() => cutCopy("copy", selected)}
                onCut={() => cutCopy("cut", selected)}
                onDelete={bulkDelete}
                onClear={clearSelection}
              />
            ) : (
              canWrite && (
                <>
                  <NewMenu onFolder={newFolder} onFile={newFile} onSymlink={() => setSymlinkOpen(true)} />
                  <Button size="sm" onClick={() => uploadRef.current?.click()}>
                    <Upload className="size-4" />
                    Upload
                  </Button>
                  <input
                    ref={uploadRef}
                    type="file"
                    multiple
                    hidden
                    onChange={(e) => upload(e.target.files)}
                  />
                </>
              )
            )}
          </>
        }
      />

      <div className="flex min-h-0 gap-3">
        {showTree && (
          <Panel className="hidden w-60 shrink-0 md:flex">
            <FileTree
              root="/"
              statusMap={{}}
              canWrite={canWrite}
              canDelete={canDestruct}
              activeDir={path}
              onNavigate={(p) => setPath(p)}
              onOpenFile={(p) => setEditing(p)}
              onConfirm={(req) =>
                confirm({
                  title: req.title,
                  description: req.body,
                  confirmLabel: req.confirmLabel,
                  action: async () => {
                    await req.run()
                  },
                })
              }
              onChanged={refresh}
            />
          </Panel>
        )}

        <Panel
          className={cn("min-w-0 flex-1", dragOver && "ring-2 ring-primary")}
          onDragOver={(e) => {
            if (!canWrite) return
            e.preventDefault()
            setDragOver(true)
          }}
          onDragLeave={(e) => {
            // Only when leaving the panel itself, not a child.
            if (e.currentTarget === e.target) setDragOver(false)
          }}
          onDrop={(e) => {
            if (!canWrite) return
            e.preventDefault()
            setDragOver(false)
            if (e.dataTransfer.files.length) void upload(e.dataTransfer.files)
          }}
        >
          <PanelToolbar className="justify-between">
            <div className="flex min-w-0 items-center gap-2">
              <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary/12 text-primary">
                <FolderTree className="size-3.5" />
              </span>
              <Breadcrumb className="min-w-0">
                <BreadcrumbList className="gap-1 text-[13px] sm:gap-1">
                  <BreadcrumbItem>
                    <BreadcrumbLink asChild>
                      <button
                        type="button"
                        className="flex size-6 items-center justify-center rounded-md transition-colors hover:bg-accent hover:text-accent-foreground"
                        onClick={() => setPath("/")}
                      >
                        <Home className="size-3.5" />
                        <span className="sr-only">Root</span>
                      </button>
                    </BreadcrumbLink>
                  </BreadcrumbItem>
                  {crumbs.slice(1).map((crumb, i, all) => (
                    <Fragment key={crumb.href}>
                      <BreadcrumbSeparator />
                      <BreadcrumbItem>
                        {i === all.length - 1 ? (
                          <BreadcrumbPage className="font-medium">{crumb.label}</BreadcrumbPage>
                        ) : (
                          <BreadcrumbLink asChild>
                            <button
                              type="button"
                              className="rounded-md px-1.5 py-0.5 transition-colors hover:bg-accent hover:text-accent-foreground"
                              onClick={() => setPath(crumb.href)}
                            >
                              {crumb.label}
                            </button>
                          </BreadcrumbLink>
                        )}
                      </BreadcrumbItem>
                    </Fragment>
                  ))}
                </BreadcrumbList>
              </Breadcrumb>
            </div>
            <div className="flex shrink-0 items-center gap-4">
              {listing.data && (
                <span className="numeric text-[11px] text-muted-foreground">
                  {selected.length > 0
                    ? `${selected.length} selected`
                    : `${entries.length} item${entries.length === 1 ? "" : "s"}`}
                </span>
              )}
              <label className="flex items-center gap-2 text-[11px] text-muted-foreground">
                <Checkbox checked={showHidden} onCheckedChange={(v) => setShowHidden(v === true)} />
                Show hidden
              </label>
            </div>
          </PanelToolbar>

          {clip && (
            <div className="flex items-center gap-2 border-b border-hairline bg-primary/[0.06] px-3 py-1.5 text-xs">
              {clip.mode === "cut" ? (
                <Scissors className="size-3.5 text-primary" />
              ) : (
                <ClipboardPaste className="size-3.5 text-primary" />
              )}
              <span className="text-muted-foreground">
                {clip.paths.length} item{clip.paths.length === 1 ? "" : "s"} ready to{" "}
                {clip.mode === "cut" ? "move" : "copy"}
              </span>
              <span className="flex-1" />
              {canWrite && (
                <Button size="xs" onClick={paste}>
                  <ClipboardPaste className="size-3.5" />
                  Paste here
                </Button>
              )}
              <Button size="xs" variant="ghost" onClick={() => setClip(null)}>
                <X className="size-3.5" />
              </Button>
            </div>
          )}

          <PanelBody flush className="relative">
            {dragOver && (
              <div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center bg-primary/[0.08]">
                <span className="rounded-lg border border-dashed border-primary bg-card px-4 py-2 text-[13px] font-medium text-primary">
                  Drop to upload to {truncateMiddle(path, 40)}
                </span>
              </div>
            )}
            {listing.loading && <LoadingRows className="p-4" />}
            {listing.error && <ErrorState error={listing.error} className="m-4" />}
            {listing.data && (
              <Table containerClassName="max-h-[calc(100svh-17rem)]">
                <TableHeader className={stickyTableHeader}>
                  <TableRow>
                    <TableHead className="w-8">
                      <Checkbox
                        aria-label="Select all"
                        checked={
                          entries.length > 0 && selected.length === entries.length
                            ? true
                            : selected.length > 0
                              ? "indeterminate"
                              : false
                        }
                        onCheckedChange={(v) =>
                          setSelection({
                            dir: path,
                            paths: v === true ? new Set(entries.map((e) => e.path)) : new Set(),
                          })
                        }
                      />
                    </TableHead>
                    <SortHead label="Name" k="name" sort={sort} setSort={setSort} className="w-full" />
                    <SortHead label="Size" k="size" sort={sort} setSort={setSort} className="text-right" />
                    <SortHead label="Modified" k="modified" sort={sort} setSort={setSort} />
                    <SortHead label="Owner" k="owner" sort={sort} setSort={setSort} />
                    <SortHead label="Mode" k="mode" sort={sort} setSort={setSort} className="w-20" />
                    <TableHead className="w-px" />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {path !== "/" && (
                    <TableRow className="select-none" onActivate={() => setPath(listing.data!.parent)}>
                      <TableCell />
                      <TableCell colSpan={6}>
                        <span className="flex items-center gap-2 text-[13px] text-muted-foreground">
                          <ArrowUp className="size-3.5" />
                          Parent directory
                        </span>
                      </TableCell>
                    </TableRow>
                  )}
                  {entries.map((entry) => (
                    <FileRow
                      key={entry.path}
                      entry={entry}
                      selected={selected.includes(entry.path)}
                      dimmed={clip?.mode === "cut" && clipPaths.has(entry.path)}
                      caps={{ write: canWrite, destruct: canDestruct, admin: canAdmin }}
                      onToggle={(checked) =>
                        setSelection((prev) => {
                          const paths = new Set(prev.dir === path ? prev.paths : [])
                          if (checked) paths.add(entry.path)
                          else paths.delete(entry.path)
                          return { dir: path, paths }
                        })
                      }
                      onOpen={() => (entry.isDir ? setPath(entry.path) : setEditing(entry.path))}
                      onRename={() => rename(entry)}
                      onDuplicate={() => duplicate(entry)}
                      onCopy={() => cutCopy("copy", [entry.path])}
                      onCut={() => cutCopy("cut", [entry.path])}
                      onExtract={() => extract(entry)}
                      onPermissions={() => setPermsEntry(entry)}
                      onDelete={() => deleteEntry(entry)}
                    />
                  ))}
                  {entries.length === 0 && (
                    <TableRow>
                      <TableCell colSpan={7} className="p-0">
                        <EmptyState
                          icon={FolderTree}
                          title="This directory is empty"
                          description={canWrite ? "Drop files here to upload, or use New." : undefined}
                        />
                      </TableCell>
                    </TableRow>
                  )}
                </TableBody>
              </Table>
            )}
          </PanelBody>
        </Panel>
      </div>

      <FileEditorSheet
        path={editing}
        onOpenChange={(open) => !open && setEditing(null)}
        onSaved={refresh}
      />
      <PermissionsDialog
        entry={permsEntry}
        onOpenChange={(open) => !open && setPermsEntry(null)}
        onDone={refresh}
      />
      <SymlinkDialog
        open={symlinkOpen}
        dir={path}
        onOpenChange={setSymlinkOpen}
        onDone={refresh}
      />
      {dialog}
      {promptDialog}
    </Page>
  )
}

function SortHead({
  label,
  k,
  sort,
  setSort,
  className,
}: {
  label: string
  k: SortKey
  sort: { key: SortKey; dir: "asc" | "desc" }
  setSort: (s: { key: SortKey; dir: "asc" | "desc" }) => void
  className?: string
}) {
  const active = sort.key === k
  return (
    <TableHead className={className}>
      <button
        className={cn(
          "inline-flex items-center gap-1 hover:text-foreground",
          active && "text-foreground",
          className?.includes("text-right") && "flex-row-reverse",
        )}
        onClick={() => setSort({ key: k, dir: active && sort.dir === "asc" ? "desc" : "asc" })}
      >
        {label}
        {active &&
          (sort.dir === "asc" ? (
            <ChevronUp className="size-3" />
          ) : (
            <ChevronDown className="size-3" />
          ))}
      </button>
    </TableHead>
  )
}

function SelectionActions({
  count,
  canWrite,
  canDestruct,
  archiveHref,
  onCopy,
  onCut,
  onDelete,
  onClear,
}: {
  count: number
  canWrite: boolean
  canDestruct: boolean
  archiveHref: string
  onCopy: () => void
  onCut: () => void
  onDelete: () => void
  onClear: () => void
}) {
  return (
    <>
      <span className="numeric text-[13px] text-muted-foreground">{count} selected</span>
      {canWrite && (
        <>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="outline" size="sm" onClick={onCopy}>
                <ClipboardPaste className="size-4" />
                Copy
              </Button>
            </TooltipTrigger>
            <TooltipContent>Hold these to paste into another folder</TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button variant="outline" size="sm" onClick={onCut}>
                <Scissors className="size-4" />
                Cut
              </Button>
            </TooltipTrigger>
            <TooltipContent>Hold these to move into another folder</TooltipContent>
          </Tooltip>
        </>
      )}
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="outline" size="sm" asChild>
            <a href={archiveHref} download>
              <FileArchive className="size-4" />
              Archive
            </a>
          </Button>
        </TooltipTrigger>
        <TooltipContent>Download the selection as one .tar.gz</TooltipContent>
      </Tooltip>
      {canDestruct && (
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="outline" size="sm" className="text-destructive" onClick={onDelete}>
              <Trash2 className="size-4" />
              Delete
            </Button>
          </TooltipTrigger>
          <TooltipContent>Delete the selection permanently</TooltipContent>
        </Tooltip>
      )}
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="ghost" size="sm" aria-label="Clear selection" onClick={onClear}>
            <X className="size-4" />
          </Button>
        </TooltipTrigger>
        <TooltipContent>Clear the selection</TooltipContent>
      </Tooltip>
    </>
  )
}

function NewMenu({
  onFolder,
  onFile,
  onSymlink,
}: {
  onFolder: () => void
  onFile: () => void
  onSymlink: () => void
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm">
          <Plus className="size-4" />
          New
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-44">
        <DropdownMenuItem onSelect={onFolder}>
          <FolderPlus className="size-3.5" />
          Folder
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={onFile}>
          <FilePlus className="size-3.5" />
          File
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={onSymlink}>
          <Link2 className="size-3.5" />
          Symlink
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function SymlinkDialog({
  open,
  dir,
  onOpenChange,
  onDone,
}: {
  open: boolean
  dir: string
  onOpenChange: (open: boolean) => void
  onDone: () => void
}) {
  // The body is only mounted while open (Radix unmounts closed content), so its
  // fields start empty every time without a reset effect.
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        {open && (
          <SymlinkBody dir={dir} onClose={() => onOpenChange(false)} onDone={onDone} />
        )}
      </DialogContent>
    </Dialog>
  )
}

function SymlinkBody({
  dir,
  onClose,
  onDone,
}: {
  dir: string
  onClose: () => void
  onDone: () => void
}) {
  const [target, setTarget] = useState("")
  const [name, setName] = useState("")
  const [busy, setBusy] = useState(false)

  const create = async () => {
    if (!target.trim() || !name.trim()) return
    setBusy(true)
    try {
      await post("/files/symlink", { target: target.trim(), link: join(dir, name.trim()) })
      toast.success(`Created link ${name.trim()}`)
      onDone()
      onClose()
    } catch (err) {
      toast.error("Could not create symlink", { description: String(err) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>New symlink</DialogTitle>
      </DialogHeader>
      <div className="space-y-3">
          <div className="space-y-1.5">
            <label className="text-xs text-muted-foreground">Points at</label>
            <Input
              autoFocus
              value={target}
              onChange={(e) => setTarget(e.target.value)}
              placeholder="/path/it/points/to"
              className="font-mono text-xs"
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-xs text-muted-foreground">Link name</label>
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && create()}
              placeholder="link-name"
              className="font-mono text-xs"
            />
            <p className="font-mono text-[11px] break-all text-muted-foreground">
              {join(dir, name || "…")} → {target || "…"}
            </p>
          </div>
        </div>
      <DialogFooter>
        <Button variant="ghost" onClick={onClose} disabled={busy}>
          Cancel
        </Button>
        <Button onClick={create} disabled={busy || !target.trim() || !name.trim()}>
          Create link
        </Button>
      </DialogFooter>
    </>
  )
}

function SearchDialog({
  path,
  onOpen,
}: {
  path: string
  onOpen: (path: string, isDir: boolean) => void
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const [content, setContent] = useState(false)
  const [regex, setRegex] = useState(false)
  const [hits, setHits] = useState<
    { path: string; name: string; isDir: boolean; line?: number; snippet?: string }[]
  >([])
  const [busy, setBusy] = useState(false)

  const run = async () => {
    if (!query) return
    setBusy(true)
    try {
      setHits(await get("/files/search", { path, q: query, content, regex, limit: 200 }))
    } catch (err) {
      toast.error("Search failed", { description: String(err) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">
          <Search className="size-4" />
          Search
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Search under {truncateMiddle(path, 40)}</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">
          <div className="flex gap-2">
            <Input
              autoFocus
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && run()}
              placeholder={content ? "Text to find inside files" : "File or directory name"}
            />
            <Button onClick={run} disabled={busy || !query}>
              Search
            </Button>
          </div>
          <div className="flex flex-wrap gap-4">
            <label className="flex items-center gap-2 text-[13px]">
              <Checkbox checked={content} onCheckedChange={(v) => setContent(v === true)} />
              Search inside file contents
            </label>
            <label className="flex items-center gap-2 text-[13px]">
              <Checkbox checked={regex} onCheckedChange={(v) => setRegex(v === true)} />
              Regular expression
            </label>
          </div>
          <div className="max-h-80 space-y-0.5 overflow-auto">
            {hits.map((hit) => (
              <button
                key={`${hit.path}:${hit.line ?? 0}`}
                className="block w-full rounded-md px-2 py-1.5 text-left hover:bg-accent"
                onClick={() => {
                  onOpen(hit.path, hit.isDir)
                  setOpen(false)
                }}
              >
                <span className="block truncate font-mono text-xs">{hit.path}</span>
                {hit.snippet && (
                  <span className="block truncate text-[11px] text-muted-foreground">
                    line {hit.line}: {hit.snippet}
                  </span>
                )}
              </button>
            ))}
            {!busy && hits.length === 0 && query && (
              <p className="py-4 text-center text-[13px] text-muted-foreground">No matches.</p>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
