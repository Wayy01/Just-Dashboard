# Docker, files, and logs

## Docker

`internal/dockerx` uses the official SDK over the socket. It shells out in exactly three places —
compose, the streaming compose runner, and `Build` — because the Engine API has no equivalent; all three
build argv explicitly.

- **`ContainerSpec` is the dashboard's shape, not `container.Config` + `HostConfig`.** Those are split on
  the historical accident of which fields the daemon could change after creation, and rendering them as a
  form is how Portainer's create page became twelve accordions. `toEngine` translates and warns about
  what is legal but probably unintended (a port on every interface, no memory limit, an anonymous volume,
  a writable bind mount of a sensitive path). `SpecOf` reads a container back into that shape, which is
  what makes duplicate, edit-and-recreate and "save as a stack" possible.
- **`Recreate` is the verb Docker lacks.** Editing means destroy-and-recreate, which is fine until the
  create fails and the operator has nothing where their service was — so the old container is renamed
  aside (`<name>_jd_replaced`), restored if anything later fails, and removed only once the replacement
  runs. Compose-managed containers are refused with `ErrComposeManaged`. `UpdateResources` is separate
  because limits genuinely can change in place.
- **`render.go` keeps the form from being a black box**: a spec back into the `docker run` line and the
  compose service, rendered **on the server** so "what does this spec mean" has one implementation. The
  YAML is hand-written, not marshalled — key order carries meaning and a marshaller would sort it into
  something correct and unreadable.
- **`diagnose.go` is what nothing else in this class has.** Every panel shows a state, an exit code and a
  restart count and leaves you to read them; this says what 137 means and what the limit was, that a
  container restarted twelve times in a minute, that a health check is failing and what it last said,
  that a port is published in front of the firewall, that an unrotated json-file log has reached 800 MB,
  that data is being written into the container rather than a volume. Findings carry a `Level`, the
  reasoning and an `Action` where the remedy is ours to run. Deliberately conservative — a panel that
  cries wolf is ignored wholesale — and `diagnose_test.go` pins the claims, including the two **silences**
  (a finished one-shot job, a loopback-bound port).
- **`events.go` keeps what Docker throws away**, so "why did this restart at 04:00" has an answer. An
  in-memory ring: an event log worth keeping across restarts belongs in the audit table. `oom` and
  `health_status: unhealthy` justify the feature alone.
- **`CheckUpdate` compares the registry's current digest against the pulled one** — a more useful question
  than "is there a newer tag", because it catches a moving tag that moved. Cached 30 min, four-worker
  pool, because Docker Hub rate-limits by address. Unreachable or credentialed registries are `unknown`
  with the reason; a locally built image is `local`, not a failure.
- **Compose.** `RunComposeStream` forwards output line by line, because a request that hangs for minutes
  is indistinguishable from a broken dashboard. `composeSteps` maps actions to commands; `update` is a
  pull **then** an up, so a registry that is down leaves the running stack alone. `ValidateCompose` feeds
  the candidate to the parser on **stdin** so a syntax error never touches disk; `WriteComposeFile` goes
  through a temp file in the same directory and keeps `<name>.bak`, guarding what validation cannot catch
  — a correct file that says the wrong thing. `DeclaredServices` costs a subprocess per stack and is read
  on demand; it supplies the one fact the container list cannot, a declared service with no container.
- **`Build` drives the `docker` binary** because BuildKit is a separate builder the classic API path never
  reaches, and silently building with the legacy one produces images differing from the same Dockerfile
  from a shell.
- **Efficiency rules that are load-bearing**: `ListContainers` carries `Mounts` (the Engine summary
  already has them, and "what uses this volume" for every volume at once is otherwise an inspect per
  container per poll); `ListStacks` builds on `ListContainers` so it inherits resolved health and uptime;
  `Diagnose` inspects each container once and runs every rule against that payload.
  `ListContainersWithLabels` applies exact label filters in the Engine list call before health/uptime
  enrichment, so a deployment detail read inspects only its matching running containers.

## Files

`internal/files` used to be a listing, a reader and a writer, with a page that answered every click by
loading the file into Monaco — right for a config file, wrong for a picture, a tarball, a video and a
two-gigabyte log.

- **`preview.go`** reports what a thing *is* without loading it: a trimmed head for text (whole lines, no
  rune cut in half), image dimensions, the first 200 entries of a zip or tar without unpacking, child
  counts for a directory. `Editable` is separate from `Kind` on purpose — a preview of a huge log must not
  offer a Save that writes those hundred lines over it. Extensions decide media kinds, **bytes** decide
  the rest (a `.log` is sometimes a rotated binary; a file with no extension is usually a script).
  `imageSize` parses webp and svg by hand: half of what a web root holds, and neither has a stdlib decoder.
- **`MediaType` is a security boundary.** `GET /files/raw` returns a file's bytes with a content type the
  browser acts on, on the same origin as a session that drives the Docker socket — so it is a closed
  allowlist (images, video, audio, PDF) and everything else is refused rather than sniffed. **HTML is not
  on it and must not be**: served inline it runs as this dashboard. The route tightens CSP to `sandbox`
  (which neuters a directly opened SVG) and is the one route with a short `Cache-Control` instead of
  `no-store` — forty thumbnails otherwise re-read every JPEG on every scroll; callers append mtime so a
  saved image is a new URL.
- **`find.go`** is the fuzzy finder (`search.go` is the literal/regex one, optionally grepping contents).
  Subsequence matching scored so the basename beats directories, a run beats scattered characters, a
  boundary beats mid-word and a shallow path beats a deep one; terms ANDed; positions as **UTF-16
  offsets**. Bounded three ways (time, visits, matches) and it *says* when it stopped early — a fuzzy
  search that quietly answers from a third of the disk is worse than one that admits it.
- **`places.go`**: `Home` prefers `$HOME`, then `/root`, then a single account under `/home`, then the
  first configured root — every candidate checked through `Resolve`, because a shortcut landing outside
  the roots is worse than no shortcut. `Complete` treats a trailing separator as "inside this directory"
  and anything else as a component being typed; dotfiles appear only once a dot is typed.
- **`Usage`** accumulates per-child totals in the *same* bounded walk (forty children would otherwise be
  forty-one walks) and reports `Truncated` rather than quoting a partial total. A symlink counts as the
  link, or a tree with links into /usr reports the size of the operating system.
- **`GET /system/disk-usage` is a file operation.** Its requested mount passes through `files.Resolve`,
  recursive visits and top-level children are capped, and only two scans run concurrently. A result limit
  controls the response size; it is not mistaken for a bound on the work needed to rank those results.

Bookmarks live in the `settings` table (`handlers_files_browse.go`), not the browser — which directory
matters is a fact about the server and should be there from a phone. Recent folders are the opposite and
stay in `useViewState`. The bookmark list is saved whole, so an add, a removal and a reorder cannot
disagree about order; every path is resolved before storing.

Frontend `components/files/`: `file-icon.tsx` is the vocabulary (~200 extensions, the files with none —
Dockerfile, authorized_keys, lockfiles — and the folders whose name says more than "folder") mapped to
eight **categories** rather than languages, in the shared semantic `--tag-*` hues, drawn from Material
Design Icons (`@mdi/js`); every other glyph in the product comes from the Heroicons vocabulary in
`components/icons.tsx`. `file-actions.tsx` is
the one menu both the row and the tile use, or an action ends up in one view only. Two layout rules are
easy to undo: **the panel body does not scroll** (a sticky table header sticks to its nearest scrolling
ancestor, and the header rode away with the rows), and **the rail's tree waits for `/files/places`**
before mounting, since it caches and would keep showing the refusal from listing "/". The image editor
commits each operation to a **new canvas** rather than a live parameter pipeline — that is what makes
undo a stack of bitmaps and why "rotate, crop, rotate again" behaves the way it looks; saving goes
through the ordinary upload route so owner and mode survive.

## Logs

`logsx` + `handlers_logs.go` were three products wearing one page: the grep box and level chips applied
to *file* tails only, `/logs/search` and `/logs/logrotate` had no caller, export ignored the filter, and
rotated archives were unreachable — so "when did this start" could not be asked past last night's
logrotate run, which is the question that sent people back to ssh and zgrep.

- **One filter, compiled once, applied to every kind.** `logsx.Filter` is the single description of what
  the operator wants; `handleLogStream` turns a file, a container, a PM2 process and the journal into the
  same `logsx.Line` before filtering. A bad regex is refused **before** the socket upgrades, so it is a
  form error rather than a stream that opens and stays empty. `logsx.Collector` does the same for history.
- **A filtered tail opens on n *matches*, not n lines.** `TailLines` scans backwards through the last
  32 MB collecting matches, then follows from the byte the scan stopped at — a byte earlier duplicates a
  line, a byte later loses one. `Prefill` distinguishes "few matches" from "we only looked so far back".
- **Archives are part of the log.** `Archives` orders rotated generations by **mtime**: the two schemes
  count in opposite directions (`syslog.1` is newer than `syslog.2`; `syslog-20240612` is older than
  `-20240613`), so ordering by name reports an incident running backwards. gzip and bzip2 read
  transparently; xz and zstd are skipped rather than offered and then refused.
- **`unknown` is a level.** Most lines carry no level word, so a filter without that chip hid every
  unclassified line — including the continuation lines of the stack trace being hunted.
- **The journal's numbers and a text log's words are one vocabulary** (`LevelFromPriority`).
  `maxJournalPriority` pushes only the *maximum* down to `journalctl -p 0..n`, since the chips are a set
  and `-p` takes a range; the exact test is still done here. A text filter is deliberately **not** pushed
  down — `journalctl -g` needs a PCRE2 build nobody can assume — so the window is widened instead.
- **Nothing is offered that cannot be opened**: `Discover` runs `Allow` over the well-known paths, or an
  install that narrowed `JD_LOG_ROOTS` gets a rail of files that refuse to open.
- **Retention is a verdict, not a rule list.** `MatchRetention` finds the file no rule governs — precisely
  the entry a rule list cannot show. Two parser details, both found against a real host: a stanza's paths
  may be listed **one per line before the brace** (exactly how Debian ships rsyslog's, so reading only the
  brace line reported syslog, auth.log and kern.log as governed by nothing), and the globals at the top of
  `logrotate.conf` sit at the same indentation and must not be mistaken for paths. A rule that has not run
  in a fortnight is a warning — that is the failure this panel is for.
- **Parsing performance is the difference between usable and not** (a five-file auth.log scan: 19 s → 2 s):
  a word scan with a map lookup (`detectLevel`) instead of a regex, timestamp layouts chosen from the
  first byte rather than tried in turn, and case-insensitive substring folding in place. The ISO timestamp
  is read as a **token**, not a fixed width — slicing 25 characters chopped the zone off
  `2026-08-28T23:03:24.804642+02:00` and filed the line an hour wrong outside UTC. `Filter.Highlights`
  returns **UTF-16** offsets, because Go counts bytes and the browser slices by code unit.

Frontend `components/logs/`: `filter-bar.tsx` holds the one filter and the Live/History switch, because
"these errors are scrolling past, when did they start" is one thought. Live applies as you type
(debounced; the socket restarts, which is what makes the prefill meaningful); History runs on Enter,
because a keystroke-triggered full scan would queue a pass over gigabytes per character.
`log-console.tsx` uses `content-visibility` rather than a virtualiser — off-screen rows skip layout while
the scrollbar stays honest, wrapped rows keep real heights, and the browser's own find still works.
**Pausing holds incoming lines instead of dropping them.** `histogram.tsx` is matches by level over time;
clicking a column narrows the window to it.
