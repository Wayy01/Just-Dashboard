"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import {
  Activity,
  Archive,
  Box,
  ChevronsUpDown,
  Database,
  FileText,
  FolderTree,
  GitBranch,
  Globe,
  ListChecks,
  LogOut,
  PackageCheck,
  Palette,
  Rocket,
  ScrollText,
  Search,
  ShieldCheck,
  Shield,
  TerminalSquare,
  Users,
} from "lucide-react"
import { useAuth } from "@/hooks/use-auth"
import { useCommandPalette } from "@/components/command-palette"
import { Logo, LogoMark } from "@/components/logo"
import { UpdateNotice } from "@/components/update/update-notice"
import type { Capability } from "@/lib/types"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
  useSidebar,
} from "@/components/ui/sidebar"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

type NavItem = {
  title: string
  href: string
  icon: React.ComponentType<{ className?: string }>
  /** Hidden unless the signed-in role holds this capability. */
  capability?: Capability
}

export const NAV: { label: string; items: NavItem[] }[] = [
  {
    label: "Server",
    items: [
      { title: "Overview", href: "/", icon: Activity },
      { title: "Docker", href: "/docker", icon: Box },
      { title: "Processes", href: "/processes", icon: ListChecks },
      { title: "Logs", href: "/logs", icon: ScrollText },
    ],
  },
  {
    label: "Access",
    items: [
      { title: "Terminal", href: "/terminal", icon: TerminalSquare, capability: "terminal" },
      { title: "Files", href: "/files", icon: FolderTree },
      { title: "Git", href: "/git", icon: GitBranch },
      { title: "Databases", href: "/databases", icon: Database },
    ],
  },
  {
    label: "Network",
    items: [
      { title: "Proxy & TLS", href: "/proxy", icon: Globe },
      { title: "Security", href: "/security", icon: Shield },
    ],
  },
  {
    label: "Operations",
    items: [
      { title: "Updates", href: "/updates", icon: PackageCheck },
      { title: "Deployments", href: "/deploy", icon: Rocket },
      { title: "Backups", href: "/backups", icon: Archive },
      { title: "System users", href: "/system-users", icon: Users, capability: "system.admin" },
      { title: "Audit log", href: "/audit", icon: FileText, capability: "system.admin" },
    ],
  },
]

/** Entries that live in the footer menu rather than a nav group. */
export const PERSONAL_NAV: NavItem[] = [
  { title: "Account", href: "/account", icon: ShieldCheck },
  { title: "Appearance", href: "/appearance", icon: Palette },
]

/** Whether a nav entry owns the given path. */
export function navMatches(href: string, pathname: string) {
  return href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(`${href}/`)
}

/** The group and item a path belongs to, for the breadcrumb in the top bar. */
export function navLocation(pathname: string): { group?: string; title: string } | null {
  for (const group of NAV) {
    const item = group.items.find((i) => navMatches(i.href, pathname))
    if (item) return { group: group.label, title: item.title }
  }
  const personal = PERSONAL_NAV.find((i) => navMatches(i.href, pathname))
  if (personal) return { group: "You", title: personal.title }
  return null
}

export function AppSidebar() {
  const pathname = usePathname()
  const { can } = useAuth()
  const palette = useCommandPalette()
  const { state } = useSidebar()
  const collapsed = state === "collapsed"

  const isActive = (href: string) => navMatches(href, pathname)

  return (
    <Sidebar collapsible="icon" className="border-r border-sidebar-border">
      <SidebarHeader className="gap-3 p-3">
        {/* The wordmark alone: no tile, and no "Control panel" strapline under
            it. The strapline named the product category to somebody already
            inside the product, and the tile spent a third of the header's
            width saying nothing the name did not. */}
        <Link
          href="/"
          className="flex h-8 min-w-0 items-center rounded-lg outline-none focus-visible:ring-[3px] focus-visible:ring-sidebar-ring/50 group-data-[collapsible=icon]:justify-center"
        >
          <Logo className="group-data-[collapsible=icon]:hidden" />
          <LogoMark className="hidden group-data-[collapsible=icon]:block" />
        </Link>

        {/* The palette is the fastest route to any of fifteen pages, so it gets
            a permanent affordance rather than only a shortcut nobody
            discovers. Collapsed, it keeps its place in the rail as an icon. */}
        <button
          type="button"
          onClick={palette.open}
          className="flex h-8 w-full min-w-0 items-center gap-2 rounded-lg border border-sidebar-border bg-sidebar-accent/40 px-2 text-left text-[13px] text-muted-foreground transition-colors outline-none hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-[3px] focus-visible:ring-sidebar-ring/50 group-data-[collapsible=icon]:size-8 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:p-0"
        >
          <Search className="size-3.5 shrink-0" />
          <span className="flex-1 truncate group-data-[collapsible=icon]:hidden">Search</span>
          <kbd className="pointer-events-none rounded border border-sidebar-border bg-sidebar px-1 font-mono text-[10px] group-data-[collapsible=icon]:hidden">
            ⌘K
          </kbd>
        </button>
      </SidebarHeader>

      <SidebarContent className="gap-0 px-2">
        {NAV.map((group) => {
          const items = group.items.filter((item) => !item.capability || can(item.capability))
          if (items.length === 0) return null
          return (
            <SidebarGroup key={group.label} className="px-0 py-1.5">
              <SidebarGroupLabel className="eyebrow h-6 px-2">{group.label}</SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu className="gap-0.5">
                  {items.map((item) => (
                    <SidebarMenuItem key={item.href}>
                      <SidebarMenuButton
                        asChild
                        isActive={isActive(item.href)}
                        tooltip={item.title}
                        className="h-8 text-[13px]"
                      >
                        <Link href={item.href}>
                          <item.icon className="size-4" />
                          <span>{item.title}</span>
                        </Link>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  ))}
                </SidebarMenu>
              </SidebarGroupContent>
            </SidebarGroup>
          )
        })}
      </SidebarContent>

      <SidebarFooter className="gap-2 p-2">
        {/* Above the account card, not below it and not in the nav. It is the
            one thing in this shell that is about the dashboard rather than
            about the server, and it has to be seen without being looked for —
            a page for it would be a page nobody visits, which is how a
            self-hosted panel ends up eighteen months behind. It renders
            nothing when there is nothing to say. */}
        <UpdateNotice collapsed={collapsed} />
        <UserCard collapsed={collapsed} />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}

/**
 * Who is signed in, and everything that belongs to them rather than to the
 * server: the account page, the palette, signing out.
 *
 * A card at the foot of the rail rather than three more nav rows, because none
 * of it is a place in the product — it is the same identity menu on every
 * page, and mixing it into the nav made the nav look longer than it is.
 */
function UserCard({ collapsed }: { collapsed: boolean }) {
  const pathname = usePathname()
  const { status, logout } = useAuth()
  const user = status?.user
  const initials = (user?.username ?? "?").slice(0, 2).toUpperCase()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          className="flex w-full min-w-0 items-center gap-2.5 rounded-lg border border-sidebar-border bg-sidebar-accent/35 p-1.5 text-left transition-colors outline-none hover:bg-sidebar-accent focus-visible:ring-[3px] focus-visible:ring-sidebar-ring/50 data-[state=open]:bg-sidebar-accent group-data-[collapsible=icon]:size-8 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:border-0 group-data-[collapsible=icon]:p-0"
        >
          <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary/15 text-[11px] font-semibold text-primary">
            {initials}
          </span>
          <span className="grid min-w-0 flex-1 group-data-[collapsible=icon]:hidden">
            <span className="truncate text-[13px] leading-tight font-medium">
              {user?.username ?? "not signed in"}
            </span>
            <span className="truncate text-[11px] leading-tight text-muted-foreground capitalize">
              {user?.role ?? "—"}
            </span>
          </span>
          <ChevronsUpDown className="size-3.5 shrink-0 text-muted-foreground group-data-[collapsible=icon]:hidden" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        side={collapsed ? "right" : "top"}
        align="start"
        className="w-56"
        sideOffset={8}
      >
        <DropdownMenuLabel className="flex flex-col gap-0.5">
          <span className="text-[13px]">{user?.username}</span>
          <span className="text-[11px] font-normal text-muted-foreground">
            {user?.totpEnabled ? "Two-factor enabled" : "Two-factor not enrolled"}
          </span>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        {PERSONAL_NAV.map((item) => (
          <DropdownMenuItem key={item.href} asChild>
            <Link href={item.href} data-active={navMatches(item.href, pathname) || undefined}>
              <item.icon className="size-4" />
              {item.title}
            </Link>
          </DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onSelect={() => logout()}>
          <LogOut className="size-4" />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
