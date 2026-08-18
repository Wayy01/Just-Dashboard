"use client"

import { useEffect, useState } from "react"
import dynamic from "next/dynamic"
import { Loader2, Save, ShieldAlert } from "lucide-react"
import { toast } from "sonner"
import { get, post, put } from "@/lib/api"
import { bytes } from "@/lib/format"
import type { FileContent } from "@/lib/types"
import { useAuth } from "@/hooks/use-auth"
import { ErrorState, LoadingRows } from "@/components/state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet"

// Monaco pulls in a large worker bundle and touches `window`, so it is loaded
// only when an editor is actually opened.
const MonacoEditor = dynamic(() => import("@monaco-editor/react"), {
  ssr: false,
  loading: () => (
    <div className="flex h-full items-center justify-center">
      <Loader2 className="size-5 animate-spin text-muted-foreground" />
    </div>
  ),
})

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
    <Sheet open={path !== null} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="flex w-full flex-col gap-0 p-0 sm:max-w-4xl">
        {/* Keyed on the path: opening another file must not inherit the
            previous file's unsaved draft. */}
        {path && <FileEditorBody key={path} path={path} onSaved={onSaved} />}
      </SheetContent>
    </Sheet>
  )
}

function FileEditorBody({ path, onSaved }: { path: string; onSaved?: () => void }) {
  const { can } = useAuth()
  const [file, setFile] = useState<FileContent>()
  const [draft, setDraft] = useState("")
  const [error, setError] = useState<Error>()
  const [saving, setSaving] = useState(false)
  const [mode, setMode] = useState("")

  useEffect(() => {
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
    try {
      await post("/files/chmod", { path, mode })
      toast.success(`Mode set to ${mode}`)
      onSaved?.()
    } catch (err) {
      toast.error("Could not change mode", { description: String(err) })
    }
  }

  return (
    <>
      <SheetHeader className="border-b p-4">
        <SheetTitle className="flex items-center gap-2 truncate">
          {path.split("/").pop()}
          {dirty && <Badge variant="destructive">unsaved</Badge>}
        </SheetTitle>
        <SheetDescription className="truncate font-mono text-xs">{path}</SheetDescription>
      </SheetHeader>

      {error && <ErrorState error={error} className="m-4" />}
      {!file && !error && <LoadingRows className="p-4" />}

      {file?.binary && (
        <div className="m-4 flex items-center gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-sm">
          <ShieldAlert className="size-4 text-amber-400" />
          This looks like a binary file ({bytes(file.size)}); it is not shown in the editor.
        </div>
      )}

      {file && !file.binary && (
        <div className="min-h-0 flex-1">
          <MonacoEditor
            height="100%"
            theme="vs-dark"
            language={file.language}
            value={draft}
            onChange={(value) => setDraft(value ?? "")}
            options={{
              readOnly: !can("file.write"),
              minimap: { enabled: false },
              fontSize: 13,
              scrollBeyondLastLine: false,
              automaticLayout: true,
              tabSize: 2,
              renderWhitespace: "selection",
            }}
          />
        </div>
      )}

      {file && can("file.write") && (
        <SheetFooter className="flex-row items-center gap-2 border-t p-4">
          <div className="flex items-center gap-2">
            <Label htmlFor="file-mode" className="text-xs text-muted-foreground">
              Mode
            </Label>
            <Input
              id="file-mode"
              value={mode}
              onChange={(e) => setMode(e.target.value)}
              className="w-20 font-mono"
            />
            <Button
              size="sm"
              variant="outline"
              onClick={applyMode}
              disabled={mode === file.modeOctal}
            >
              Apply
            </Button>
          </div>
          <span className="flex-1" />
          <span className="text-xs text-muted-foreground">{bytes(draft.length)}</span>
          <Button onClick={save} disabled={!dirty || saving || file.binary}>
            {saving ? <Loader2 className="size-4 animate-spin" /> : <Save className="size-4" />}
            Save
          </Button>
        </SheetFooter>
      )}
    </>
  )
}
