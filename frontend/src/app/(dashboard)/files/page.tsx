"use client"

import { Fragment, useMemo, useRef, useState } from "react"
import {
  ArrowUp,
  Download,
  File as FileIcon,
  FileArchive,
  Folder,
  FolderPlus,
  FolderTree,
  Home,
  Link as LinkIcon,
  Pencil,
  Search,
  Trash2,
  Upload,
} from "lucide-react"
import { toast } from "sonner"
import { API_BASE, del, downloadUrl, get, post } from "@/lib/api"
import { bytes, relativeTime, truncateMiddle } from "@/lib/format"
import type { FileEntry, FileListing } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { useConfirm } from "@/components/confirm-dialog"
import { Page, PageHeader } from "@/components/page"
import { Panel, PanelBody, PanelToolbar } from "@/components/panel"
import { FileEditorSheet } from "@/components/files/file-editor"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { IconAction } from "@/components/icon-action"
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

export default function FilesPage() {
  const { can } = useAuth()
  const { confirm, dialog } = useConfirm()
  const [path, setPath] = useState("/")
  const [showHidden, setShowHidden] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)
  // The selection is scoped to the directory it was made in, so navigating
  // away discards it without a reset effect.
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

  const upload = async (files: FileList | null) => {
    if (!files?.length) return
    const form = new FormData()
    for (const file of Array.from(files)) form.append("file", file)
    try {
      const res = await fetch(
        `${API_BASE}/files/upload?path=${encodeURIComponent(path)}&overwrite=true`,
        { method: "POST", credentials: "include", body: form },
      )
      if (!res.ok) throw new Error((await res.json()).error?.message ?? res.statusText)
      toast.success(`Uploaded ${files.length} file(s)`)
      listing.refresh()
    } catch (err) {
      toast.error("Upload failed", { description: String(err) })
    } finally {
      if (uploadRef.current) uploadRef.current.value = ""
    }
  }

  return (
    <Page>
      <PageHeader
        eyebrow="Access"
        title="Files"
        description="Browse, edit and transfer files on the host"
        actions={
          <>
            <SearchDialog path={path} onOpen={(p, isDir) => (isDir ? setPath(p) : setEditing(p))} />
            {selected.length > 0 && (
              <Button variant="outline" size="sm" asChild>
                <a
                  href={
                    downloadUrl("/files/archive", { base: path, format: "tar.gz" }) +
                    selected.map((p) => `&path=${encodeURIComponent(p)}`).join("")
                  }
                  download
                >
                  <FileArchive className="size-4" />
                  Archive {selected.length}
                </a>
              </Button>
            )}
            {can("file.write") && (
              <>
                <NewFolderDialog path={path} onDone={listing.refresh} />
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
            )}
          </>
        }
      />

      <Panel>
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
                  : `${listing.data.entries.length} item${listing.data.entries.length === 1 ? "" : "s"}`}
              </span>
            )}
            <label className="flex items-center gap-2 text-[11px] text-muted-foreground">
              <Checkbox checked={showHidden} onCheckedChange={(v) => setShowHidden(v === true)} />
              Show hidden
            </label>
          </div>
        </PanelToolbar>

        <PanelBody flush>
          {listing.loading && <LoadingRows className="p-4" />}
          {listing.error && <ErrorState error={listing.error} className="m-4" />}
          {listing.data && (
            <Table containerClassName="max-h-[calc(100svh-17rem)]">
              <TableHeader className={stickyTableHeader}>
                <TableRow>
                  <TableHead className="w-8" />
                  <TableHead className="w-full">Name</TableHead>
                  <TableHead className="text-right">Size</TableHead>
                  <TableHead>Modified</TableHead>
                  <TableHead>Owner</TableHead>
                  <TableHead className="w-20">Mode</TableHead>
                  <TableHead className="w-px" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {path !== "/" && (
                  <TableRow
                    className="select-none"
                    onActivate={() => setPath(listing.data!.parent)}
                  >
                    <TableCell />
                    <TableCell colSpan={6}>
                      <span className="flex items-center gap-2 text-[13px] text-muted-foreground">
                        <ArrowUp className="size-3.5" />
                        Parent directory
                      </span>
                    </TableCell>
                  </TableRow>
                )}
                {listing.data.entries.map((entry) => (
                  <FileRow
                    key={entry.path}
                    entry={entry}
                    selected={selected.includes(entry.path)}
                    onToggle={(checked) =>
                      setSelection((prev) => {
                        const paths = new Set(prev.dir === path ? prev.paths : [])
                        if (checked) paths.add(entry.path)
                        else paths.delete(entry.path)
                        return { dir: path, paths }
                      })
                    }
                    onOpen={() => (entry.isDir ? setPath(entry.path) : setEditing(entry.path))}
                    onDelete={() =>
                      confirm({
                        title: entry.isDir ? "Delete directory" : "Delete file",
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
                          listing.refresh()
                        },
                      })
                    }
                  />
                ))}
                {listing.data.entries.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={7} className="p-0">
                      <EmptyState icon={Folder} title="This directory is empty" />
                    </TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          )}
        </PanelBody>
      </Panel>

      <FileEditorSheet
        path={editing}
        onOpenChange={(open) => !open && setEditing(null)}
        onSaved={listing.refresh}
      />
      {dialog}
    </Page>
  )
}

function FileRow({
  entry,
  selected,
  onToggle,
  onOpen,
  onDelete,
}: {
  entry: FileEntry
  selected: boolean
  onToggle: (checked: boolean) => void
  onOpen: () => void
  onDelete: () => void
}) {
  const { can } = useAuth()
  // Double-click opens, the way every file manager works. The name stays
  // clickable on its own for a single deliberate click, but hitting those few
  // characters exactly is no longer the only way in.
  const openOnEnter = (e: React.KeyboardEvent<HTMLTableRowElement>) => {
    if (e.key !== "Enter" || e.target !== e.currentTarget) return
    e.preventDefault()
    onOpen()
  }

  return (
    <TableRow
      className="group cursor-pointer select-none"
      data-state={selected ? "selected" : undefined}
      tabIndex={0}
      onDoubleClick={onOpen}
      onKeyDown={openOnEnter}
    >
      {/* The controls carry their own gestures; a double-click on them is not
          a request to open the row underneath. */}
      <TableCell onDoubleClick={(e) => e.stopPropagation()}>
        <Checkbox
          checked={selected}
          onCheckedChange={(v) => onToggle(v === true)}
          aria-label={`Select ${entry.name}`}
        />
      </TableCell>
      <TableCell>
        <div className="max-w-[26rem] min-w-0">
          <button
            className="flex max-w-full items-center gap-2 text-left text-[13px] hover:underline"
            onClick={onOpen}
            title={entry.name}
          >
            {entry.isDir ? (
              <Folder className="size-4 shrink-0 fill-primary/20 text-primary" />
            ) : entry.isSymlink ? (
              <LinkIcon className="size-4 shrink-0 text-muted-foreground" />
            ) : (
              <FileIcon className="size-4 shrink-0 text-muted-foreground" />
            )}
            <span className="truncate">{entry.name}</span>
          </button>
          {entry.isSymlink && (
            <p className="ml-6 truncate font-mono text-[11px] text-muted-foreground">
              → {entry.linkTarget}
              {entry.linkBroken && (
                <Badge variant="destructive" className="ml-1 text-[10px] font-normal">
                  broken
                </Badge>
              )}
            </p>
          )}
        </div>
      </TableCell>
      <TableCell className="numeric text-right font-mono text-xs text-muted-foreground">
        {entry.isDir ? "—" : bytes(entry.size)}
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {relativeTime(entry.modified)}
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {entry.owner}:{entry.group}
      </TableCell>
      <TableCell className="font-mono text-xs text-muted-foreground">{entry.modeOctal}</TableCell>
      <TableCell onDoubleClick={(e) => e.stopPropagation()}>
        <div className="flex justify-end gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100">
          {!entry.isDir && (
            <>
              <IconAction label="Edit" onClick={onOpen}>
                <Pencil />
              </IconAction>
              <IconAction label="Download" asChild>
                <a href={downloadUrl("/files/download", { path: entry.path })} download>
                  <Download />
                </a>
              </IconAction>
            </>
          )}
          {can("destructive") && (
            <IconAction label="Delete" className="text-destructive" onClick={onDelete}>
              <Trash2 />
            </IconAction>
          )}
        </div>
      </TableCell>
    </TableRow>
  )
}

function NewFolderDialog({ path, onDone }: { path: string; onDone: () => void }) {
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")

  const create = async () => {
    try {
      await post("/files/mkdir", { path: `${path.replace(/\/$/, "")}/${name}` })
      toast.success(`Created ${name}`)
      setOpen(false)
      setName("")
      onDone()
    } catch (err) {
      toast.error("Could not create directory", { description: String(err) })
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="outline" size="sm">
          <FolderPlus className="size-4" />
          New folder
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>New folder</DialogTitle>
        </DialogHeader>
        <div className="space-y-2">
          <Input
            autoFocus
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && create()}
            placeholder="Folder name"
          />
          <p className="font-mono text-xs break-all text-muted-foreground">
            {path.replace(/\/$/, "")}/{name || "…"}
          </p>
        </div>
        <DialogFooter>
          <Button onClick={create} disabled={!name}>
            Create
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
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
  const [hits, setHits] = useState<
    { path: string; name: string; isDir: boolean; line?: number; snippet?: string }[]
  >([])
  const [busy, setBusy] = useState(false)

  const run = async () => {
    if (!query) return
    setBusy(true)
    try {
      setHits(await get("/files/search", { path, q: query, content, limit: 200 }))
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
          <label className="flex items-center gap-2 text-[13px]">
            <Checkbox checked={content} onCheckedChange={(v) => setContent(v === true)} />
            Search inside file contents
          </label>
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
