"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import type { Terminal } from "@xterm/xterm"
import type { SearchAddon } from "@xterm/addon-search"
import {
  ArrowDown,
  ArrowUp,
  Copy,
  Maximize2,
  Minimize2,
  Minus,
  Plus,
  Search,
  Trash2,
  X,
} from "lucide-react"
import { toast } from "sonner"
import { wsUrl } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useTheme } from "@/hooks/use-theme"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

type Query = Record<string, string | number | boolean | undefined | null>

/**
 * xterm cannot read the theme tokens: it parses colours itself and does not
 * understand `oklch()` or `var()`. So the terminal gets two hand-picked
 * palettes and follows the active theme's mode rather than its hue — which is
 * the part that matters, since a black pane in the middle of a light page is
 * the one place a palette can be actively unreadable.
 */
const TERMINAL_THEMES = {
  dark: {
    background: "#0e1117",
    foreground: "#e3e6ee",
    cursor: "#e4e4e7",
    selectionBackground: "#3f3f46",
    black: "#18181b",
    red: "#f87171",
    green: "#4ade80",
    yellow: "#fbbf24",
    blue: "#60a5fa",
    magenta: "#c084fc",
    cyan: "#22d3ee",
    white: "#e4e4e7",
  },
  light: {
    background: "#fbfbfd",
    foreground: "#26262b",
    cursor: "#3f3f46",
    selectionBackground: "#c7d2fe",
    black: "#26262b",
    red: "#b91c1c",
    green: "#15803d",
    yellow: "#a16207",
    blue: "#1d4ed8",
    magenta: "#7e22ce",
    cyan: "#0e7490",
    white: "#52525b",
  },
} as const

const FONT_MIN = 9
const FONT_MAX = 22
const FONT_STORAGE_KEY = "jd.terminal.fontSize"

/**
 * An xterm.js terminal wired to a PTY over a WebSocket.
 *
 * Binary frames carry raw terminal bytes in both directions; JSON frames carry
 * control messages, of which resize is the one that matters — without it the
 * remote shell keeps drawing for an 80x24 window no matter how large the pane.
 */
export function XtermPane({
  path,
  query,
  className,
  onExit,
  subtitle,
}: {
  path: string
  query?: Query
  className?: string
  onExit?: () => void
  /** Shown in the pane header instead of the socket path — e.g. who you are. */
  subtitle?: React.ReactNode
}) {
  const frameRef = useRef<HTMLDivElement>(null)
  const hostRef = useRef<HTMLDivElement>(null)
  const [state, setState] = useState<"connecting" | "open" | "closed">("connecting")
  const [error, setError] = useState<string>()
  const [fullscreen, setFullscreen] = useState(false)
  const [searching, setSearching] = useState(false)
  const [needle, setNeedle] = useState("")
  const { mode } = useTheme()
  // The live terminal, kept so that switching theme can re-colour it instead
  // of tearing down the PTY session behind it. The mode is mirrored into a ref
  // for the same reason: the connect effect must not depend on it.
  const termRef = useRef<Terminal | null>(null)
  const searchRef = useRef<SearchAddon | null>(null)
  const fitRef = useRef<{ fit: () => void } | null>(null)
  const socketRef = useRef<WebSocket | null>(null)
  const modeRef = useRef(mode)
  // Read once, not on every render: a font size that resets when you switch
  // tabs is a setting you have to make again every time.
  const [fontSize, setFontSize] = useState(() => {
    if (typeof window === "undefined") return 13
    const stored = Number(window.localStorage.getItem(FONT_STORAGE_KEY))
    return stored >= FONT_MIN && stored <= FONT_MAX ? stored : 13
  })

  useEffect(() => {
    const host = hostRef.current
    if (!host) return

    let disposed = false
    let cleanup = () => {}

    // xterm touches `window` at import time, so it is loaded in the effect
    // rather than at module scope where the server render would break.
    ;(async () => {
      const [{ Terminal }, { FitAddon }, { WebLinksAddon }, { SearchAddon }] = await Promise.all([
        import("@xterm/xterm"),
        import("@xterm/addon-fit"),
        import("@xterm/addon-web-links"),
        import("@xterm/addon-search"),
      ])
      await import("@xterm/xterm/css/xterm.css")
      if (disposed) return

      const term = new Terminal({
        fontFamily: 'ui-monospace, "SF Mono", Menlo, Consolas, monospace',
        fontSize,
        cursorBlink: true,
        convertEol: true,
        // Deep enough to hold the output of a real build or a long tail. The
        // buffer is the reason "scroll up and read what scrolled past" works
        // at all, and a shallow one is indistinguishable from a broken pager.
        scrollback: 50000,
        // A right-click should offer the page's paste, not xterm's selection
        // handling, which is what makes copy/paste behave like a normal app.
        rightClickSelectsWord: false,
        macOptionIsMeta: true,
        theme: { ...TERMINAL_THEMES[modeRef.current] },
      })
      termRef.current = term
      const fit = new FitAddon()
      const search = new SearchAddon()
      fitRef.current = fit
      searchRef.current = search
      term.loadAddon(fit)
      term.loadAddon(search)
      term.loadAddon(new WebLinksAddon())
      term.open(host)
      fit.fit()

      const socket = new WebSocket(wsUrl(path, { ...query, rows: term.rows, cols: term.cols }))
      socket.binaryType = "arraybuffer"
      socketRef.current = socket

      const sendResize = () => {
        fit.fit()
        if (socket.readyState === WebSocket.OPEN) {
          socket.send(JSON.stringify({ type: "resize", rows: term.rows, cols: term.cols }))
        }
      }

      socket.onopen = () => {
        setState("open")
        sendResize()
        term.focus()
      }
      socket.onmessage = (event) => {
        if (typeof event.data === "string") {
          // Only control frames arrive as text; an error is the one that
          // matters to the reader.
          try {
            const msg = JSON.parse(event.data)
            if (msg.type === "error") {
              setError(msg.error)
              term.writeln(`\r\n\x1b[31m${msg.error}\x1b[0m`)
            }
          } catch {
            term.write(event.data)
          }
          return
        }
        term.write(new Uint8Array(event.data as ArrayBuffer))
      }
      socket.onclose = () => {
        setState("closed")
        term.writeln("\r\n\x1b[90m— session closed —\x1b[0m")
        onExit?.()
      }
      socket.onerror = () => setState("closed")

      const inputDisposable = term.onData((data) => {
        if (socket.readyState === WebSocket.OPEN) socket.send(data)
      })

      // The shortcuts a terminal emulator is expected to own. Ctrl+Shift is
      // the standard escape hatch precisely because Ctrl+C and Ctrl+V belong
      // to the shell — intercepting those instead would break SIGINT, which is
      // the single most used key in any terminal.
      term.attachCustomKeyEventHandler((event) => {
        if (event.type !== "keydown") return true
        const combo = event.ctrlKey && event.shiftKey
        if (combo && event.code === "KeyC") {
          void copySelection(term)
          return false
        }
        if (combo && event.code === "KeyV") {
          void pasteInto(socket)
          return false
        }
        if (combo && event.code === "KeyF") {
          setSearching(true)
          return false
        }
        return true
      })

      const observer = new ResizeObserver(() => sendResize())
      observer.observe(host)

      cleanup = () => {
        observer.disconnect()
        inputDisposable.dispose()
        socket.close()
        term.dispose()
        termRef.current = null
        searchRef.current = null
        fitRef.current = null
        socketRef.current = null
      }
    })()

    return () => {
      disposed = true
      cleanup()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, JSON.stringify(query ?? {})])

  useEffect(() => {
    modeRef.current = mode
    if (termRef.current) termRef.current.options.theme = { ...TERMINAL_THEMES[mode] }
  }, [mode])

  // Font size changes the cell size, so the PTY has to be told the new
  // geometry — otherwise the remote shell keeps wrapping for the old one.
  useEffect(() => {
    const term = termRef.current
    if (!term) return
    term.options.fontSize = fontSize
    fitRef.current?.fit()
    const socket = socketRef.current
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "resize", rows: term.rows, cols: term.cols }))
    }
    window.localStorage.setItem(FONT_STORAGE_KEY, String(fontSize))
  }, [fontSize])

  // Fullscreen is the browser's, not a CSS class: only the real thing escapes
  // the window chrome, and only it survives the operator pressing Escape,
  // which they will expect to work.
  useEffect(() => {
    const onChange = () => setFullscreen(document.fullscreenElement === frameRef.current)
    document.addEventListener("fullscreenchange", onChange)
    return () => document.removeEventListener("fullscreenchange", onChange)
  }, [])

  const toggleFullscreen = useCallback(async () => {
    const frame = frameRef.current
    if (!frame) return
    try {
      if (document.fullscreenElement === frame) await document.exitFullscreen()
      else await frame.requestFullscreen()
    } catch {
      toast.error("Fullscreen was refused by the browser")
    }
    // The pane's box changes after the transition, and the ResizeObserver
    // fires before the browser has finished laying it out.
    setTimeout(() => {
      fitRef.current?.fit()
      termRef.current?.focus()
    }, 120)
  }, [])

  const runSearch = useCallback(
    (direction: "next" | "previous") => {
      const search = searchRef.current
      if (!search || !needle) return
      if (direction === "next") search.findNext(needle, { caseSensitive: false })
      else search.findPrevious(needle, { caseSensitive: false })
    },
    [needle],
  )

  return (
    <div
      ref={frameRef}
      className={cn(
        "relative flex min-w-0 flex-col overflow-hidden rounded-xl border bg-surface-sunken",
        // In fullscreen the pane is the whole screen, so the rounded corners
        // and border would draw a frame around nothing.
        fullscreen && "rounded-none border-0",
        className,
      )}
    >
      <div className="flex flex-wrap items-center gap-2 border-b border-hairline bg-surface-header px-2.5 py-1.5">
        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-muted-foreground">
          {subtitle ?? path}
        </span>

        {searching && (
          <div className="flex items-center gap-1">
            <Input
              autoFocus
              value={needle}
              onChange={(e) => setNeedle(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") runSearch(e.shiftKey ? "previous" : "next")
                if (e.key === "Escape") {
                  setSearching(false)
                  termRef.current?.focus()
                }
              }}
              placeholder="Find in scrollback"
              className="h-7 w-44 text-xs"
            />
            <PaneButton label="Previous match" onClick={() => runSearch("previous")}>
              <ArrowUp className="size-3.5" />
            </PaneButton>
            <PaneButton label="Next match" onClick={() => runSearch("next")}>
              <ArrowDown className="size-3.5" />
            </PaneButton>
            <PaneButton
              label="Close search"
              onClick={() => {
                setSearching(false)
                searchRef.current?.clearDecorations()
                termRef.current?.focus()
              }}
            >
              <X className="size-3.5" />
            </PaneButton>
          </div>
        )}

        {!searching && (
          <PaneButton label="Search scrollback (Ctrl+Shift+F)" onClick={() => setSearching(true)}>
            <Search className="size-3.5" />
          </PaneButton>
        )}
        <PaneButton
          label="Copy selection (Ctrl+Shift+C)"
          onClick={() => termRef.current && copySelection(termRef.current)}
        >
          <Copy className="size-3.5" />
        </PaneButton>
        <div className="flex items-center rounded-md border border-hairline">
          <PaneButton
            label="Smaller text"
            onClick={() => setFontSize((f) => Math.max(FONT_MIN, f - 1))}
          >
            <Minus className="size-3.5" />
          </PaneButton>
          <span className="numeric px-1 text-[10px] text-muted-foreground">{fontSize}</span>
          <PaneButton
            label="Larger text"
            onClick={() => setFontSize((f) => Math.min(FONT_MAX, f + 1))}
          >
            <Plus className="size-3.5" />
          </PaneButton>
        </div>
        <PaneButton
          label="Clear the screen"
          onClick={() => {
            termRef.current?.clear()
            termRef.current?.focus()
          }}
        >
          <Trash2 className="size-3.5" />
        </PaneButton>
        <PaneButton
          label={fullscreen ? "Leave fullscreen (Esc)" : "Fullscreen"}
          onClick={toggleFullscreen}
        >
          {fullscreen ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
        </PaneButton>
        <Badge
          variant={state === "open" ? "success" : "secondary"}
          className="shrink-0 gap-1.5 text-[10px] font-normal"
        >
          <span
            className={cn(
              "size-1.5 rounded-full",
              state === "open" ? "bg-success" : "bg-muted-foreground",
            )}
          />
          {state}
        </Badge>
      </div>
      {error && (
        <p className="border-b border-hairline bg-destructive/10 px-3 py-1.5 text-xs text-destructive">
          {error}
        </p>
      )}
      <div
        ref={hostRef}
        className="min-h-0 flex-1 p-2"
        style={{ backgroundColor: TERMINAL_THEMES[mode].background }}
        // Middle-click paste is the X11 convention every Linux operator has in
        // their fingers, and the browser does not provide it for us.
        onAuxClick={(e) => {
          if (e.button === 1 && socketRef.current) {
            e.preventDefault()
            void pasteInto(socketRef.current)
          }
        }}
      />
    </div>
  )
}

/** A square icon button sized for the pane's header strip. */
function PaneButton({
  label,
  onClick,
  children,
}: {
  label: string
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant="ghost"
      aria-label={label}
      title={label}
      className="size-7 shrink-0 p-0 text-muted-foreground hover:text-foreground"
      onClick={onClick}
    >
      {children}
    </Button>
  )
}

async function copySelection(term: Terminal) {
  const selection = term.getSelection()
  if (!selection) {
    toast.info("Nothing is selected")
    return
  }
  try {
    await navigator.clipboard.writeText(selection)
    toast.success("Copied")
  } catch {
    toast.error("The browser refused clipboard access")
  }
}

// Paste goes to the PTY as ordinary input rather than through xterm, so the
// remote shell sees exactly the bytes it would from a keyboard.
async function pasteInto(socket: WebSocket) {
  if (socket.readyState !== WebSocket.OPEN) return
  try {
    const text = await navigator.clipboard.readText()
    if (text) socket.send(text)
  } catch {
    toast.error("The browser refused clipboard access", {
      description: "Ctrl+V pastes into the shell directly if the page has no permission.",
    })
  }
}
