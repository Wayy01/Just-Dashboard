/**
 * The palettes the dashboard ships with.
 *
 * Every entry here has a matching `[data-theme="<id>"]` block in
 * `app/themes.css`, and that block is the only place colour is defined — this
 * file carries the name, the blurb and which mode the palette belongs to.
 * Switching themes is therefore one attribute write, which is what makes it
 * cheap enough to do before first paint.
 */

export type ThemeMode = "dark" | "light"

export type Theme = {
  id: string
  name: string
  /** One line, shown under the name in the picker. */
  description: string
  mode: ThemeMode
}

export const DEFAULT_THEME = "midnight"

export const THEMES: Theme[] = [
  {
    id: "midnight",
    name: "Midnight",
    description: "The original. Deep blue-black with a single saturated blue.",
    mode: "dark",
  },
  {
    id: "nord",
    name: "Nord",
    description: "Cool and desaturated. Lifted background, softer contrast.",
    mode: "dark",
  },
  {
    id: "graphite",
    name: "Graphite",
    description: "Neutral grey with almost no hue. Colour comes from the data.",
    mode: "dark",
  },
  {
    id: "evergreen",
    name: "Evergreen",
    description: "Forest greens, for a dashboard that is mostly healthy.",
    mode: "dark",
  },
  {
    id: "ember",
    name: "Ember",
    description: "Warm amber on a brown-black canvas. Easy at 3am.",
    mode: "dark",
  },
  {
    id: "crimson",
    name: "Crimson",
    description: "High-alert red. Loud on purpose.",
    mode: "dark",
  },
  {
    id: "orchid",
    name: "Orchid",
    description: "Violet primary over a plum-tinted grey.",
    mode: "dark",
  },
  {
    id: "abyss",
    name: "Abyss",
    description: "Near-black for OLED panels, with a cyan accent.",
    mode: "dark",
  },
  {
    id: "terminal",
    name: "Terminal",
    description: "Phosphor green on black. A serial console that grew a chart.",
    mode: "dark",
  },
  {
    id: "daylight",
    name: "Daylight",
    description: "Clean white and blue, for a bright room.",
    mode: "light",
  },
  {
    id: "parchment",
    name: "Parchment",
    description: "Warm paper and ink. Light without the glare.",
    mode: "light",
  },
  {
    id: "porcelain",
    name: "Porcelain",
    description: "Neutral light grey. The quietest of the set.",
    mode: "light",
  },
]

const BY_ID = new Map(THEMES.map((t) => [t.id, t]))

/** The theme for an id, falling back to the default for anything unknown. */
export function resolveTheme(id: string | null | undefined): Theme {
  return BY_ID.get(id ?? "") ?? BY_ID.get(DEFAULT_THEME)!
}

export function isThemeId(id: string): boolean {
  return BY_ID.has(id)
}

/** Where the choice is kept. Shared with the pre-paint script in the layout. */
export const THEME_STORAGE_KEY = "just-dashboard.theme"

/**
 * The script the root layout inlines in <head>, before anything paints.
 *
 * Reading the stored theme after hydration would show one frame of the default
 * palette instead — which, for someone who chose a light theme, is a
 * full-screen flash of near-black on every navigation that reloads the
 * document. It is generated from the registry above so the two can never
 * disagree about which ids are light.
 */
export function themeBootstrapScript(): string {
  const light = THEMES.filter((t) => t.mode === "light").map((t) => t.id)
  return (
    `(function(){try{var d=document.documentElement,` +
    `t=localStorage.getItem(${JSON.stringify(THEME_STORAGE_KEY)})||${JSON.stringify(DEFAULT_THEME)},` +
    `l=${JSON.stringify(light)}.indexOf(t)>=0;` +
    `if(!${JSON.stringify(THEMES.map((t) => t.id))}.includes(t)){t=${JSON.stringify(DEFAULT_THEME)};l=false}` +
    `d.dataset.theme=t;d.classList.toggle("dark",!l);d.style.colorScheme=l?"light":"dark"}catch(e){}})()`
  )
}
