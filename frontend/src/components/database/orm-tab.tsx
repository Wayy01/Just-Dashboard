"use client"

import { useState } from "react"
import { Copy, Download, Sparkles } from "lucide-react"
import { toast } from "sonner"
import { post } from "@/lib/api"
import type { DbConnection, OrmTarget } from "@/lib/types"
import { CodeEditor } from "@/components/code-editor"
import { Button } from "@/components/ui/button"
import { Panel, PanelBody, PanelFooter, PanelHeader } from "@/components/panel"
import { EmptyState, Notice, Spinner } from "@/components/state"
import { Database } from "lucide-react"

/**
 * ORM support, done the way a server panel honestly can: introspect the live
 * database and generate the schema file a developer would otherwise get from
 * `prisma db pull` or `drizzle-kit pull`, with no Node toolchain on the box.
 * The result is a reviewed starting point, which the generated header and the
 * notice here both say plainly.
 */
export function OrmTab({ conn, schema }: { conn: DbConnection; schema: string }) {
  const [target, setTarget] = useState<OrmTarget>("prisma")
  const [output, setOutput] = useState<{ schema: string; filename: string } | null>(null)
  const [busy, setBusy] = useState(false)

  if (conn.driver === "mongodb") {
    return (
      <Notice tone="default" title="Not available for MongoDB">
        ORM schema generation introspects a relational catalogue. Use your driver&apos;s native
        tooling for a document database.
      </Notice>
    )
  }

  const generate = async (t: OrmTarget) => {
    setTarget(t)
    setBusy(true)
    try {
      const res = await post<{ schema: string; filename: string }>(`/databases/${conn.id}/orm`, {
        target: t,
        schema,
      })
      setOutput(res)
    } catch (err) {
      toast.error("Generation failed", { description: String(err) })
    } finally {
      setBusy(false)
    }
  }

  const download = () => {
    if (!output) return
    const blob = new Blob([output.schema], { type: "text/plain" })
    const url = URL.createObjectURL(blob)
    const a = document.createElement("a")
    a.href = url
    a.download = output.filename
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <Panel>
      <PanelHeader
        icon={Sparkles}
        title="Generate ORM schema"
        description="Introspect this database into a Prisma or Drizzle schema"
        actions={
          <div className="flex items-center gap-1.5">
            <Button
              size="sm"
              variant={target === "prisma" ? "default" : "outline"}
              onClick={() => generate("prisma")}
              disabled={busy}
            >
              {busy && target === "prisma" ? <Spinner /> : null}
              Prisma
            </Button>
            <Button
              size="sm"
              variant={target === "drizzle" ? "default" : "outline"}
              onClick={() => generate("drizzle")}
              disabled={busy}
            >
              {busy && target === "drizzle" ? <Spinner /> : null}
              Drizzle
            </Button>
          </div>
        }
      />
      <PanelBody flush>
        {!output && !busy && (
          <EmptyState
            icon={Database}
            title="Nothing generated yet"
            description="Choose Prisma or Drizzle above to introspect the current schema into a ready-to-review file."
          />
        )}
        {busy && !output && (
          <div className="flex items-center justify-center gap-2 p-10 text-sm text-muted-foreground">
            <Spinner /> Introspecting {schema || "database"}…
          </div>
        )}
        {output && (
          <CodeEditor
            className="h-[calc(100svh-24rem)]"
            language={output.filename.endsWith(".ts") ? "typescript" : "prisma"}
            value={output.schema}
            readOnly
          />
        )}
      </PanelBody>
      {output && (
        <PanelFooter>
          <Button
            size="sm"
            variant="outline"
            onClick={() =>
              navigator.clipboard
                .writeText(output.schema)
                .then(() => toast.success("Copied schema"))
                .catch(() => toast.error("Could not copy"))
            }
          >
            <Copy className="size-3.5" />
            Copy
          </Button>
          <Button size="sm" variant="outline" onClick={download}>
            <Download className="size-3.5" />
            Download {output.filename}
          </Button>
          <span className="text-xs text-muted-foreground">
            A reviewed starting point — verify types and relations before committing.
          </span>
        </PanelFooter>
      )}
    </Panel>
  )
}
