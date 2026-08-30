"use client"

import { BlendMode, Check } from "@/components/icons"
import { cn } from "@/lib/utils"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

/**
 * Colour tags for the terminal rail.
 *
 * The rail's problem, before this, was that a folder, a session and a window
 * were three different things drawn as three near-identical rows: same height,
 * same weight, same neutral grey. Fixing that is mostly typography and
 * indentation — but the part typography cannot do is tell you at a glance that
 * *these four* rows belong to production and *those* to the staging box, which
 * is exactly the question somebody with fifteen sessions is asking.
 *
 * So a folder can be painted, and what is inside it inherits the paint. The
 * inheritance is the whole point: colouring eight sessions by hand is work
 * nobody does twice, and a group whose members are individually grey is not a
 * group. A session may still override, for the one root shell inside an
 * otherwise ordinary folder that should be impossible to mistake.
 *
 * The values live in globals.css as `--tag-*`; only the ids are here.
 */
export const TAG_COLOURS = [
  { id: "slate", label: "Grey" },
  { id: "red", label: "Red" },
  { id: "amber", label: "Amber" },
  { id: "green", label: "Green" },
  { id: "cyan", label: "Cyan" },
  { id: "blue", label: "Blue" },
  { id: "violet", label: "Violet" },
  { id: "pink", label: "Pink" },
] as const

export type TagColour = (typeof TAG_COLOURS)[number]["id"]

const IDS = new Set<string>(TAG_COLOURS.map((c) => c.id))

/**
 * The CSS value for a tag, or undefined when there is nothing to draw.
 *
 * An unknown id resolves to nothing rather than to a default colour: the
 * server drops what it does not recognise, and a client that invented a
 * fallback would paint rows a colour no record holds.
 */
export function tagVar(colour?: string | null): string | undefined {
  if (!colour || !IDS.has(colour)) return undefined
  return `var(--tag-${colour})`
}

/**
 * Everything a tagged row needs, as one style object.
 *
 * `--tag` is set on the element rather than passed down as a class so the
 * children can mix against it — a tinted background, a hairline in the same
 * hue, a swatch at full strength — without any of them having to know which
 * colour was chosen. An untagged row simply has no `--tag`, and every rule
 * that reads it falls back through `color-mix(... transparent)` to nothing.
 */
export function tagStyle(colour?: string | null): React.CSSProperties | undefined {
  const value = tagVar(colour)
  return value ? ({ "--tag": value } as React.CSSProperties) : undefined
}

/** A filled dot in the tag's colour: the smallest possible legend. */
export function TagSwatch({ colour, className }: { colour?: string | null; className?: string }) {
  const value = tagVar(colour)
  return (
    <span
      className={cn("size-2 shrink-0 rounded-full", className)}
      style={{
        backgroundColor: value ?? "transparent",
        boxShadow: value ? undefined : "inset 0 0 0 1px var(--hairline)",
      }}
    />
  )
}

/**
 * The colour choices as bare menu items, so the same list can sit in a menu of
 * its own or nested as a submenu of a row's overflow menu. A row with two
 * separate menus on it — one for colour, one for everything else — is two
 * things to find where there should be one.
 *
 * A menu rather than a row of swatches: eight circles are faster to click and
 * slower to understand, because the colours have no names on screen and a
 * keyboard user gets eight unlabelled buttons.
 */
export function ColourMenuItems({
  colour,
  onPick,
  inherited,
}: {
  colour?: string | null
  onPick: (colour: string) => void
  /** Names where the fallback comes from, where something supplies one. */
  inherited?: string
}) {
  return (
    <>
      <DropdownMenuItem className="gap-2 text-xs" onSelect={() => onPick("")}>
        <TagSwatch colour={null} />
        <span className="flex-1">{inherited ? `Inherit (${inherited})` : "None"}</span>
        {!colour && <Check className="size-3.5" />}
      </DropdownMenuItem>
      {TAG_COLOURS.map((c) => (
        <DropdownMenuItem key={c.id} className="gap-2 text-xs" onSelect={() => onPick(c.id)}>
          <TagSwatch colour={c.id} />
          <span className="flex-1">{c.label}</span>
          {colour === c.id && <Check className="size-3.5" />}
        </DropdownMenuItem>
      ))}
    </>
  )
}

/** The same list as a menu of its own, for a control that only sets colour. */
export function ColourMenu({
  colour,
  onPick,
  children,
  inherited,
}: {
  colour?: string | null
  onPick: (colour: string) => void
  children: React.ReactNode
  inherited?: string
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-44">
        <DropdownMenuLabel className="flex items-center gap-1.5 text-xs">
          <BlendMode className="size-3.5" />
          Colour
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <ColourMenuItems colour={colour} onPick={onPick} inherited={inherited} />
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
