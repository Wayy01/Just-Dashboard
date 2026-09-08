import type { Metadata } from "next"
import { headers } from "next/headers"
import { Toaster } from "@/components/ui/sonner"
import { AuthProvider } from "@/hooks/use-auth"
import { themeBootstrapScript } from "@/lib/themes"
import "./globals.css"

export const metadata: Metadata = {
  title: "Just Dashboard",
  description: "Self-hosted server management",
  robots: { index: false, follow: false },
}

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  const nonce = (await headers()).get("x-nonce") ?? undefined
  // The server has no way to know which mode this browser chose, so it always
  // renders the default (dark) and the inline script below corrects the class
  // before the first paint. suppressHydrationWarning covers exactly that
  // divergence, which is confined to <html>.
  return (
    <html lang="en" className="dark" suppressHydrationWarning>
      <head>
        <script nonce={nonce} dangerouslySetInnerHTML={{ __html: themeBootstrapScript() }} />
      </head>
      <body className="antialiased">
        <AuthProvider>
          {children}
          <Toaster position="top-right" richColors closeButton />
        </AuthProvider>
      </body>
    </html>
  )
}
