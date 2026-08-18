/**
 * Typed client for the dashboard API.
 *
 * Two things every call here relies on:
 *  - `credentials: "include"` so the HttpOnly session cookie travels; the
 *    token is never readable from JS, which is the point of it.
 *  - the `X-Confirm` header, which irreversible endpoints require. The server
 *    rejects the request without it, so a confirmation cannot be skipped by
 *    calling the API directly.
 */

export const API_BASE = process.env.NEXT_PUBLIC_API_BASE ?? "/api"

export type ApiErrorBody = {
  error: { code: string; message: string }
}

export class ApiError extends Error {
  code: string
  status: number
  /** The phrase the server wants echoed back, parsed out of its message. */
  confirmPhrase?: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.code = code
    if (code === "confirmation_required" || code === "confirmation_mismatch") {
      this.confirmPhrase = message.match(/"([^"]+)"/)?.[1]
    }
  }

  get needsConfirmation() {
    return this.code === "confirmation_required" || this.code === "confirmation_mismatch"
  }

  get isAuthProblem() {
    return this.status === 401 || this.code === "account_disabled"
  }

  get needsTotp() {
    return this.code === "totp_required" || this.code === "totp_enrollment_required"
  }
}

type RequestOptions = {
  method?: string
  body?: unknown
  /** Echoed in X-Confirm to satisfy a typed-confirmation endpoint. */
  confirm?: string
  signal?: AbortSignal
  query?: Record<string, string | number | boolean | undefined | null>
}

function buildUrl(path: string, query?: RequestOptions["query"]) {
  const url = `${API_BASE}${path}`
  if (!query) return url
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value !== undefined && value !== null && value !== "") {
      params.set(key, String(value))
    }
  }
  const qs = params.toString()
  return qs ? `${url}?${qs}` : url
}

export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {}
  if (options.body !== undefined) headers["Content-Type"] = "application/json"
  if (options.confirm) headers["X-Confirm"] = options.confirm

  const res = await fetch(buildUrl(path, options.query), {
    method: options.method ?? "GET",
    headers,
    credentials: "include",
    signal: options.signal,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  })

  if (res.status === 204) return undefined as T

  const text = await res.text()
  let parsed: unknown = undefined
  if (text) {
    try {
      parsed = JSON.parse(text)
    } catch {
      if (!res.ok) throw new ApiError(res.status, "unknown", text.slice(0, 300))
    }
  }

  if (!res.ok) {
    const body = parsed as ApiErrorBody | undefined
    throw new ApiError(
      res.status,
      body?.error?.code ?? "unknown",
      body?.error?.message ?? res.statusText,
    )
  }
  return parsed as T
}

export const get = <T>(path: string, query?: RequestOptions["query"], signal?: AbortSignal) =>
  api<T>(path, { query, signal })

export const post = <T>(path: string, body?: unknown, opts: Omit<RequestOptions, "method" | "body"> = {}) =>
  api<T>(path, { ...opts, method: "POST", body })

export const put = <T>(path: string, body?: unknown, opts: Omit<RequestOptions, "method" | "body"> = {}) =>
  api<T>(path, { ...opts, method: "PUT", body })

export const patch = <T>(path: string, body?: unknown, opts: Omit<RequestOptions, "method" | "body"> = {}) =>
  api<T>(path, { ...opts, method: "PATCH", body })

export const del = <T>(path: string, opts: Omit<RequestOptions, "method"> = {}) =>
  api<T>(path, { ...opts, method: "DELETE" })

/** Absolute URL for a download or archive link the browser follows directly. */
export function downloadUrl(path: string, query?: RequestOptions["query"]) {
  return buildUrl(path, query)
}

/**
 * WebSocket URL for a streaming endpoint.
 *
 * Next's rewrites proxy HTTP but not upgrade requests, so in development the
 * socket has to address the Go backend directly via NEXT_PUBLIC_WS_BASE.
 * Cookies ignore port when deciding same-site, so the session still travels.
 * In production both sit behind one reverse proxy and this is same-origin.
 */
export function wsUrl(path: string, query?: RequestOptions["query"]) {
  const explicit = process.env.NEXT_PUBLIC_WS_BASE
  const suffix = buildUrl(path, query)
  if (explicit) return `${explicit.replace(/^http/, "ws").replace(/\/$/, "")}${suffix}`
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:"
  return `${proto}//${window.location.host}${suffix}`
}
