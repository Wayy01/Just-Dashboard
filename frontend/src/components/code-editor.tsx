"use client"

import { useEffect, useRef } from "react"
import dynamic from "next/dynamic"
import { Loader2 } from "lucide-react"
import { cn } from "@/lib/utils"
import { useTheme } from "@/hooks/use-theme"

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
  completions,
}: {
  value: string
  onChange?: (value: string) => void
  language?: string
  readOnly?: boolean
  className?: string
  minHeight?: string
  /**
   * Names to offer as completions, as a parent-to-children map — tables to
   * their columns for SQL. Kept as a plain map rather than a Monaco type so
   * this component does not drag the editor's types into every caller.
   */
  completions?: Record<string, string[]>
}) {
  const { mode } = useTheme()
  // Held in a ref so the provider registered on mount always reads the current
  // schema. Re-registering on every change would stack providers and offer each
  // table name once per registration.
  const completionsRef = useRef(completions)
  useEffect(() => {
    completionsRef.current = completions
  }, [completions])

  return (
    <div className={cn("monaco-host min-h-0", className)} style={{ minHeight }}>
      <MonacoEditor
        height="100%"
        theme={mode === "light" ? "vs" : "vs-dark"}
        language={language}
        value={value}
        onChange={(v) => onChange?.(v ?? "")}
        onMount={(_editor, monaco) => {
          if (!completions || !language) return
          registerSchemaCompletions(monaco, language, completionsRef)
        }}
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


/**
 * Registers a schema-aware completion provider once per language.
 *
 * Monaco's providers are global to the language rather than scoped to an editor
 * instance, so registering one per mount would leave every previous editor's
 * provider attached and offer each table name once per editor ever opened. One
 * registration reading from a module-level slot is what keeps the list to one
 * copy, and writing the current editor's schema into that slot on mount is what
 * makes the suggestions follow the connection being looked at.
 */
const activeSchema: Record<string, () => Record<string, string[]> | undefined> = {}

function registerSchemaCompletions(
  monaco: typeof import("monaco-editor"),
  language: string,
  ref: { current?: Record<string, string[]> },
) {
  const first = !(language in activeSchema)
  activeSchema[language] = () => ref.current
  if (!first) return

  monaco.languages.registerCompletionItemProvider(language, {
    provideCompletionItems(model, position) {
      const tables = activeSchema[language]?.()
      if (!tables) return { suggestions: [] }
      const word = model.getWordUntilPosition(position)
      const range = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: word.startColumn,
        endColumn: word.endColumn,
      }
      const line = model.getValueInRange({
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: 1,
        endColumn: position.column,
      })

      // After `table.` only that table's columns make sense. Offering the whole
      // schema there is what makes most SQL completion useless.
      const qualified = /([A-Za-z_][\w$]*)\.\w*$/.exec(line)
      if (qualified) {
        const cols = tables[qualified[1]] ?? []
        return {
          suggestions: cols.map((c) => ({
            label: c,
            kind: monaco.languages.CompletionItemKind.Field,
            insertText: c,
            range,
          })),
        }
      }

      return {
        suggestions: [
          ...Object.keys(tables).map((t) => ({
            label: t,
            kind: monaco.languages.CompletionItemKind.Struct,
            insertText: t,
            detail: `${(tables[t] ?? []).length} columns`,
            range,
          })),
          // Unqualified column names, de-duplicated across tables: that is what
          // people actually type in a single-table query.
          ...[...new Set(Object.values(tables).flat())].map((c) => ({
            label: c,
            kind: monaco.languages.CompletionItemKind.Field,
            insertText: c,
            range,
          })),
        ],
      }
    },
  })
}
