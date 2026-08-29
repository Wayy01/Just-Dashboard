"use client"

import { useConfirm } from "@/components/confirm-dialog"
import { Page } from "@/components/page"
import { QueryTab } from "@/components/database/query-tab"
import { useDatabase } from "@/components/database/db-context"

export default function QueryPage() {
  const { conn } = useDatabase()
  const { confirm, dialog } = useConfirm()
  if (!conn) return null
  return (
    <Page fill>
      <div className="min-h-0 min-w-0 flex-1 overflow-y-auto">
        <QueryTab conn={conn} confirm={confirm} />
      </div>
      {dialog}
    </Page>
  )
}
