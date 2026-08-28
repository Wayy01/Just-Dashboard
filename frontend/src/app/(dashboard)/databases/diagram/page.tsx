"use client"

import { Page } from "@/components/page"
import { ErDiagram } from "@/components/database/er-diagram"
import { useDatabase } from "@/components/database/db-context"

export default function DiagramPage() {
  const { conn, selection, goto } = useDatabase()
  if (!conn) return null
  return (
    <Page fill>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <ErDiagram
          conn={conn}
          schema={selection.schema}
          onOpenTable={(schema, table) => goto("/databases", { schema, table })}
        />
      </div>
    </Page>
  )
}
