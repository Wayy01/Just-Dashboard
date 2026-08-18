import type { Metadata } from "next"
import { Toaster } from "@/components/ui/sonner"
import { AuthProvider } from "@/hooks/use-auth"
import "./globals.css"

export const metadata: Metadata = {
  title: "VPS Dashboard",
  description: "Self-hosted server management",
  robots: { index: false, follow: false },
}

export default function RootLayout({ children }: { children: React.ReactNode }) {
  // Dark-only: an operator reads this during incidents, often at night, and a
  // theme toggle is not worth the flash-of-wrong-theme it costs.
  return (
    <html lang="en" className="dark" suppressHydrationWarning>
      <body className="antialiased">
        <AuthProvider>
          {children}
          <Toaster position="top-right" richColors closeButton />
        </AuthProvider>
      </body>
    </html>
  )
}
