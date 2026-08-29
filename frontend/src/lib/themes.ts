/**
 * Light and dark. Nothing else.
 *
 * This used to be a registry of twelve named palettes; the product now ships
 * one palette ("Just Dashboard", defined in `app/globals.css`'s `:root` and
 * `.dark` blocks) in two modes. What's left here is the part that doesn't
 * belong in a component: where the choice is stored, and the script that
 * applies it before first paint.
 */

export type ThemeMode = "light" | "dark"

export const DEFAULT_MODE: ThemeMode = "dark"

/** Where the choice is kept. Shared with the pre-paint script in the layout. */
export const THEME_STORAGE_KEY = "just-dashboard.theme"

export function isThemeMode(v: string | null | undefined): v is ThemeMode {
  return v === "light" || v === "dark"
}

/**
 * The script the root layout inlines in <head>, before anything paints.
 *
 * Reading the stored mode after hydration would show one frame of the
 * default instead — which, for someone who chose light, is a full-screen
 * flash of near-black on every navigation that reloads the document.
 */
export function themeBootstrapScript(): string {
  return (
    `(function(){try{var d=document.documentElement,` +
    `m=localStorage.getItem(${JSON.stringify(THEME_STORAGE_KEY)}),` +
    `l=m==="light"||(m!=="dark"&&${JSON.stringify(DEFAULT_MODE)}==="light");` +
    `d.className=l?"light":"dark";d.style.colorScheme=l?"light":"dark"}catch(e){}})()`
  )
}
