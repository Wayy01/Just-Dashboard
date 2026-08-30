import type { ForwardRefExoticComponent, RefAttributes, SVGProps } from "react"
import { Check } from "geist-icons"

/**
 * The icon vocabulary.
 *
 * Every glyph in the product comes from here, and here is the only file that
 * names the icon library. That is the same argument `logo.tsx` makes about the
 * wordmark: an icon set is a visual language, and one imported directly by a
 * hundred and forty feature files is one nobody can change, audit or keep
 * consistent. A page that wants "the delete glyph" should not be choosing
 * between `Trash`, `Trash2` and `Delete` — it asks for `Trash` and gets
 * whatever this file has decided that means.
 *
 * The set is Vercel's **Geist**, and the choice is not a preference. Geist is
 * drawn on a **16px grid**, which is the size this dashboard actually renders
 * icons at — `size-3.5` and `size-4` are 520 of the ~700 icon instances here.
 * A 24-grid stroke set (which is what we came from) is a 24px drawing scaled to
 * two thirds, so its geometry lands between pixels and its 2px strokes arrive at
 * 1.33px. Geist at 16px is drawn where it is drawn. The whole set is also
 * *filled paths* rather than strokes, which is why it survives the scaling it
 * does get: a filled shape at 14px stays a shape, where a hairline stroke turns
 * into a grey suggestion.
 *
 * **Both themes come free, and that is a property to protect.** Every glyph
 * here paints with `fill="currentColor"` and nothing else — no hard-coded hex,
 * no gradient, no light-mode assumption. An icon is therefore whatever the text
 * colour around it is, which means it inherits `text-muted-foreground` on a
 * near-white card and on a near-black one without either being a special case.
 * Geist's brand logos are the exception — several paint themselves from
 * Vercel's own `--ds-*` tokens, which do not exist in this palette and would
 * render as invisible black on a dark card. None are re-exported below, and
 * none should be without checking that first.
 *
 * Names are Geist's own, not the old set's. Where several old names collapsed
 * onto one glyph — a container, a box and a group of boxes were three drawings
 * of a cube — they are one name now, because that is what they always were.
 */

export type IconProps = SVGProps<SVGSVGElement> & {
  size?: string | number
  title?: string
}

/** What every icon in this file is, for anything that takes one as a value. */
export type Icon = ForwardRefExoticComponent<Omit<IconProps, "ref"> & RefAttributes<SVGSVGElement>>

export type { Icon as IconComponent }

// Re-exported so `typeof Check` above and the value below stay the same thing.
export { Check }

/* Arrows, chevrons and movement. `ChevronDouble*` is the "jump to the end"
   family; the single chevrons are navigation and disclosure. */
export {
  ArrowDown,
  ArrowLeft,
  ArrowRight,
  ArrowUp,
  ArrowCircleUp,
  ArrowUpRight,
  ArrowLeftRight,
  ArrowUpDown,
  ArrowMove,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronUp,
  ChevronDoubleDown,
  ChevronDoubleUp,
  CornerDownLeft,
  CornerUpLeft,
  CornerUpRight,
  External,
  Fullscreen,
  FullscreenClose,
} from "geist-icons"

/* The verbs. */
export {
  Plus,
  Minus,
  Cross,
  Copy,
  Clipboard,
  Trash,
  Pencil,
  FloppyDisk,
  Download,
  CloudDownload,
  CloudUpload,
  MagnifyingGlass,
  MagnifyingGlassMinus,
  Filter,
  RefreshClockwise,
  RotateClockwise,
  RotateCounterClockwise,
  Backspace,
  Crop,
  Sparkles,
  Wrench,
  Calculator,
  Fingerprint,
  Hash,
  Command,
  Play,
  Pause,
  StopCircle,
  PlusSquareSmall,
} from "geist-icons"

/* Text and matching affordances — the search bar's case/word/regex toggles and
   the editor's wrap switch. `SlashForward` is the regex toggle because `/…/` is
   how a regular expression is written, which is a better glyph than any icon
   set's attempt to draw one. */
export {
  TextFormat,
  TextTitle,
  TextUppercase,
  SlashForward,
  CodeWrap,
  CodeBracket,
  Code,
} from "geist-icons"

/* State and feedback.
 *
 * `Stop` is Geist's circled exclamation — upstream named it for the road sign,
 * not the media control, and it is the app's alert-in-a-circle. The media stop
 * is `StopCircle`, which is the square in a ring. Getting these two the wrong
 * way round puts a warning triangle on the button that stops a container. */
export {
  LoaderCircle,
  CheckCircle,
  CrossCircle,
  Status,
  Slash,
  Question,
  Information,
  Stop,
  Warning,
  WarningFill,
  Stopwatch,
  Clock,
  ClockRewind,
  Bell,
  Bug,
  Lightning,
  Inbox,
  Heart,
  Lifebuoy,
} from "geist-icons"

/* The machine, and the things running on it.
 *
 * `Servers` carries both a host and its storage: the set has one stacked-slab
 * drawing and it reads as a rack or as drive bays depending on the label beside
 * it, which is the honest outcome when the alternative is drafting a hard disk
 * nobody has owned in a decade. `GridSquare` is memory — a grid of cells is the
 * nearest true thing to a DIMM in a set with no RAM glyph. */
export {
  Servers,
  Cpu,
  GridSquare,
  Gauge,
  ChartActivity,
  LineChart,
  Monitoring,
  NetworkDevice,
  Connection,
  Router,
  Route,
  Globe,
  Database,
  Layout,
  SidebarLeft,
  SidebarRight,
  Footer,
  ListOrdered,
  ListUnordered,
  Monorepo,
  GridMasonry,
  Box,
  Layers,
  Puzzle,
  Archive,
  Terminal,
  TerminalWindow,
  Logs,
  Rss,
  Notes,
} from "geist-icons"

/* git and deployment. */
export { GitBranch, GitCommit, GitMerge, GitPullRequest, BranchPlus } from "geist-icons"

/* Security. `FirewallCheck` and `SecureConnection` are why this set suits the
   product: they are drawings of the things the security pages are about,
   rather than a padlock standing in for all of them. */
export {
  Shield,
  ShieldCheck,
  ShieldOff,
  FirewallCheck,
  SecureConnection,
  LockClosed,
  LockOpen,
  Key,
  Inspect,
  Crosshair,
  SignIn,
} from "geist-icons"

/* People and the account menu. */
export { Users, UserPlus, UserMinus, UserSettings, Logout, Home, DesktopDevice } from "geist-icons"

/* Appearance and view controls. `BlendMode` is the appearance page: two discs
   overlapping is a light/dark drawing, where a painter's palette promises a
   colour picker this product does not have. */
export {
  BlendMode,
  Sun,
  Moon,
  Eye,
  EyeOff,
  Star,
  StarFill,
  Pin,
  MoreHorizontal,
  SettingsGear,
  SettingsSliders,
  BookOpen,
  Link,
  Linked,
} from "geist-icons"

/* Files and folders. The acronym family — `AcronymJson`, `AcronymCsv` and the
   rest — has no equivalent in a general-purpose set and is what lets the file
   browser say `json` instead of drawing braces and hoping. See
   `files/file-icon.tsx`, which is where they are spent. */
export {
  File,
  FileText,
  FileZip,
  FolderClosed,
  FolderOpen,
  FolderPlus,
  FolderMinus,
  PreviewDocument,
  Image,
  Video,
  Music,
  Location,
  AcronymCsv,
  AcronymGif,
  AcronymJpg,
  AcronymJson,
  AcronymMarkdown,
  AcronymSvg,
} from "geist-icons"
