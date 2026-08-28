"use client"

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react"
import { useRouter } from "next/navigation"
import { Check, LogOut, Moon, Sun } from "lucide-react"
import { useAuth } from "@/hooks/use-auth"
import { useTheme } from "@/hooks/use-theme"
import { NAV, PERSONAL_NAV } from "@/components/app-sidebar"
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

type PaletteValue = { open: () => void; close: () => void; toggle: () => void }

const PaletteContext = createContext<PaletteValue | null>(null)

/**
 * One keystroke to any of fifteen pages, or to light/dark.
 *
 * A server dashboard is navigated by someone who already knows where they are
 * going — they are here because something is wrong at 3am, not to browse. The
 * palette is the shortest path, and it also gives the theme toggle a home that
 * does not require finding the Appearance page first.
 */
export function CommandPaletteProvider({ children }: { children: React.ReactNode }) {
  const [open, setOpen] = useState(false)

  const value = useMemo<PaletteValue>(
    () => ({
      open: () => setOpen(true),
      close: () => setOpen(false),
      toggle: () => setOpen((o) => !o),
    }),
    [],
  )

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() !== "k" || !(event.metaKey || event.ctrlKey)) return
      event.preventDefault()
      setOpen((o) => !o)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])

  return (
    <PaletteContext.Provider value={value}>
      {children}
      <Palette open={open} onOpenChange={setOpen} />
    </PaletteContext.Provider>
  )
}

export function useCommandPalette() {
  const ctx = useContext(PaletteContext)
  if (!ctx) throw new Error("useCommandPalette must be used inside CommandPaletteProvider")
  return ctx
}

function Palette({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const router = useRouter()
  const { can, logout } = useAuth()
  const { mode, setMode } = useTheme()

  const run = useCallback(
    (action: () => void) => {
      onOpenChange(false)
      action()
    },
    [onOpenChange],
  )

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogHeader className="sr-only">
        <DialogTitle>Command palette</DialogTitle>
        <DialogDescription>Jump to a page or change the palette</DialogDescription>
      </DialogHeader>
      <DialogContent className="overflow-hidden p-0 sm:max-w-xl" showCloseButton={false}>
        <Command className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:tracking-[0.14em] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group]]:px-2 [&_[cmdk-item]]:gap-2.5 [&_[cmdk-item]]:rounded-md [&_[cmdk-item]]:px-2 [&_[cmdk-item]]:py-2 [&_[cmdk-item]]:text-[13px]">
          <CommandInput placeholder="Jump to a page, or switch light/dark…" />
          <CommandList className="max-h-[60svh]">
            <CommandEmpty>Nothing matches.</CommandEmpty>

            {NAV.map((group) => {
              const items = group.items.filter((item) => !item.capability || can(item.capability))
              if (items.length === 0) return null
              return (
                <CommandGroup key={group.label} heading={group.label}>
                  {items.flatMap((item) => {
                    const rows = [
                      <CommandItem
                        key={item.href}
                        value={`${group.label} ${item.title}`}
                        onSelect={() => run(() => router.push(item.href))}
                      >
                        <item.icon className="size-4" />
                        {item.title}
                      </CommandItem>,
                    ]
                    // A nested feature's pages are reachable here even when the
                    // sidebar is collapsed to the icon rail and hides them.
                    for (const child of item.children ?? []) {
                      if (child.href === item.href) continue
                      rows.push(
                        <CommandItem
                          key={child.href}
                          value={`${group.label} ${item.title} ${child.title}`}
                          onSelect={() => run(() => router.push(child.href))}
                        >
                          <child.icon className="size-4" />
                          <span className="text-muted-foreground">{item.title}</span>
                          {child.title}
                        </CommandItem>,
                      )
                    }
                    return rows
                  })}
                </CommandGroup>
              )
            })}

            <CommandSeparator />
            <CommandGroup heading="You">
              {PERSONAL_NAV.map((item) => (
                <CommandItem
                  key={item.href}
                  value={`account ${item.title}`}
                  onSelect={() => run(() => router.push(item.href))}
                >
                  <item.icon className="size-4" />
                  {item.title}
                </CommandItem>
              ))}
              <CommandItem value="sign out logout" onSelect={() => run(() => void logout())}>
                <LogOut className="size-4" />
                Sign out
              </CommandItem>
            </CommandGroup>

            <CommandSeparator />
            <CommandGroup heading="Theme">
              <CommandItem value="theme dark" onSelect={() => run(() => setMode("dark"))}>
                <Moon className="size-4" />
                <span className="flex-1">Dark</span>
                {mode === "dark" && <Check className="size-3.5 text-muted-foreground" />}
              </CommandItem>
              <CommandItem value="theme light" onSelect={() => run(() => setMode("light"))}>
                <Sun className="size-4" />
                <span className="flex-1">Light</span>
                {mode === "light" && <Check className="size-3.5 text-muted-foreground" />}
              </CommandItem>
            </CommandGroup>
          </CommandList>
        </Command>
      </DialogContent>
    </Dialog>
  )
}
