"use client"

import dynamic from "next/dynamic"
import { Loader2 } from "lucide-react"
import { cn } from "@/lib/utils"
import { useTheme } from "@/hooks/use-theme"

// Monaco pulls in a large worker bundle and touches `window`, so it is loaded
// only when an editor is actually opened — and it is loaded from *here*.
//
// @monaco-editor/react defaults to fetching the editor from
// cdn.jsdelivr.net at runtime. That is third-party JavaScript running in the
// same origin as a session that drives the Docker socket, systemd and a root
// shell, which this product's whole security argument rules out; and it means
// every editor in the dashboard — files, compose, nginx, the site preview —
// is a spinner that never resolves for an operator whose workstation has no
// egress, which is exactly the workstation this is meant to be reached from.
// `scripts/sync-monaco.mjs` puts the files under public/monaco, and the config
// happens inside the import so it is applied before any editor can mount.
const MonacoEditor = dynamic(
  () =>
    import("@monaco-editor/react").then((mod) => {
      mod.loader.config({ paths: { vs: "/monaco/vs" } })
      return mod.default
    }),
  {
    ssr: false,
    loading: () => (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="size-5 animate-spin text-muted-foreground" />
      </div>
    ),
  },
)

/**
 * Monaco, wired to the dashboard's palette.
 *
 * Three places embedded the editor and all three hard-coded `theme="vs-dark"`,
 * which put a slate-grey slab inside a card in every one of the three light
 * palettes. Monaco has no notion of our tokens, so the fix is in two parts:
 * pick its built-in light or dark base from the active mode, and let the
 * container behind it own the ground (see `.monaco-host` in globals.css) so
 * the editor blends into whatever surface it was dropped onto.
 */
export function CodeEditor({
  value,
  onChange,
  language,
  readOnly,
  className,
  minHeight,
}: {
  value: string
  onChange?: (value: string) => void
  language?: string
  readOnly?: boolean
  className?: string
  minHeight?: string
}) {
  const { mode } = useTheme()

  return (
    <div className={cn("monaco-host min-h-0", className)} style={{ minHeight }}>
      <MonacoEditor
        height="100%"
        theme={mode === "light" ? "vs" : "vs-dark"}
        language={language}
        value={value}
        onChange={(v) => onChange?.(v ?? "")}
        options={{
          readOnly,
          minimap: { enabled: false },
          fontSize: 13,
          lineHeight: 20,
          scrollBeyondLastLine: false,
          automaticLayout: true,
          tabSize: 2,
          renderLineHighlight: "none",
          renderWhitespace: "selection",
          padding: { top: 12, bottom: 12 },
          scrollbar: { verticalScrollbarSize: 10, horizontalScrollbarSize: 10 },
          overviewRulerLanes: 0,
        }}
      />
    </div>
  )
}
