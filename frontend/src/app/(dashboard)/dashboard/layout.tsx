"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { cn } from "@/lib/utils"

/**
 * The dashboard's own two pages: what version it is, and how it is configured.
 *
 * They are separated because they answer different questions and are used at
 * different moments — "what changed in 0.7" is read before an upgrade, "which
 * port does this answer on" is opened when something is wrong. The strip is
 * the same switcher Docker and Databases use, so the pattern is learned once.
 */
const TABS = [
  { title: "Version", href: "/dashboard" },
  { title: "Configuration", href: "/dashboard/configuration" },
]

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()

  return (
    <>
      <div className="sticky top-0 z-10 border-b border-hairline bg-background/85 backdrop-blur-md">
        <nav className="mx-auto flex w-full max-w-[1600px] gap-1 overflow-x-auto px-4 md:px-6">
          {TABS.map((tab) => {
            const active =
              tab.href === "/dashboard" ? pathname === "/dashboard" : pathname.startsWith(tab.href)
            return (
              <Link
                key={tab.href}
                href={tab.href}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "inline-flex h-11 shrink-0 items-center border-b-2 border-transparent px-3 text-[13px] font-medium whitespace-nowrap text-muted-foreground transition-colors hover:text-foreground",
                  active && "border-primary text-foreground",
                )}
              >
                {tab.title}
              </Link>
            )
          })}
        </nav>
      </div>
      {children}
    </>
  )
}
