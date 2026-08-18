"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import {
  Activity,
  Archive,
  Box,
  Database,
  FileText,
  FolderTree,
  GitBranch,
  Globe,
  ListChecks,
  PackageCheck,
  LogOut,
  Rocket,
  ScrollText,
  Server,
  Shield,
  ShieldCheck,
  TerminalSquare,
  Users,
} from "lucide-react"
import { useAuth } from "@/hooks/use-auth"
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
} from "@/components/ui/sidebar"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"

type NavItem = {
  title: string
  href: string
  icon: React.ComponentType<{ className?: string }>
  /** Hidden unless the signed-in role holds this capability. */
  capability?: Capability
}

const NAV: { label: string; items: NavItem[] }[] = [
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

export function AppSidebar() {
  const pathname = usePathname()
  const { status, can, logout } = useAuth()

  const isActive = (href: string) =>
    href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(`${href}/`)

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" asChild>
              <Link href="/">
                <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground">
                  <Server className="size-4" />
                </div>
                <div className="grid flex-1 text-left text-sm leading-tight">
                  <span className="truncate font-semibold">VPS Dashboard</span>
                  <span className="truncate text-xs text-muted-foreground">
                    {status?.user?.username ?? "not signed in"}
                  </span>
                </div>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent>
        {NAV.map((group) => {
          const items = group.items.filter((item) => !item.capability || can(item.capability))
          if (items.length === 0) return null
          return (
            <SidebarGroup key={group.label}>
              <SidebarGroupLabel>{group.label}</SidebarGroupLabel>
              <SidebarGroupContent>
                <SidebarMenu>
                  {items.map((item) => (
                    <SidebarMenuItem key={item.href}>
                      <SidebarMenuButton
                        asChild
                        isActive={isActive(item.href)}
                        tooltip={item.title}
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

      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton asChild tooltip="Account">
              <Link href="/account">
                <ShieldCheck className="size-4" />
                <span className="flex-1">Account</span>
                {status?.user && (
                  <Badge variant="outline" className="text-[10px] uppercase">
                    {status.user.role}
                  </Badge>
                )}
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
          <SidebarMenuItem>
            <Button
              variant="ghost"
              size="sm"
              className="w-full justify-start gap-2 px-2 text-muted-foreground"
              onClick={() => logout()}
            >
              <LogOut className="size-4" />
              <span className="group-data-[collapsible=icon]:hidden">Sign out</span>
            </Button>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}
