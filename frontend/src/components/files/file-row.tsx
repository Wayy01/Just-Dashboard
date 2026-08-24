"use client"

import {
  Clipboard,
  Copy,
  CornerUpRight,
  Download,
  File as FileIcon,
  FileArchive,
  Folder,
  Link as LinkIcon,
  MoreHorizontal,
  Pencil,
  Scissors,
  Shield,
  Trash2,
} from "lucide-react"
import { notify } from "@/lib/toast"
import { downloadUrl } from "@/lib/api"
import { bytes, relativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"
import type { FileEntry } from "@/lib/types"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { Checkbox } from "@/components/ui/checkbox"
import { IconAction } from "@/components/icon-action"
import { TableCell, TableRow } from "@/components/ui/table"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

/** Archives the backend can extract in place. */
const ARCHIVE_RE = /\.(zip|tar|tar\.gz|tgz)$/i
export const isArchive = (name: string) => ARCHIVE_RE.test(name)

export type RowCaps = { write: boolean; destruct: boolean; admin: boolean }

/**
 * One row of the file listing, with every operation the backend supports
 * reachable from it: open, download, rename, duplicate, copy, cut, extract,
 * permissions and delete. The common two (edit, download) stay as quick icons;
 * the rest live in a menu so the row is not a wall of buttons, and so an action
 * that only makes sense sometimes — extract on an archive, permissions for an
 * admin — appears only when it does.
 */
export function FileRow({
  entry,
  selected,
  dimmed,
  caps,
  onToggle,
  onOpen,
  onRename,
  onDuplicate,
  onCopy,
  onCut,
  onExtract,
  onPermissions,
  onDelete,
}: {
  entry: FileEntry
  selected: boolean
  /** Faded because it is on the clipboard waiting to be moved. */
  dimmed?: boolean
  caps: RowCaps
  onToggle: (checked: boolean) => void
  onOpen: () => void
  onRename: () => void
  onDuplicate: () => void
  onCopy: () => void
  onCut: () => void
  onExtract: () => void
  onPermissions: () => void
  onDelete: () => void
}) {
  const openOnEnter = (e: React.KeyboardEvent<HTMLTableRowElement>) => {
    if (e.key !== "Enter" || e.target !== e.currentTarget) return
    e.preventDefault()
    onOpen()
  }
  const copyPath = () =>
    navigator.clipboard
      ?.writeText(entry.path)
      .then(() => notify.success("Path copied"))
      .catch(() => notify.error("The browser refused clipboard access"))

  const archive = !entry.isDir && isArchive(entry.name)

  return (
    <TableRow
      className={cn("group cursor-pointer select-none", dimmed && "opacity-50")}
      data-state={selected ? "selected" : undefined}
      tabIndex={0}
      onDoubleClick={onOpen}
      onKeyDown={openOnEnter}
    >
      <TableCell onDoubleClick={(e) => e.stopPropagation()}>
        <Checkbox
          checked={selected}
          onCheckedChange={(v) => onToggle(v === true)}
          aria-label={`Select ${entry.name}`}
        />
      </TableCell>
      <TableCell>
        <div className="max-w-[26rem] min-w-0">
          <button
            className="flex max-w-full items-center gap-2 text-left text-[13px] hover:underline"
            onClick={onOpen}
            title={entry.name}
          >
            {entry.isDir ? (
              <Folder className="size-4 shrink-0 fill-primary/20 text-primary" />
            ) : entry.isSymlink ? (
              <LinkIcon className="size-4 shrink-0 text-muted-foreground" />
            ) : archive ? (
              <FileArchive className="size-4 shrink-0 text-muted-foreground" />
            ) : (
              <FileIcon className="size-4 shrink-0 text-muted-foreground" />
            )}
            <span className="truncate">{entry.name}</span>
          </button>
          {entry.isSymlink && (
            <p className="ml-6 truncate font-mono text-[11px] text-muted-foreground">
              → {entry.linkTarget}
              {entry.linkBroken && (
                <Badge variant="destructive" className="ml-1 text-[10px] font-normal">
                  broken
                </Badge>
              )}
            </p>
          )}
        </div>
      </TableCell>
      <TableCell className="numeric text-right font-mono text-xs text-muted-foreground">
        {entry.isDir ? "—" : bytes(entry.size)}
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {relativeTime(entry.modified)}
      </TableCell>
      <TableCell className="text-xs text-muted-foreground">
        {entry.owner}:{entry.group}
      </TableCell>
      <TableCell className="font-mono text-xs text-muted-foreground">{entry.modeOctal}</TableCell>
      <TableCell onDoubleClick={(e) => e.stopPropagation()}>
        <div className="flex justify-end gap-0.5 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100 [@media(hover:none)]:opacity-100">
          {!entry.isDir && (
            <>
              <IconAction label="Edit" onClick={onOpen}>
                <Pencil />
              </IconAction>
              <IconAction label="Download" asChild>
                <a href={downloadUrl("/files/download", { path: entry.path })} download>
                  <Download />
                </a>
              </IconAction>
            </>
          )}
          <DropdownMenu>
            <Tooltip>
              <TooltipTrigger asChild>
                <DropdownMenuTrigger asChild>
                  <Button
                    size="sm"
                    variant="ghost"
                    aria-label="More actions"
                    className="size-7 p-0 text-muted-foreground hover:text-foreground"
                  >
                    <MoreHorizontal className="size-3.5" />
                  </Button>
                </DropdownMenuTrigger>
              </TooltipTrigger>
              <TooltipContent>Rename, move, copy, permissions, delete</TooltipContent>
            </Tooltip>
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuItem onSelect={onOpen}>
                {entry.isDir ? (
                  <>
                    <Folder className="size-3.5" />
                    Open
                  </>
                ) : (
                  <>
                    <Pencil className="size-3.5" />
                    Open / edit
                  </>
                )}
              </DropdownMenuItem>
              {!entry.isDir && (
                <DropdownMenuItem asChild>
                  <a href={downloadUrl("/files/download", { path: entry.path })} download>
                    <Download className="size-3.5" />
                    Download
                  </a>
                </DropdownMenuItem>
              )}
              <DropdownMenuItem onSelect={copyPath}>
                <Clipboard className="size-3.5" />
                Copy path
              </DropdownMenuItem>

              {caps.write && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem onSelect={onRename}>
                    <Pencil className="size-3.5" />
                    Rename
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={onDuplicate}>
                    <Copy className="size-3.5" />
                    Duplicate
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={onCopy}>
                    <Copy className="size-3.5" />
                    Copy
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={onCut}>
                    <Scissors className="size-3.5" />
                    Cut
                  </DropdownMenuItem>
                  {archive && (
                    <DropdownMenuItem onSelect={onExtract}>
                      <CornerUpRight className="size-3.5" />
                      Extract here
                    </DropdownMenuItem>
                  )}
                </>
              )}

              {caps.admin && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem onSelect={onPermissions}>
                    <Shield className="size-3.5" />
                    Permissions…
                  </DropdownMenuItem>
                </>
              )}

              {caps.destruct && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    className="text-destructive focus:text-destructive"
                    onSelect={onDelete}
                  >
                    <Trash2 className="size-3.5" />
                    Delete
                  </DropdownMenuItem>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </TableCell>
    </TableRow>
  )
}
