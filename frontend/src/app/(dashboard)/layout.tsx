"use client"

import { useEffect } from "react"
import { usePathname, useRouter } from "next/navigation"
import { useAuth } from "@/hooks/use-auth"
import { AppSidebar, navLocation } from "@/components/app-sidebar"
import { Spinner } from "@/components/state"
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar"
import { Separator } from "@/components/ui/separator"
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb"

export default function DashboardLayout({ children }: { children: React.ReactNode }) {
  const { status, loading } = useAuth()
  const router = useRouter()
  const pathname = usePathname()
  const here = navLocation(pathname)

  // Redirecting here is a convenience, not a security control: every API call
  // behind this shell is independently authenticated by the server.
  useEffect(() => {
    if (!loading && !status?.authenticated) router.replace("/login")
  }, [loading, status, router])

  if (loading || !status?.authenticated) {
    return (
      <div className="flex min-h-screen items-center justify-center">
        <Spinner className="size-6 text-muted-foreground" />
      </div>
    )
  }

  return (
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset>
        <header className="sticky top-0 z-10 flex h-14 shrink-0 items-center gap-2 border-b bg-background/95 px-4 backdrop-blur">
          <SidebarTrigger className="-ml-1" />
          <Separator orientation="vertical" className="mr-2 h-4" />
          {here && (
            <Breadcrumb>
              <BreadcrumbList>
                {here.group && (
                  <>
                    <BreadcrumbItem className="hidden sm:block">{here.group}</BreadcrumbItem>
                    <BreadcrumbSeparator className="hidden sm:block" />
                  </>
                )}
                <BreadcrumbItem>
                  <BreadcrumbPage className="font-medium">{here.title}</BreadcrumbPage>
                </BreadcrumbItem>
              </BreadcrumbList>
            </Breadcrumb>
          )}
          <span className="flex-1" />
          <span className="truncate text-sm text-muted-foreground">{status.user?.username}</span>
        </header>
        <main className="flex flex-1 flex-col gap-6 p-4 md:p-6">{children}</main>
      </SidebarInset>
    </SidebarProvider>
  )
}
