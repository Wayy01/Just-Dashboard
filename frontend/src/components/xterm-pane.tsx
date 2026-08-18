"use client"

import { useEffect, useRef, useState } from "react"
import { wsUrl } from "@/lib/api"
import { cn } from "@/lib/utils"
import { Badge } from "@/components/ui/badge"

type Query = Record<string, string | number | boolean | undefined | null>

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
        theme: {
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
      })
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
      }
    })()

    return () => {
      disposed = true
      cleanup()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, JSON.stringify(query ?? {})])

  return (
    <div
      className={cn(
        "relative flex flex-col overflow-hidden rounded-lg border bg-[#0e1117]",
        className,
      )}
    >
      <div className="flex items-center justify-between border-b bg-muted/40 px-3 py-1.5">
        <span className="font-mono text-xs text-muted-foreground">{path}</span>
        <Badge variant={state === "open" ? "success" : "secondary"} className="gap-1.5 text-[10px]">
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
        <p className="border-b bg-destructive/10 px-3 py-1.5 text-xs text-destructive">{error}</p>
      )}
      <div ref={hostRef} className="min-h-0 flex-1 p-2" />
    </div>
  )
}
