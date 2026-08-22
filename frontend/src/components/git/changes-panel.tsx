"use client"

import { useState } from "react"
import { Check, GitCommitHorizontal, Minus, Plus, RotateCcw, Upload } from "lucide-react"
import { toast } from "sonner"
import { get, post } from "@/lib/api"
import { cn } from "@/lib/utils"
import type { GitFileChange, GitResult, GitStatus } from "@/lib/types"
import type { usePoll } from "@/hooks/use-poll"
import type { ConfirmRequest } from "@/components/confirm-dialog"
import { GitExplain } from "@/components/git/help"
import type { GitPreview } from "@/components/git/preview-panel"
import { EmptyState, ErrorState, LoadingRows } from "@/components/state"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"

type GitRun = (label: string, fn: () => Promise<GitResult>) => Promise<GitResult>

/**
 * The staging-and-commit half of the workspace, written to be legible to
 * someone who has never staged anything: two labelled lists — what is about to
 * be committed, and what is not yet chosen — and a commit box that spells the
 * step out. Each file carries the one or two actions that make sense for where
 * it is, and clicking its name shows the diff in the preview column.
 */
export function ChangesPanel({
  repoPath,
  status,
  busy,
  canControl,
  canDestruct,
  run,
  confirm,
  onSelectDiff,
  activePath,
}: {
  repoPath: string
  status: ReturnType<typeof usePoll<GitStatus>>
  busy?: string
  canControl: boolean
  canDestruct: boolean
  run: GitRun
  confirm: (req: ConfirmRequest) => void
  onSelectDiff: (p: GitPreview) => void
  activePath?: string
}) {
  const [message, setMessage] = useState("")
  const [amend, setAmend] = useState(false)
  const q = { path: repoPath }

  const showDiff = async (file: GitFileChange, staged: boolean) => {
    try {
      const res = await get<{ diff: string }>("/git/diff", {
        path: repoPath,
        file: file.path,
        staged: staged ? "true" : undefined,
      })
      onSelectDiff({
        kind: "diff",
        title: file.path,
        subtitle: staged ? "staged — ready to commit" : `working tree — ${file.label}`,
        body: res.diff || "No textual diff (binary file, or no line changes).",
      })
    } catch (err) {
      toast.error("Could not read the diff", { description: String(err) })
    }
  }

  // Fire-and-forget: `run` already reports failures, so the rejection is
  // swallowed here rather than left to surface as an unhandled one.
  const stage = (files: string[]) =>
    void run("Staged", () => post<GitResult>("/git/stage", { files }, { query: q })).catch(() => {})
  const unstage = (files: string[]) =>
    void run("Unstaged", () => post<GitResult>("/git/unstage", { files }, { query: q })).catch(() => {})

  const commit = async (thenPush: boolean) => {
    const msg = message.trim()
    if (!msg && !amend) {
      toast.error("Write a short message describing your changes first")
      return
    }
    try {
      await run("Committed", () =>
        post<GitResult>("/git/commit", { message: msg, amend }, { query: q }),
      )
      setMessage("")
      setAmend(false)
      if (thenPush) await run("Pushed", () => post<GitResult>("/git/push", undefined, { query: q }))
    } catch {
      /* run already surfaced it */
    }
  }

  const discard = (file: GitFileChange) =>
    confirm({
      title: `Discard changes to ${file.path.split("/").pop()}`,
      phrase: "discard changes",
      confirmLabel: "Discard",
      description: (
        <p className="text-destructive">
          Puts <span className="font-mono break-all">{file.path}</span> back the way it was at the
          last commit. The current edits are not recoverable.
        </p>
      ),
      action: async (c) => {
        await post("/git/discard", { file: file.path }, { confirm: c, query: q })
        status.refresh()
      },
    })

  const discardAll = () =>
    confirm({
      title: "Discard every change",
      phrase: "reset hard",
      confirmLabel: "Discard all",
      description: (
        <p className="text-destructive">
          Every tracked file is put back to the last commit. Untracked files are left alone. This
          cannot be undone.
        </p>
      ),
      action: async (c) => {
        await post("/git/reset", { ref: "HEAD", hard: true }, { confirm: c, query: q })
        status.refresh()
      },
    })

  if (status.error) return <ErrorState error={status.error} className="m-3" />
  if (status.loading && !status.data) return <LoadingRows className="p-3" rows={5} />

  const files = status.data?.files ?? []
  const staged = files.filter((f) => f.staged)
  const unstaged = files.filter((f) => !f.staged)
  const canCommit = staged.length > 0 && (message.trim().length > 0 || amend)

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="min-h-0 flex-1 overflow-auto">
        {files.length === 0 ? (
          <div className="p-4">
            <EmptyState
              icon={Check}
              title="Nothing to commit"
              description="You have no uncommitted changes. Edit a file — here or in a terminal — and it will show up ready to stage."
            />
          </div>
        ) : (
          <>
            {staged.length > 0 && (
              <Group
                label="Ready to commit"
                explain="stage"
                count={staged.length}
                tone="success"
                action={
                  canControl && (
                    <GroupAction disabled={!!busy} onClick={() => unstage([])}>
                      Unstage all
                    </GroupAction>
                  )
                }
              >
                {staged.map((f) => (
                  <FileRow
                    key={"s" + f.path}
                    file={f}
                    active={activePath === f.path}
                    onClick={() => showDiff(f, true)}
                    actions={
                      canControl && (
                        <RowAction label="Unstage" disabled={!!busy} onClick={() => unstage([f.path])}>
                          <Minus className="size-3.5" />
                        </RowAction>
                      )
                    }
                  />
                ))}
              </Group>
            )}

            {unstaged.length > 0 && (
              <Group
                label="Changes"
                explain="changes"
                count={unstaged.length}
                tone="warning"
                action={
                  <>
                    {canDestruct && (
                      <GroupAction danger disabled={!!busy} onClick={discardAll}>
                        Discard all
                      </GroupAction>
                    )}
                    {canControl && (
                      <GroupAction disabled={!!busy} onClick={() => stage([])}>
                        Stage all
                      </GroupAction>
                    )}
                  </>
                }
              >
                {unstaged.map((f) => (
                  <FileRow
                    key={"u" + f.path}
                    file={f}
                    active={activePath === f.path}
                    onClick={() => showDiff(f, false)}
                    actions={
                      <>
                        {canDestruct && f.label !== "untracked" && (
                          <RowAction
                            label="Discard"
                            className="text-destructive"
                            disabled={!!busy}
                            onClick={() => discard(f)}
                          >
                            <RotateCcw className="size-3.5" />
                          </RowAction>
                        )}
                        {canControl && (
                          <RowAction label="Stage" disabled={!!busy} onClick={() => stage([f.path])}>
                            <Plus className="size-3.5" />
                          </RowAction>
                        )}
                      </>
                    }
                  />
                ))}
              </Group>
            )}
          </>
        )}
      </div>

      {canControl && (
        <div className="shrink-0 space-y-2 border-t border-hairline bg-surface-header/60 p-3">
          <Textarea
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter" && canCommit) {
                e.preventDefault()
                void commit(false)
              }
            }}
            placeholder={
              amend ? "New message (leave blank to keep the previous one)" : "Describe what you changed…"
            }
            rows={3}
            className="resize-none font-mono text-[12px]"
          />
          <div className="flex items-center gap-2">
            <label className="flex cursor-pointer items-center gap-1.5 text-[11px] text-muted-foreground">
              <input
                type="checkbox"
                checked={amend}
                onChange={(e) => setAmend(e.target.checked)}
                className="size-3 accent-[var(--primary)]"
              />
              Amend last commit
            </label>
            <span className="flex-1" />
            <Button
              size="sm"
              variant="outline"
              disabled={!!busy || !canCommit}
              onClick={() => void commit(true)}
              title="Commit the staged changes, then push them to the remote"
            >
              <Upload className="size-3.5" />
              Commit &amp; push
            </Button>
            <Button
              size="sm"
              disabled={!!busy || !canCommit}
              onClick={() => void commit(false)}
              title="Commit the staged changes (Ctrl+Enter)"
            >
              <GitCommitHorizontal className="size-3.5" />
              Commit
            </Button>
          </div>
          {staged.length === 0 && (
            <p className="text-[11px] text-muted-foreground">
              Stage at least one change above before committing.
            </p>
          )}
        </div>
      )}
    </div>
  )
}

function Group({
  label,
  explain,
  count,
  tone,
  action,
  children,
}: {
  label: string
  explain: string
  count: number
  tone: "success" | "warning"
  action?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <div>
      <div className="sticky top-0 z-10 flex items-center gap-2 border-b border-hairline bg-surface-header/95 px-3 py-1.5 backdrop-blur">
        <span className={cn("size-1.5 rounded-full", tone === "success" ? "bg-success" : "bg-warning")} />
        <span className="text-[12px] font-medium">{label}</span>
        <GitExplain name={explain} />
        <span className="numeric text-[11px] text-muted-foreground">{count}</span>
        <span className="flex-1" />
        {action}
      </div>
      {children}
    </div>
  )
}

function FileRow({
  file,
  active,
  onClick,
  actions,
}: {
  file: GitFileChange
  active?: boolean
  onClick: () => void
  actions?: React.ReactNode
}) {
  return (
    <div
      className={cn(
        "group flex min-w-0 items-center gap-2 px-3 py-1.5 hover:bg-[var(--row-hover)]",
        active && "bg-primary/10",
      )}
    >
      <span
        className={cn(
          "w-5 shrink-0 text-center font-mono text-[10px]",
          file.label === "deleted" ? "text-destructive" : "text-muted-foreground",
        )}
        title={file.label}
      >
        {file.label === "untracked" ? "U" : (file.worktree || file.index || "?").toUpperCase()}
      </span>
      <button
        onClick={onClick}
        className="min-w-0 flex-1 truncate text-left font-mono text-[12px] hover:underline"
        title={`${file.path} — ${file.label}`}
      >
        {file.path}
      </button>
      <div className="flex shrink-0 items-center opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100">
        {actions}
      </div>
    </div>
  )
}

function RowAction({
  label,
  disabled,
  className,
  onClick,
  children,
}: {
  label: string
  disabled?: boolean
  className?: string
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant="ghost"
      title={label}
      aria-label={label}
      disabled={disabled}
      className={cn("size-6 shrink-0 p-0 text-muted-foreground hover:text-foreground", className)}
      onClick={onClick}
    >
      {children}
    </Button>
  )
}

function GroupAction({
  disabled,
  danger,
  onClick,
  children,
}: {
  disabled?: boolean
  danger?: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <button
      disabled={disabled}
      onClick={onClick}
      className={cn(
        "rounded px-1.5 py-0.5 text-[11px] transition-colors disabled:opacity-40",
        danger
          ? "text-destructive hover:bg-destructive/10"
          : "text-muted-foreground hover:bg-accent hover:text-foreground",
      )}
    >
      {children}
    </button>
  )
}
