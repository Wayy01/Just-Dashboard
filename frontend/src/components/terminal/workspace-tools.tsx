"use client"

import { useEffect, useMemo, useState } from "react"
import {
  FileCode,
  FolderTree,
  GitBranch as GitBranchIcon,
  Loader2,
  PanelRightClose,
  Save,
  X,
} from "lucide-react"
import { toast } from "sonner"
import { get, put } from "@/lib/api"
import { bytes } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { FileContent, GitDetect, GitFileChange, GitStatus } from "@/lib/types"
import { usePoll } from "@/hooks/use-poll"
import { useAuth } from "@/hooks/use-auth"
import { CodeEditor } from "@/components/code-editor"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { FileTree, type ConfirmRequest } from "@/components/terminal/file-tree"
import { GitTools } from "@/components/terminal/git-tools"

type Overlay =
  | { kind: "file"; path: string }
  | { kind: "diff"; title: string; subtitle?: string; body: string }
  | null

/**
 * The Files + Git companion for the terminal.
 *
 * It owns the git detection and status polls once for both tabs — the tree
 * borrows the status to badge changed files, and the git tab drives them — and
 * it owns every surface that would otherwise be a portalled dialog: the file
 * viewer/editor, the diff, and the confirm. They are drawn *inside* this panel
 * so the whole thing keeps working when the workspace is in the browser's real
 * fullscreen, where a portal to document.body renders outside the fullscreen
 * element and vanishes.
 */
export function WorkspaceTools({
  dir,
  onOpenInFiles,
  onClose,
}: {
  dir?: string
  onOpenInFiles: (path: string) => void
  onClose?: () => void
}) {
  const { can } = useAuth()
  const [tab, setTab] = useState<"files" | "git">("files")
  const [overlay, setOverlay] = useState<Overlay>(null)
  const [confirm, setConfirm] = useState<ConfirmRequest | null>(null)

  const detect = usePoll(
    (signal) =>
      dir
        ? get<GitDetect>("/git/detect", { path: dir }, signal)
        : Promise.resolve({ available: true } as GitDetect),
    15000,
    [dir],
    { enabled: Boolean(dir) },
  )

  const repoPath = detect.data?.inRoots ? detect.data.repo?.path : undefined

  const status = usePoll(
    (signal) =>
      repoPath
        ? get<GitStatus>("/git/status", { path: repoPath }, signal)
        : Promise.resolve(null as unknown as GitStatus),
    8000,
    [repoPath],
    { enabled: Boolean(repoPath) },
  )

  // Absolute path → git status, so the tree can badge a changed file wherever
  // it sits. git reports paths relative to the repository root.
  const statusMap = useMemo(() => {
    const map: Record<string, GitFileChange> = {}
    if (repoPath && status.data?.files) {
      for (const f of status.data.files) map[`${repoPath.replace(/\/$/, "")}/${f.path}`] = f
    }
    return map
  }, [repoPath, status.data])

  const treeRoot = repoPath ?? dir
  const refreshGit = () => {
    detect.refresh()
    status.refresh()
  }

  return (
    <div className="relative flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-xl border bg-card">
      <div className="flex shrink-0 items-center gap-0.5 border-b border-hairline bg-surface-header px-1.5 py-1">
        <TabButton active={tab === "files"} onClick={() => setTab("files")} icon={FolderTree}>
          Files
        </TabButton>
        <TabButton active={tab === "git"} onClick={() => setTab("git")} icon={GitBranchIcon}>
          Git
          {(status.data?.files.length ?? 0) > 0 && (
            <span className="ml-1 rounded bg-warning/20 px-1 font-mono text-[10px] text-warning">
              {status.data?.files.length}
            </span>
          )}
          {detect.data?.inRoots && detect.data.repo && !status.data?.files.length && (
            <span className="ml-1 size-1.5 rounded-full bg-success" title="clean checkout" />
          )}
        </TabButton>
        <span className="flex-1" />
        {onClose && (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            title="Hide this panel"
            aria-label="Hide this panel"
            className="size-6 shrink-0 p-0 text-muted-foreground hover:text-foreground"
            onClick={onClose}
          >
            <PanelRightClose className="size-3.5" />
          </Button>
        )}
      </div>

      <div className="relative flex min-h-0 flex-1 flex-col">
        {!treeRoot ? (
          <EmptyState
            className="m-3"
            icon={FolderTree}
            title="No session"
            description="Open or pick a session and its files and git status show up here."
          />
        ) : tab === "files" ? (
          <FileTree
            key={treeRoot}
            root={treeRoot}
            statusMap={statusMap}
            canWrite={can("file.write")}
            canDelete={can("destructive")}
            activeFile={overlay?.kind === "file" ? overlay.path : undefined}
            onOpenFile={(path) => setOverlay({ kind: "file", path })}
            onConfirm={setConfirm}
            onChanged={refreshGit}
            onOpenInFiles={onOpenInFiles}
          />
        ) : (
          <GitTools
            dir={dir ?? ""}
            detect={detect.data}
            detectLoading={detect.loading}
            detectError={detect.error}
            status={status}
            canControl={can("service.control")}
            canDestruct={can("destructive")}
            onShowDiff={(d) => setOverlay({ kind: "diff", ...d })}
            onConfirm={setConfirm}
            onChanged={refreshGit}
          />
        )}

        {overlay?.kind === "file" && (
          <InlineFile
            // Keyed on the path so opening another file starts clean rather
            // than inheriting the previous file's unsaved draft.
            key={overlay.path}
            path={overlay.path}
            canWrite={can("file.write")}
            onClose={() => setOverlay(null)}
            onSaved={refreshGit}
          />
        )}
        {overlay?.kind === "diff" && (
          <InlineDiff
            title={overlay.title}
            subtitle={overlay.subtitle}
            body={overlay.body}
            onClose={() => setOverlay(null)}
          />
        )}
      </div>

      {confirm && (
        <InlineConfirm request={confirm} onClose={() => setConfirm(null)} />
      )}
    </div>
  )
}

function TabButton({
  active,
  onClick,
  icon: Icon,
  children,
}: {
  active: boolean
  onClick: () => void
  icon: React.ComponentType<{ className?: string }>
  children: React.ReactNode
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "flex items-center gap-1.5 rounded-md px-2.5 py-1 text-[12px] font-medium transition-colors",
        active ? "bg-primary/12 text-primary" : "text-muted-foreground hover:text-foreground",
      )}
    >
      <Icon className="size-3.5" />
      {children}
    </button>
  )
}

/** The file viewer/editor, drawn over the panel body rather than in a sheet. */
function InlineFile({
  path,
  canWrite,
  onClose,
  onSaved,
}: {
  path: string
  canWrite: boolean
  onClose: () => void
  onSaved: () => void
}) {
  const [file, setFile] = useState<FileContent>()
  const [draft, setDraft] = useState("")
  const [error, setError] = useState<Error>()
  const [saving, setSaving] = useState(false)

  // The component is keyed on the path by its caller, so it mounts fresh for
  // each file and this only ever fetches — no synchronous reset needed.
  useEffect(() => {
    const controller = new AbortController()
    get<FileContent>("/files/read", { path }, controller.signal)
      .then((f) => {
        setFile(f)
        setDraft(f.content)
      })
      .catch((err) => !controller.signal.aborted && setError(err))
    return () => controller.abort()
  }, [path])

  const dirty = file !== undefined && draft !== file.content

  const save = async () => {
    setSaving(true)
    try {
      await put("/files/write", { path, content: draft })
      toast.success("Saved", { description: path })
      setFile((f) => (f ? { ...f, content: draft } : f))
      onSaved()
    } catch (err) {
      toast.error("Could not save", { description: String(err) })
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="absolute inset-0 z-20 flex flex-col bg-card">
      <div className="flex shrink-0 items-center gap-2 border-b border-hairline bg-surface-header px-2 py-1.5">
        <FileCode className="size-3.5 shrink-0 text-primary" />
        <span className="min-w-0 flex-1 truncate font-mono text-[12px]" title={path}>
          {path.split("/").pop()}
        </span>
        {dirty && (
          <Badge variant="warning" className="font-normal">
            unsaved
          </Badge>
        )}
        {file && !file.binary && canWrite && (
          <Button size="xs" onClick={save} disabled={!dirty || saving}>
            {saving ? <Loader2 className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
            Save
          </Button>
        )}
        <Button
          size="sm"
          variant="ghost"
          className="size-6 p-0 text-muted-foreground hover:text-foreground"
          title="Close (Esc)"
          aria-label="Close"
          onClick={onClose}
        >
          <X className="size-3.5" />
        </Button>
      </div>
      <div className="min-h-0 flex-1">
        {error && <ErrorState error={error} className="m-3" />}
        {!file && !error && <LoadingRows className="p-3" rows={6} />}
        {file?.binary && (
          <div className="p-4 text-xs text-muted-foreground">
            This looks like a binary file ({bytes(file.size)}); it is not shown here.
          </div>
        )}
        {file && !file.binary && (
          <CodeEditor
            className="h-full"
            value={draft}
            onChange={setDraft}
            language={file.language}
            readOnly={!canWrite}
          />
        )}
      </div>
    </div>
  )
}

/** A unified diff, coloured, drawn over the panel body. */
function InlineDiff({
  title,
  subtitle,
  body,
  onClose,
}: {
  title: string
  subtitle?: string
  body: string
  onClose: () => void
}) {
  return (
    <div className="absolute inset-0 z-20 flex flex-col bg-card">
      <div className="flex shrink-0 items-start gap-2 border-b border-hairline bg-surface-header px-2 py-1.5">
        <div className="min-w-0 flex-1">
          <p className="truncate font-mono text-[12px]" title={title}>
            {title}
          </p>
          {subtitle && <p className="truncate text-[10px] text-muted-foreground">{subtitle}</p>}
        </div>
        <Button
          size="sm"
          variant="ghost"
          className="size-6 shrink-0 p-0 text-muted-foreground hover:text-foreground"
          title="Close (Esc)"
          aria-label="Close"
          onClick={onClose}
        >
          <X className="size-3.5" />
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        <pre className="p-3 font-mono text-[11px] leading-relaxed">
          {body.split("\n").map((line, i) => {
            let cls = ""
            if (line.startsWith("+") && !line.startsWith("+++")) cls = "text-success"
            else if (line.startsWith("-") && !line.startsWith("---")) cls = "text-destructive"
            else if (line.startsWith("@@")) cls = "text-primary"
            else if (line.startsWith("diff ") || line.startsWith("index "))
              cls = "text-muted-foreground"
            return (
              <div key={i} className={cn("whitespace-pre", cls)}>
                {line || " "}
              </div>
            )
          })}
        </pre>
      </div>
    </div>
  )
}

/**
 * The confirm surface for the destructive things in this panel — deleting a
 * file, discarding a change. Non-portalled on purpose (fullscreen), and a plain
 * yes/no rather than a typed phrase: the panel supplies the phrase the server
 * still requires, and the file's name shown in the body is the thing being
 * confirmed. Esc cancels, Enter confirms.
 */
function InlineConfirm({ request, onClose }: { request: ConfirmRequest; onClose: () => void }) {
  const [busy, setBusy] = useState(false)
  const runIt = async () => {
    setBusy(true)
    try {
      await request.run()
      onClose()
    } catch (err) {
      toast.error("That did not go through", { description: String(err) })
      setBusy(false)
    }
  }
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose()
      if (e.key === "Enter") void runIt()
    }
    window.addEventListener("keydown", onKey, { capture: true })
    return () => window.removeEventListener("keydown", onKey, { capture: true })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  return (
    <div className="absolute inset-0 z-30 flex items-center justify-center bg-background/70 p-3 backdrop-blur-sm">
      <div className="w-full max-w-sm rounded-xl border bg-card p-4 shadow-lg">
        <p className="text-[13px] font-medium">{request.title}</p>
        {request.body && (
          <div className="mt-1.5 text-xs leading-relaxed text-muted-foreground">{request.body}</div>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <Button size="sm" variant="ghost" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button
            size="sm"
            variant={request.danger ? "destructive" : "default"}
            onClick={runIt}
            disabled={busy}
          >
            {busy && <Loader2 className="size-3.5 animate-spin" />}
            {request.confirmLabel ?? "Confirm"}
          </Button>
        </div>
      </div>
    </div>
  )
}
