import type { CSSProperties } from "react"
import type { GitFileChange } from "@/lib/types"

/**
 * What a changed file looks like, in one place.
 *
 * The letter, the colour and the sentence are the same three facts in the file
 * tree, the terminal's git tab and the Git page, and they were being derived
 * separately in each — which is how "M" ended up amber in one list and grey in
 * another for the same file. A status is also the one label a reader arrives
 * already knowing: green added, red deleted, amber modified. Anything that
 * only marks *whether* a file changed throws that away and makes them read the
 * letter.
 *
 * The colours are the `--git-*` tokens; every surface here is color-mix
 * against the card, so one set of hues holds on a near-black and a near-white
 * palette alike.
 */
export type GitTone = "added" | "modified" | "deleted" | "untracked" | "renamed" | "conflict"

/**
 * A file's tone from its status.
 *
 * Conflict wins over everything: a `UU` file is "modified" by the label and is
 * the one thing in the list that will not commit.
 */
export function gitTone(change: GitFileChange): GitTone {
  if (change.label === "conflicted" || change.index === "U" || change.worktree === "U") {
    return "conflict"
  }
  switch (change.label) {
    case "added":
      return "added"
    case "deleted":
      return "deleted"
    case "untracked":
      return "untracked"
    case "renamed":
    case "copied":
      return "renamed"
    default:
      return "modified"
  }
}

/** The one-letter mark git itself uses, so the two agree. */
export function gitLetter(change: GitFileChange): string {
  if (change.label === "untracked") return "U"
  const raw = change.staged ? change.index : change.worktree || change.index
  return (raw || "?").toUpperCase()
}

/** The status as a sentence, for a tooltip. */
export function describeChange(change: GitFileChange): string {
  const where = change.staged ? "staged" : "not staged"
  return gitTone(change) === "conflict"
    ? "conflicted — resolve before committing"
    : `${change.label} · ${where}`
}

/**
 * The colour, plus the tint drawn behind the row.
 *
 * Returned as a style object rather than classes because the value is a token
 * chosen at runtime; Tailwind cannot generate a class per status without the
 * six of them being written out somewhere to be scanned, which is the same
 * table twice.
 */
export function gitStyle(tone: GitTone): CSSProperties {
  const colour = `var(--git-${tone})`
  return {
    // The row tint is deliberately faint: a list where every second line is a
    // coloured band is harder to read than one with none.
    "--git-colour": colour,
    "--git-tint": `color-mix(in oklab, ${colour} 10%, transparent)`,
    "--git-edge": `color-mix(in oklab, ${colour} 55%, transparent)`,
  } as CSSProperties
}
