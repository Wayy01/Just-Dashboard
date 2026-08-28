"use client"

import Link from "next/link"
import { usePathname } from "next/navigation"
import { get } from "@/lib/api"
import { cn } from "@/lib/utils"
import { usePoll } from "@/hooks/use-poll"
import { ProxyProvider, type ProxyStatus } from "@/components/proxy/proxy-context"

/**
 * The proxy page is five areas — the sites, the certificates, what a visitor
 * actually gets over TLS, the non-HTTP streams, and every listening port. The
 * sidebar entry expands to them; this strip is the switcher when it is
 * collapsed.
 *
 * The layout polls one thing — which proxy this host runs — because the site
 * builder needs nginx and both it and the Overview read that. It does not gate
 * the section: certificates and ports are real questions with or without a
 * proxy installed.
 */
const TABS = [
  { title: "Overview", href: "/proxy" },
  { title: "Sites", href: "/proxy/sites" },
  { title: "Certificates", href: "/proxy/certificates" },
  { title: "TLS report", href: "/proxy/tls" },
  { title: "Streams", href: "/proxy/streams" },
  { title: "Ports", href: "/proxy/ports" },
]

export default function ProxyLayout({ children }: { children: React.ReactNode }) {
  const pathname = usePathname()
  const status = usePoll((signal) => get<ProxyStatus>("/proxy/status", undefined, signal), 60_000)

  return (
    <ProxyProvider
      value={{
        status: status.data,
        loading: status.loading,
        hasNginx: status.data?.nginx ?? false,
      }}
    >
      <div className="sticky top-0 z-10 border-b border-hairline bg-background/85 backdrop-blur-md">
        <nav className="mx-auto flex w-full max-w-[1600px] gap-1 overflow-x-auto px-4 md:px-6">
          {TABS.map((tab) => {
            const active =
              tab.href === "/proxy" ? pathname === "/proxy" : pathname.startsWith(tab.href)
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
    </ProxyProvider>
  )
}
