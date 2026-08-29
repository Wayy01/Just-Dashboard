"use client"

import { useConfirm } from "@/components/confirm-dialog"
import { Page } from "@/components/page"
import { BrowseTab } from "@/components/database/browse-tab"
import { RedisBrowser } from "@/components/database/redis-browser"
import { MongoBrowser } from "@/components/database/mongo-browser"
import { useDatabase } from "@/components/database/db-context"

export default function BrowsePage() {
  const { conn, info, selection, select } = useDatabase()
  const { confirm, dialog } = useConfirm()
  if (!conn) return null

  const sel = selection.table ? { schema: selection.schema, table: selection.table } : null

  return (
    <Page fill>
      <div className="min-h-0 min-w-0 flex-1 overflow-y-auto">
        {conn.driver === "redis" ? (
          <RedisBrowser conn={conn} confirm={confirm} />
        ) : conn.driver === "mongodb" ? (
          <MongoBrowser conn={conn} confirm={confirm} />
        ) : (
          <BrowseTab
            conn={conn}
            info={info}
            confirm={confirm}
            selection={sel}
            onSelect={(s) => select({ schema: s?.schema ?? "", table: s?.table ?? null })}
          />
        )}
      </div>
      {dialog}
    </Page>
  )
}
