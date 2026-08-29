"use client"

import { useConfirm } from "@/components/confirm-dialog"
import { Page } from "@/components/page"
import { StructureTab } from "@/components/database/structure-tab"
import { useDatabase } from "@/components/database/db-context"

export default function StructurePage() {
  const { conn, info, selection } = useDatabase()
  const { confirm, dialog } = useConfirm()
  if (!conn) return null
  return (
    <Page fill>
      <div className="min-h-0 min-w-0 flex-1 overflow-y-auto">
        <StructureTab
          conn={conn}
          info={info}
          confirm={confirm}
          schema={selection.schema}
          table={selection.table}
        />
      </div>
      {dialog}
    </Page>
  )
}
