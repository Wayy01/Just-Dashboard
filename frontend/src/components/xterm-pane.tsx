"use client"

import { useEffect, useRef, useState } from "react"
import type { Terminal } from "@xterm/xterm"
import { wsUrl } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useTheme } from "@/hooks/use-theme"
import { Badge } from "@/components/ui/badge"

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
}: {
  path: string
  query?: Query
  className?: string
  onExit?: () => void
}) {
  const hostRef = useRef<HTMLDivElement>(null)
  const [state, setState] = useState<"connecting" | "open" | "closed">("connecting")
  const [error, setError] = useState<string>()
  const { mode } = useTheme()
  // The live terminal, kept so that switching theme can re-colour it instead
  // of tearing down the PTY session behind it. The mode is mirrored into a ref
  // for the same reason: the connect effect must not depend on it.
  const termRef = useRef<Terminal | null>(null)
  const modeRef = useRef(mode)

  useEffect(() => {
    const host = hostRef.current
    if (!host) return

    let disposed = false
    let cleanup = () => {}

    // xterm touches `window` at import time, so it is loaded in the effect
    // rather than at module scope where the server render would break.
    ;(async () => {
      const [{ Terminal }, { FitAddon }, { WebLinksAddon }] = await Promise.all([
        import("@xterm/xterm"),
        import("@xterm/addon-fit"),
        import("@xterm/addon-web-links"),
      ])
      await import("@xterm/xterm/css/xterm.css")
      if (disposed) return

      const term = new Terminal({
        fontFamily: 'ui-monospace, "SF Mono", Menlo, Consolas, monospace',
        fontSize: 13,
        cursorBlink: true,
        convertEol: true,
        scrollback: 10000,
        theme: { ...TERMINAL_THEMES[modeRef.current] },
      })
      termRef.current = term
      const fit = new FitAddon()
      term.loadAddon(fit)
      term.loadAddon(new WebLinksAddon())
      term.open(host)
      fit.fit()

      const socket = new WebSocket(wsUrl(path, { ...query, rows: term.rows, cols: term.cols }))
      socket.binaryType = "arraybuffer"

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

      const observer = new ResizeObserver(() => sendResize())
      observer.observe(host)

      cleanup = () => {
        observer.disconnect()
        inputDisposable.dispose()
        socket.close()
        term.dispose()
        termRef.current = null
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

  return (
    <div
      className={cn("relative flex min-w-0 flex-col overflow-hidden rounded-xl border", className)}
    >
      <div className="flex items-center justify-between gap-2 border-b border-hairline bg-surface-header px-3 py-2">
        <span className="truncate font-mono text-[11px] text-muted-foreground">{path}</span>
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
      />
    </div>
  )
}
