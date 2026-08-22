"use client"

import { useCallback, useEffect, useRef, useState } from "react"
import type { IDisposable, Terminal } from "@xterm/xterm"
import type { SearchAddon } from "@xterm/addon-search"
import {
  ArrowDown,
  ArrowUp,
  CaseSensitive,
  ChevronsDown,
  Copy,
  Download,
  FolderOpen,
  Keyboard,
  Maximize2,
  Minimize2,
  Minus,
  Plus,
  Regex,
  RotateCw,
  Search,
  Settings2,
  Trash2,
  WholeWord,
  X,
  Zap,
} from "lucide-react"
import { toast } from "sonner"
import { wsUrl } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useTheme } from "@/hooks/use-theme"
import { actionFor, formatChord, useKeymap } from "@/lib/terminal-keymap"
import {
  FONT_MAX,
  FONT_MIN,
  TERMINAL_FONTS,
  setTerminalSettings,
  terminalSettings,
  useSnippets,
  useTerminalSettings,
} from "@/lib/terminal-settings"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Slider } from "@/components/ui/slider"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { ShortcutsDialog } from "@/components/terminal/shortcuts-dialog"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

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

/** The control keys a strip can send. Bytes, not tmux key names: these go
 *  straight down the same socket a keypress would, so the shell cannot tell
 *  the difference — which is the point, since Ctrl+C has to interrupt rather
 *  than be delivered as text. */
const CONTROL_KEYS = [
  { label: "Ctrl+C", hint: "Interrupt what is running", bytes: "\u0003" },
  { label: "Ctrl+D", hint: "End of input — logs a shell out", bytes: "\u0004" },
  { label: "Ctrl+Z", hint: "Suspend to the background", bytes: "\u001a" },
  { label: "Ctrl+L", hint: "Clear the screen, keeping scrollback", bytes: "\u000c" },
  { label: "Esc", hint: "Escape", bytes: "\u001b" },
  { label: "Ctrl+\\", hint: "Quit — stronger than Ctrl+C", bytes: "\u001c" },
] as const

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
  cwd,
  onOpenFiles,
  onCellClick,
}: {
  path: string
  query?: Query
  className?: string
  onExit?: () => void
  /** Shown in the pane header instead of the socket path — e.g. who you are. */
  subtitle?: React.ReactNode
  /** Where the shell currently is, for the actions that act on that directory. */
  cwd?: string
  onOpenFiles?: (path: string) => void
  /**
   * The cell a click landed on, so the caller can work out which tmux pane was
   * under it. The browser sees one terminal however many panes tmux has
   * composed into it, so the column and row are the only thing this component
   * can honestly report.
   */
  onCellClick?: (cell: { col: number; row: number }) => void
}) {
  const frameRef = useRef<HTMLDivElement>(null)
  const hostRef = useRef<HTMLDivElement>(null)
  const [state, setState] = useState<"connecting" | "open" | "closed">("connecting")
  const [error, setError] = useState<string>()
  const [fullscreen, setFullscreen] = useState(false)
  const [searching, setSearching] = useState(false)
  const [needle, setNeedle] = useState("")
  const [matches, setMatches] = useState<{ index: number; count: number }>({ index: -1, count: 0 })
  const [findOptions, setFindOptions] = useState({ caseSensitive: false, regex: false, word: false })
  const [atBottom, setAtBottom] = useState(true)
  const [shortcuts, setShortcuts] = useState(false)
  // What a guarded paste is holding: the bytes to send if it is confirmed,
  // and the readable version to show. They differ whenever the shell has
  // bracketed paste on, where the text arrives wrapped in escape sequences
  // that must be forwarded intact and must not be put on screen.
  const [pendingPaste, setPendingPaste] = useState<{ raw: string; text: string } | null>(null)
  const [bell, setBell] = useState(false)
  // The title the shell sets through OSC 0/2 — which for anybody with a
  // configured prompt is the command that is running. It is the one label the
  // pane can carry that says what this terminal is *doing* rather than what it
  // was called when it was opened.
  const [shellTitle, setShellTitle] = useState("")
  // Connection generation. Reconnecting is a deliberate act here — see the
  // socket effect — so it is a counter the effect depends on rather than
  // anything automatic.
  const [generation, setGeneration] = useState(0)
  const { mode } = useTheme()
  const settings = useTerminalSettings()
  const snippets = useSnippets()
  const map = useKeymap()

  // The live terminal, kept so that switching theme can re-colour it instead
  // of tearing down the PTY session behind it. The mode is mirrored into a ref
  // for the same reason: the connect effect must not depend on it.
  const termRef = useRef<Terminal | null>(null)
  const searchRef = useRef<SearchAddon | null>(null)
  const fitRef = useRef<{ fit: () => void } | null>(null)
  const socketRef = useRef<WebSocket | null>(null)
  const modeRef = useRef(mode)
  // Settings are read inside the connect effect, which must not re-run when
  // one changes: rebuilding the terminal would drop the scrollback and, on a
  // non-tmux host, the session with it. The effects below apply them to the
  // live terminal instead.
  const settingsRef = useRef(settings)
  useEffect(() => {
    settingsRef.current = settings
  }, [settings])
  // Same reason as the settings ref: rebinding a shortcut must not tear down
  // the terminal and the PTY behind it.
  const keymapRef = useRef(map)
  useEffect(() => {
    keymapRef.current = map
  }, [map])
  // The key handler is installed once, before toggleFullscreen is defined.
  const fullscreenRef = useRef<(() => Promise<void>) | null>(null)

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

      const s = settingsRef.current
      const term = new Terminal({
        fontFamily: s.fontFamily,
        fontSize: s.fontSize,
        lineHeight: s.lineHeight,
        letterSpacing: s.letterSpacing,
        cursorStyle: s.cursorStyle,
        cursorBlink: s.cursorBlink,
        // A cursor that goes hollow when the pane loses focus is the cheapest
        // possible answer to "am I about to type into the terminal or into the
        // page", which on a dashboard full of inputs is a real question.
        cursorInactiveStyle: "outline",
        convertEol: true,
        scrollback: s.scrollback,
        // A right-click should offer the page's paste, not xterm's selection
        // handling, which is what makes copy/paste behave like a normal app.
        rightClickSelectsWord: false,
        macOptionIsMeta: true,
        // The default separators break a path or a URL into pieces on
        // double-click, which is the one thing anybody double-clicks a
        // terminal for.
        wordSeparator: " ()[]{}',\"`",
        // xterm mutes a colour that would be unreadable against the
        // background. Left off, a script that prints dark grey on the dark
        // theme prints nothing at all.
        minimumContrastRatio: 1.6,
        // The search addon's match count and highlight-all live behind
        // xterm's "proposed API" flag — the decoration API they are built on
        // is not frozen yet. Without it `findNext` throws where it would have
        // decorated, `onDidChangeResults` never fires, and the counter reads
        // "none" over a scrollback full of matches, which is worse than not
        // showing a count at all.
        allowProposedApi: true,
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

      const disposables: IDisposable[] = []
      disposables.push(
        search.onDidChangeResults((r) =>
          setMatches({ index: r?.resultIndex ?? -1, count: r?.resultCount ?? 0 }),
        ),
      )
      disposables.push(term.onTitleChange((title) => setShellTitle(title)))
      disposables.push(
        term.onBell(() => {
          const now = settingsRef.current
          if (now.visualBell) {
            setBell(true)
            window.setTimeout(() => setBell(false), 250)
          }
          if (now.notifyOnBell && typeof Notification !== "undefined") {
            if (Notification.permission === "granted") {
              new Notification("Terminal", { body: "The shell rang the bell." })
            }
          }
        }),
      )
      // "Am I looking at the end of the output" — the difference between a
      // terminal that has gone quiet and one you have scrolled away from.
      disposables.push(
        term.onScroll(() => {
          const buffer = term.buffer.active
          setAtBottom(buffer.viewportY >= buffer.baseY)
        }),
      )
      disposables.push(
        term.onSelectionChange(() => {
          if (!settingsRef.current.copyOnSelect) return
          const selection = term.getSelection()
          if (selection) void navigator.clipboard?.writeText(selection).catch(() => {})
        }),
      )

      const socket = new WebSocket(wsUrl(path, { ...query, rows: term.rows, cols: term.cols }))
      socket.binaryType = "arraybuffer"
      socketRef.current = socket

      // True while the scrollback is being replayed into the terminal.
      //
      // A terminal answers some of what is written to it — `CSI c` and the
      // other device queries are the shell asking the terminal a question,
      // and xterm replies down the same channel a keystroke uses. Replaying a
      // buffer that contains one makes it answer again, at whatever prompt
      // exists now: reopening a tab typed `1;2c0;276` into the shell and left
      // a column of "command not found" behind it. The replies are dropped
      // for the length of the replay, which is the only window in which they
      // can only be about the past.
      let replaying = false

      const sendResize = () => {
        fit.fit()
        if (socket.readyState === WebSocket.OPEN) {
          socket.send(JSON.stringify({ type: "resize", rows: term.rows, cols: term.cols }))
        }
      }

      socket.onopen = () => {
        setState("open")
        setError(undefined)
        sendResize()
        term.focus()
      }
      socket.onmessage = (event) => {
        if (typeof event.data === "string") {
          // Only control frames arrive as text; an error is the one that
          // matters to the reader, and the scrollback marker says that the
          // next binary frame is the past rather than the present.
          try {
            const msg = JSON.parse(event.data)
            if (msg.type === "error") {
              setError(msg.error)
              term.writeln(`\r\n\x1b[31m${msg.error}\x1b[0m`)
            } else if (msg.type === "scrollback") {
              replaying = true
            }
          } catch {
            term.write(event.data)
          }
          return
        }
        // The callback fires once xterm has parsed everything written, which
        // is what makes "the replay is over" a fact rather than a guess at a
        // timeout.
        term.write(new Uint8Array(event.data as ArrayBuffer), () => {
          replaying = false
        })
      }
      socket.onclose = () => {
        setState("closed")
        term.writeln("\r\n\x1b[90m— disconnected —\x1b[0m")
        onExit?.()
      }
      socket.onerror = () => setState("closed")

      disposables.push(
        term.onData((data) => {
          if (socket.readyState !== WebSocket.OPEN) return
          if (replaying) return
          // The guard has to live here and not only on the paste shortcut.
          // Ctrl+V, the right-click menu and the X11 middle click all reach
          // xterm as a browser paste event, which becomes one onData call
          // carrying the whole block — so intercepting only the shortcut
          // guarded the one route nobody uses. A single Enter is one
          // character and is not a paste; anything longer that carries a line
          // ending is.
          if (settingsRef.current.confirmMultilinePaste && isMultilinePaste(data)) {
            setPendingPaste({ raw: data, text: readablePaste(data) })
            return
          }
          socket.send(data)
        }),
      )

      // The shortcuts the emulator owns, resolved through the keymap so every
      // one of them is the operator's to change. Ctrl+Shift is the default
      // family precisely because Ctrl+C and Ctrl+V belong to the shell —
      // intercepting those would break SIGINT, the most used key in any
      // terminal.
      //
      // Returning false tells xterm not to handle the key; it does *not* stop
      // the browser. Without preventDefault, Ctrl+Shift+V opened the paste
      // confirmation and let Chromium paste into xterm's textarea at the same
      // time — the guarded route and the unguarded one, at once.
      term.attachCustomKeyEventHandler((event) => {
        if (event.type !== "keydown") return true
        const action = actionFor(event, "terminal", keymapRef.current)
        if (!action) return true
        event.preventDefault()
        event.stopPropagation()
        switch (action) {
          case "terminal.copy":
            void copySelection(term)
            break
          case "terminal.paste":
            void requestPaste(socket, setPendingPaste)
            break
          case "terminal.search":
            setSearching(true)
            break
          case "terminal.clear":
            term.clear()
            break
          case "terminal.fullscreen":
            void fullscreenRef.current?.()
            break
          case "terminal.fontIn":
            setTerminalSettings({ fontSize: Math.min(FONT_MAX, settingsRef.current.fontSize + 1) })
            break
          case "terminal.fontOut":
            setTerminalSettings({ fontSize: Math.max(FONT_MIN, settingsRef.current.fontSize - 1) })
            break
          case "terminal.fontReset":
            setTerminalSettings({ fontSize: 13 })
            break
          case "terminal.shortcuts":
            setShortcuts(true)
            break
        }
        return false
      })

      // Ctrl+scroll is the zoom gesture every browser and every terminal
      // agrees on. Without `passive: false` the browser has already started
      // zooming the whole page by the time the handler runs.
      const onWheel = (event: WheelEvent) => {
        if (!event.ctrlKey) return
        event.preventDefault()
        const step = event.deltaY > 0 ? -1 : 1
        setTerminalSettings({
          fontSize: Math.min(FONT_MAX, Math.max(FONT_MIN, settingsRef.current.fontSize + step)),
        })
      }
      host.addEventListener("wheel", onWheel, { passive: false })

      const observer = new ResizeObserver(() => sendResize())
      observer.observe(host)

      cleanup = () => {
        observer.disconnect()
        host.removeEventListener("wheel", onWheel)
        for (const d of disposables) d.dispose()
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
  }, [path, generation, JSON.stringify(query ?? {})])

  useEffect(() => {
    modeRef.current = mode
    if (termRef.current) termRef.current.options.theme = { ...TERMINAL_THEMES[mode] }
  }, [mode])

  // Anything that changes the cell size changes the geometry, so the PTY has
  // to be told — otherwise the remote shell keeps wrapping for the old one.
  useEffect(() => {
    const term = termRef.current
    if (!term) return
    term.options.fontSize = settings.fontSize
    term.options.fontFamily = settings.fontFamily
    term.options.lineHeight = settings.lineHeight
    term.options.letterSpacing = settings.letterSpacing
    term.options.cursorStyle = settings.cursorStyle
    term.options.cursorBlink = settings.cursorBlink
    term.options.scrollback = settings.scrollback
    fitRef.current?.fit()
    const socket = socketRef.current
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(JSON.stringify({ type: "resize", rows: term.rows, cols: term.cols }))
    }
  }, [settings])

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

  useEffect(() => {
    fullscreenRef.current = toggleFullscreen
  }, [toggleFullscreen])

  const runSearch = useCallback(
    (direction: "next" | "previous") => {
      const search = searchRef.current
      if (!search || !needle) return
      const options = {
        caseSensitive: findOptions.caseSensitive,
        regex: findOptions.regex,
        wholeWord: findOptions.word,
        // Highlighting every match is what makes a count meaningful: "3 of 47"
        // with only one of them drawn is a number with nothing behind it.
        decorations: {
          matchBackground: "#a16207",
          matchOverviewRuler: "#a16207",
          activeMatchBackground: "#f59e0b",
          activeMatchColorOverviewRuler: "#f59e0b",
        },
      }
      if (direction === "next") search.findNext(needle, options)
      else search.findPrevious(needle, options)
    },
    [needle, findOptions],
  )

  // Re-running the search as the options change keeps the count honest; a
  // stale "12 matches" after switching on case sensitivity is worse than none.
  useEffect(() => {
    if (searching && needle) runSearch("next")
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [findOptions])

  const send = useCallback((data: string) => {
    const socket = socketRef.current
    if (socket?.readyState !== WebSocket.OPEN) {
      toast.error("Not connected")
      return
    }
    socket.send(data)
    termRef.current?.focus()
  }, [])

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
      <div className="flex flex-wrap items-center gap-1.5 border-b border-hairline bg-surface-header px-2.5 py-1.5">
        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-muted-foreground">
          {subtitle ?? path}
          {shellTitle && (
            <span className="ml-2 rounded bg-muted px-1 py-px text-[10px] text-foreground">
              {shellTitle}
            </span>
          )}
        </span>

        {searching ? (
          <div className="flex items-center gap-1">
            <Input
              autoFocus
              value={needle}
              onChange={(e) => setNeedle(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") runSearch(e.shiftKey ? "previous" : "next")
                if (e.key === "Escape") {
                  setSearching(false)
                  searchRef.current?.clearDecorations()
                  termRef.current?.focus()
                }
              }}
              placeholder="Find in scrollback"
              className="h-7 w-44 text-xs"
            />
            <span className="numeric w-14 shrink-0 text-center text-[10px] text-muted-foreground">
              {needle ? (matches.count ? `${matches.index + 1}/${matches.count}` : "none") : ""}
            </span>
            <FindToggle
              label="Match case"
              on={findOptions.caseSensitive}
              onClick={() => setFindOptions((o) => ({ ...o, caseSensitive: !o.caseSensitive }))}
            >
              <CaseSensitive className="size-3.5" />
            </FindToggle>
            <FindToggle
              label="Whole word"
              on={findOptions.word}
              onClick={() => setFindOptions((o) => ({ ...o, word: !o.word }))}
            >
              <WholeWord className="size-3.5" />
            </FindToggle>
            <FindToggle
              label="Regular expression"
              on={findOptions.regex}
              onClick={() => setFindOptions((o) => ({ ...o, regex: !o.regex }))}
            >
              <Regex className="size-3.5" />
            </FindToggle>
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
        ) : (
          <>
            <PaneButton
              label={`Search scrollback (${formatChord(map["terminal.search"])})`}
              onClick={() => setSearching(true)}
            >
              <Search className="size-3.5" />
            </PaneButton>

            <SnippetMenu snippets={snippets} onSend={(command) => send(command + "\r")} />

            <PaneButton
              label={`Copy selection (${formatChord(map["terminal.copy"])})`}
              onClick={() => termRef.current && copySelection(termRef.current)}
            >
              <Copy className="size-3.5" />
            </PaneButton>

            <div className="flex items-center rounded-md border border-hairline">
              <PaneButton
                label="Smaller text"
                onClick={() =>
                  setTerminalSettings({ fontSize: Math.max(FONT_MIN, settings.fontSize - 1) })
                }
              >
                <Minus className="size-3.5" />
              </PaneButton>
              <span className="numeric px-1 text-[10px] text-muted-foreground">
                {settings.fontSize}
              </span>
              <PaneButton
                label="Larger text"
                onClick={() =>
                  setTerminalSettings({ fontSize: Math.min(FONT_MAX, settings.fontSize + 1) })
                }
              >
                <Plus className="size-3.5" />
              </PaneButton>
            </div>

            <SettingsMenu />

            <PaneButton
              label={`Clear the screen (${formatChord(map["terminal.clear"])})`}
              onClick={() => {
                termRef.current?.clear()
                termRef.current?.focus()
              }}
            >
              <Trash2 className="size-3.5" />
            </PaneButton>

            <PaneButton
              label="Save the scrollback as a text file"
              onClick={() => termRef.current && downloadScrollback(termRef.current)}
            >
              <Download className="size-3.5" />
            </PaneButton>

            {cwd && onOpenFiles && (
              <PaneButton
                label={`Open ${cwd} in the file manager`}
                onClick={() => onOpenFiles(cwd)}
              >
                <FolderOpen className="size-3.5" />
              </PaneButton>
            )}

            <PaneButton
              label={`Keyboard shortcuts (${formatChord(map["terminal.shortcuts"])})`}
              onClick={() => setShortcuts(true)}
            >
              <Keyboard className="size-3.5" />
            </PaneButton>

            <PaneButton
              label={fullscreen ? "Leave fullscreen (Esc)" : "Fullscreen"}
              onClick={toggleFullscreen}
            >
              {fullscreen ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
            </PaneButton>
          </>
        )}

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

      {/*
        A dropped socket is not retried on its own. Reconnecting re-issues the
        GET, and for a session the dashboard is holding that is harmless — but
        the same component drives the compose runner, where re-issuing the GET
        runs the command again. So the retry is a button, and it says what it
        will do.
      */}
      {state === "closed" && (
        <div className="flex items-center gap-2 border-b border-hairline bg-muted/40 px-3 py-1.5 text-xs">
          <span className="flex-1 text-muted-foreground">
            The connection to this session ended. It is still running on the server.
          </span>
          <Button size="xs" variant="outline" onClick={() => setGeneration((g) => g + 1)}>
            <RotateCw className="size-3" />
            Reconnect
          </Button>
        </div>
      )}

      {error && (
        <p className="border-b border-hairline bg-destructive/10 px-3 py-1.5 text-xs text-destructive">
          {error}
        </p>
      )}

      <div className="relative min-h-0 flex-1">
        <div
          ref={hostRef}
          className={cn(
            "h-full p-2 transition-colors duration-150",
            bell && "bg-warning/25",
          )}
          style={bell ? undefined : { backgroundColor: TERMINAL_THEMES[mode].background }}
          // Clicking inside a pane focuses it, which is what clicking inside
          // anything does. tmux composes every pane into one screen before the
          // PTY ever sees it, so there is no element to hang a handler on —
          // only a cell. xterm publishes no pixel-to-cell mapping and does not
          // need to: the grid is uniform, so the screen's box divided by the
          // terminal's own rows and columns is exact.
          onMouseDown={(e) => {
            const term = termRef.current
            const screen = hostRef.current?.querySelector(".xterm-screen")
            if (!onCellClick || !term || !screen || e.button !== 0) return
            const box = screen.getBoundingClientRect()
            if (box.width === 0 || box.height === 0) return
            const col = Math.floor(((e.clientX - box.left) / box.width) * term.cols)
            const row = Math.floor(((e.clientY - box.top) / box.height) * term.rows)
            if (col < 0 || row < 0 || col >= term.cols || row >= term.rows) return
            onCellClick({ col, row })
          }}
          // Middle-click paste is the X11 convention every Linux operator has
          // in their fingers, and the browser does not provide it for us.
          onAuxClick={(e) => {
            if (e.button === 1 && socketRef.current) {
              e.preventDefault()
              void requestPaste(socketRef.current, setPendingPaste)
            }
          }}
        />
        {!atBottom && (
          <Button
            size="xs"
            variant="secondary"
            className="absolute right-4 bottom-4 shadow-md"
            onClick={() => {
              termRef.current?.scrollToBottom()
              termRef.current?.focus()
            }}
          >
            <ChevronsDown className="size-3" />
            Jump to the end
          </Button>
        )}
      </div>

      {/* The control keys, as buttons. Ctrl+C is unremarkable on a keyboard and
          impossible on a phone, and this panel is reached from a phone more
          often than its author would like. */}
      <div className="flex flex-wrap items-center gap-1 border-t border-hairline bg-surface-header px-2 py-1">
        {CONTROL_KEYS.map((key) => (
          <Button
            key={key.label}
            size="xs"
            variant="ghost"
            title={key.hint}
            className="h-5 px-1.5 font-mono text-[10px] text-muted-foreground hover:text-foreground"
            onClick={() => send(key.bytes)}
          >
            {key.label}
          </Button>
        ))}
      </div>

      <PasteConfirmation
        paste={pendingPaste}
        onCancel={() => setPendingPaste(null)}
        onConfirm={() => {
          if (pendingPaste) send(pendingPaste.raw)
          setPendingPaste(null)
        }}
      />

      <ShortcutsDialog open={shortcuts} onOpenChange={setShortcuts} />
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

function FindToggle({
  label,
  on,
  onClick,
  children,
}: {
  label: string
  on: boolean
  onClick: () => void
  children: React.ReactNode
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant="ghost"
      aria-label={label}
      aria-pressed={on}
      title={label}
      className={cn(
        "size-7 shrink-0 p-0",
        on ? "bg-primary/15 text-primary" : "text-muted-foreground hover:text-foreground",
      )}
      onClick={onClick}
    >
      {children}
    </Button>
  )
}

function SnippetMenu({
  snippets,
  onSend,
}: {
  snippets: { id: string; label: string; command: string }[]
  onSend: (command: string) => void
}) {
  if (snippets.length === 0) return null
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          aria-label="Send a saved command"
          title="Send a saved command"
          className="size-7 shrink-0 p-0 text-muted-foreground hover:text-foreground"
        >
          <Zap className="size-3.5" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-60">
        <DropdownMenuLabel className="text-xs">Send a command</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {snippets.map((snippet) => (
          <DropdownMenuItem
            key={snippet.id}
            className="flex-col items-start gap-0 text-xs"
            onSelect={() => onSend(snippet.command)}
          >
            <span>{snippet.label}</span>
            <span className="font-mono text-[10px] text-muted-foreground">{snippet.command}</span>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function SettingsMenu() {
  const settings = useTerminalSettings()
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          aria-label="Terminal settings"
          title="Terminal settings"
          className="size-7 shrink-0 p-0 text-muted-foreground hover:text-foreground"
        >
          <Settings2 className="size-3.5" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-72 space-y-3 text-xs">
        <p className="eyebrow">Appearance</p>
        <label className="flex items-center justify-between gap-2">
          Font
          <select
            value={settings.fontFamily}
            onChange={(e) => setTerminalSettings({ fontFamily: e.target.value })}
            className="h-7 min-w-0 flex-1 rounded-md border border-input bg-transparent px-1.5 text-xs"
          >
            {TERMINAL_FONTS.map((font) => (
              <option key={font.id} value={font.id}>
                {font.label}
              </option>
            ))}
          </select>
        </label>
        <label className="flex items-center justify-between gap-2">
          Cursor
          <select
            value={settings.cursorStyle}
            onChange={(e) =>
              setTerminalSettings({
                cursorStyle: e.target.value as "block" | "underline" | "bar",
              })
            }
            className="h-7 rounded-md border border-input bg-transparent px-1.5 text-xs"
          >
            <option value="block">Block</option>
            <option value="underline">Underline</option>
            <option value="bar">Bar</option>
          </select>
        </label>
        <div className="space-y-1">
          <div className="flex items-center justify-between">
            <span>Line height</span>
            <span className="numeric text-muted-foreground">{settings.lineHeight.toFixed(2)}</span>
          </div>
          <Slider
            min={1}
            max={2}
            step={0.05}
            value={[settings.lineHeight]}
            onValueChange={([v]) => setTerminalSettings({ lineHeight: v })}
          />
        </div>
        <SettingSwitch
          label="Blinking cursor"
          checked={settings.cursorBlink}
          onChange={(cursorBlink) => setTerminalSettings({ cursorBlink })}
        />

        <p className="eyebrow pt-1">Behaviour</p>
        <SettingSwitch
          label="Copy on select"
          hint="Selecting text copies it, as a native Linux terminal does."
          checked={settings.copyOnSelect}
          onChange={(copyOnSelect) => setTerminalSettings({ copyOnSelect })}
        />
        <SettingSwitch
          label="Confirm multi-line paste"
          hint="A pasted block runs every line but the last. This shows it first."
          checked={settings.confirmMultilinePaste}
          onChange={(confirmMultilinePaste) => setTerminalSettings({ confirmMultilinePaste })}
        />
        <SettingSwitch
          label="Flash on bell"
          checked={settings.visualBell}
          onChange={(visualBell) => setTerminalSettings({ visualBell })}
        />
        <SettingSwitch
          label="Notify on bell"
          hint="Needs the browser's permission, asked for once."
          checked={settings.notifyOnBell}
          onChange={(notifyOnBell) => {
            setTerminalSettings({ notifyOnBell })
            if (notifyOnBell && typeof Notification !== "undefined") {
              void Notification.requestPermission()
            }
          }}
        />
        <div className="space-y-1">
          <div className="flex items-center justify-between">
            <span>Scrollback</span>
            <span className="numeric text-muted-foreground">
              {settings.scrollback.toLocaleString()} lines
            </span>
          </div>
          <Slider
            min={1000}
            max={200000}
            step={1000}
            value={[settings.scrollback]}
            onValueChange={([v]) => setTerminalSettings({ scrollback: v })}
          />
        </div>
      </PopoverContent>
    </Popover>
  )
}

function SettingSwitch({
  label,
  hint,
  checked,
  onChange,
}: {
  label: string
  hint?: string
  checked: boolean
  onChange: (value: boolean) => void
}) {
  return (
    <div className="flex items-start justify-between gap-2">
      <span className="min-w-0">
        <span className="block">{label}</span>
        {hint && <span className="block text-[10px] text-muted-foreground">{hint}</span>}
      </span>
      <Switch checked={checked} onCheckedChange={onChange} className="mt-0.5 shrink-0" />
    </div>
  )
}

/**
 * The multi-line paste confirmation.
 *
 * It shows the text rather than merely warning about it, because the warning
 * on its own does not help: the operator already believes they know what is on
 * the clipboard, and the whole failure mode is that they are wrong.
 */
function PasteConfirmation({
  paste,
  onConfirm,
  onCancel,
}: {
  paste: { raw: string; text: string } | null
  onConfirm: () => void
  onCancel: () => void
}) {
  const lines = paste ? paste.text.replace(/\n$/, "").split("\n") : []
  return (
    <Dialog open={paste !== null} onOpenChange={(open) => !open && onCancel()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Paste {lines.length} lines?</DialogTitle>
          <DialogDescription>
            Every line but the last ends in a newline, so the shell will run it as soon as it
            arrives — this is not text going into the prompt for you to check first.
          </DialogDescription>
        </DialogHeader>
        <pre className="max-h-56 overflow-auto rounded-md border bg-surface-sunken p-2 font-mono text-[11px] whitespace-pre-wrap">
          {paste?.text}
        </pre>
        <DialogFooter>
          <Button size="sm" variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button size="sm" onClick={onConfirm}>
            Paste and run
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/**
 * Whether a chunk of input is a paste of more than one line.
 *
 * Length is what separates it from a keystroke: pressing Enter sends a single
 * carriage return, and a program that legitimately writes a line ending sends
 * it alongside something else. xterm normalises a pasted newline to \r, so
 * both endings count.
 */
function isMultilinePaste(data: string): boolean {
  if (data.length < 2) return false
  return /[\r\n]/.test(data.replace(/[\r\n]+$/, "")) || /[\r\n]/.test(stripBracketedPaste(data))
}

/** The escape sequences a shell in bracketed-paste mode wraps a paste in. */
function stripBracketedPaste(data: string): string {
  return data.replace(/\u001b\[200~/g, "").replace(/\u001b\[201~/g, "")
}

/** What to show the reader: the text, without the wrapper and without \r. */
function readablePaste(data: string): string {
  return stripBracketedPaste(data).replace(/\r\n?/g, "\n")
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

/**
 * Reads the clipboard and either sends it or hands it to the confirmation.
 *
 * Paste goes to the PTY as ordinary input rather than through xterm, so the
 * remote shell sees exactly the bytes it would from a keyboard.
 */
async function requestPaste(
  socket: WebSocket,
  ask: (paste: { raw: string; text: string }) => void,
) {
  if (socket.readyState !== WebSocket.OPEN) return
  try {
    const text = await navigator.clipboard.readText()
    if (!text) return
    if (terminalSettings().confirmMultilinePaste && isMultilinePaste(text)) {
      ask({ raw: text, text: readablePaste(text) })
      return
    }
    socket.send(text)
  } catch {
    toast.error("The browser refused clipboard access", {
      description: "Ctrl+V pastes into the shell directly if the page has no permission.",
    })
  }
}

/**
 * Writes the whole scrollback out as a text file.
 *
 * Walked line by line off the buffer rather than through a serialiser addon:
 * what is wanted here is the *text* of a build that failed, to be attached to
 * an issue or grepped, and escape sequences in that are noise. Trailing blank
 * lines go, because a 50,000-line buffer is mostly empty.
 */
function downloadScrollback(term: Terminal) {
  const buffer = term.buffer.active
  const lines: string[] = []
  for (let i = 0; i < buffer.length; i++) {
    lines.push(buffer.getLine(i)?.translateToString(true) ?? "")
  }
  while (lines.length && lines[lines.length - 1].trim() === "") lines.pop()
  if (lines.length === 0) {
    toast.info("There is nothing in the scrollback yet")
    return
  }
  const blob = new Blob([lines.join("\n") + "\n"], { type: "text/plain;charset=utf-8" })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement("a")
  anchor.href = url
  anchor.download = `terminal-${new Date().toISOString().replace(/[:.]/g, "-")}.txt`
  anchor.click()
  URL.revokeObjectURL(url)
  toast.success(`Saved ${lines.length.toLocaleString()} lines`)
}
