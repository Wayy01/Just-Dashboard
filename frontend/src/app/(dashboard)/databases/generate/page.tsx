"use client"

import { Page } from "@/components/page"
import { OrmTab } from "@/components/database/orm-tab"
import { useDatabase } from "@/components/database/db-context"

export default function GeneratePage() {
  const { conn, selection } = useDatabase()
  if (!conn) return null
  return (
    <Page fill>
      <div className="min-h-0 min-w-0 flex-1 overflow-y-auto">
        <OrmTab conn={conn} schema={selection.schema} />
      </div>
    </Page>
  )
}
