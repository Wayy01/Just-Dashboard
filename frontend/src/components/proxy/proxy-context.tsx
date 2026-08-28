"use client"

import { createContext, useContext } from "react"

export type ProxyStatus = {
  nginx: boolean
  caddy: boolean
  nginxVersion?: string
  caddyVersion?: string
  certbot: boolean
}

/**
 * What reverse proxy this host runs, polled once by the layout.
 *
 * `nginx` gates the site builder — the form writes nginx config and there is
 * nowhere to put it otherwise — and both the Sites page and the Overview read
 * it. The certificate and port pages do not: certificates on disk and
 * listening ports exist whether or not a proxy is installed, which is why the
 * section is never gated as a whole.
 */
export type ProxyContextValue = {
  status: ProxyStatus | undefined
  loading: boolean
  hasNginx: boolean
}

const ProxyContext = createContext<ProxyContextValue | null>(null)

export const ProxyProvider = ProxyContext.Provider

export function useProxy() {
  const value = useContext(ProxyContext)
  if (!value) throw new Error("useProxy must be used inside the proxy layout")
  return value
}
