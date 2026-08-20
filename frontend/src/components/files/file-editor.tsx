"use client"

import { useEffect, useState } from "react"
import { FileCode, Loader2, Save, ShieldAlert } from "lucide-react"
import { toast } from "sonner"
import { get, post, put } from "@/lib/api"
import { bytes } from "@/lib/format"
import type { FileContent } from "@/lib/types"
import { useAuth } from "@/hooks/use-auth"
import { CodeEditor } from "@/components/code-editor"
import { SidePanel } from "@/components/side-panel"
import { ErrorState, LoadingRows, Notice } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

export function FileEditorSheet({
  path,
  onOpenChange,
  onSaved,
}: {
  path: string | null
  onOpenChange: (open: boolean) => void
  onSaved?: () => void
}) {
  return (
    <FileEditorPanel
      // Keyed on the path: opening another file must not inherit the previous
      // file's unsaved draft.
      key={path ?? "none"}
      path={path}
      onOpenChange={onOpenChange}
      onSaved={onSaved}
    />
  )
}

function FileEditorPanel({
  path,
  onOpenChange,
  onSaved,
}: {
  path: string | null
  onOpenChange: (open: boolean) => void
  onSaved?: () => void
}) {
  const { can } = useAuth()
  const [file, setFile] = useState<FileContent>()
  const [draft, setDraft] = useState("")
  const [error, setError] = useState<Error>()
  const [saving, setSaving] = useState(false)
  const [mode, setMode] = useState("")

  useEffect(() => {
    if (!path) return
    const controller = new AbortController()
    get<FileContent>("/files/read", { path }, controller.signal)
      .then((f) => {
        setFile(f)
        setDraft(f.content)
        setMode(f.modeOctal)
      })
      .catch((err) => !controller.signal.aborted && setError(err))
    return () => controller.abort()
  }, [path])

  const dirty = file !== undefined && draft !== file.content

  const save = async () => {
    if (!path) return
    setSaving(true)
    try {
      await put("/files/write", { path, content: draft })
      toast.success("Saved", { description: path })
      setFile((f) => (f ? { ...f, content: draft } : f))
      onSaved?.()
    } catch (err) {
      toast.error("Could not save", { description: String(err) })
    } finally {
      setSaving(false)
    }
  }

  const applyMode = async () => {
    if (!path) return
    try {
      await post("/files/chmod", { path, mode })
      toast.success(`Mode set to ${mode}`)
      onSaved?.()
    } catch (err) {
      toast.error("Could not change mode", { description: String(err) })
    }
  }

  return (
    <SidePanel
      open={path !== null}
      onOpenChange={onOpenChange}
      width="xl"
      icon={FileCode}
      title={
        <>
          {path?.split("/").pop() ?? "File"}
          {dirty && (
            <Badge variant="warning" className="font-normal">
              unsaved
            </Badge>
          )}
        </>
      }
      description={path ?? undefined}
      bodyClassName="flex min-h-0 flex-1 flex-col"
      footer={
        file && can("file.write") ? (
          <>
            <Label htmlFor="file-mode" className="text-xs text-muted-foreground">
              Mode
            </Label>
            <Input
              id="file-mode"
              value={mode}
              onChange={(e) => setMode(e.target.value)}
              className="h-8 w-20 font-mono text-xs"
            />
            <Button
              size="sm"
              variant="outline"
              onClick={applyMode}
              disabled={mode === file.modeOctal}
            >
              Apply
            </Button>
            <span className="flex-1" />
            <span className="numeric text-xs text-muted-foreground">{bytes(draft.length)}</span>
            <Button size="sm" onClick={save} disabled={!dirty || saving || file.binary}>
              {saving ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
              Save
            </Button>
          </>
        ) : undefined
      }
    >
      {error && <ErrorState error={error} className="m-4" />}
      {!file && !error && <LoadingRows className="p-4" />}

      {file?.binary && (
        <Notice className="m-4" tone="warning" title="Binary file" icon={ShieldAlert}>
          This looks like a binary file ({bytes(file.size)}); it is not shown in the editor.
        </Notice>
      )}

      {file && !file.binary && (
        <CodeEditor
          className="flex-1"
          value={draft}
          onChange={setDraft}
          language={file.language}
          readOnly={!can("file.write")}
        />
      )}
    </SidePanel>
  )
}
