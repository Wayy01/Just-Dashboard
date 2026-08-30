"use client"

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import {
  ArrowRight,
  Check,
  Copy,
  External,
  Eye,
  EyeOff,
  Key,
  Logs,
  NetworkDevice,
  ShieldCheck,
} from "@/components/icons"
import { notify } from "@/lib/toast"
import { ApiError, post } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useAuth } from "@/hooks/use-auth"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Notice, Spinner } from "@/components/state"
import { Logo } from "@/components/logo"

type Step = "credentials" | "totp" | "enroll"

/** The three states of the sign-in flow, in the order they happen. */
const STEPS: { id: Step; label: string }[] = [
  { id: "credentials", label: "Identify" },
  { id: "enroll", label: "Enrol" },
  { id: "totp", label: "Verify" },
]

export default function LoginPage() {
  const router = useRouter()
  const { status, loading, login, verifyTotp, refresh } = useAuth()
  // "auto" defers to whatever the session says; the explicit values are set
  // only once the user has moved the flow forward themselves.
  const [chosenStep, setChosenStep] = useState<Step | "auto">("auto")
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [showPassword, setShowPassword] = useState(false)
  const [code, setCode] = useState("")
  const [recoveryMode, setRecoveryMode] = useState(false)
  const [busy, setBusy] = useState(false)
  const [enrollment, setEnrollment] = useState<{ secret: string; otpauthUrl: string } | null>(null)
  const [recoveryCodes, setRecoveryCodes] = useState<string[] | null>(null)

  const step: Step =
    chosenStep !== "auto"
      ? chosenStep
      : status?.needsTotp
        ? "totp"
        : status?.needsEnrollment
          ? "enroll"
          : "credentials"
  const setStep = setChosenStep

  useEffect(() => {
    if (!loading && status?.authenticated) router.replace("/")
  }, [loading, status, router])

  const submitCredentials = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const next = await login(username, password)
      if (next.authenticated) router.replace("/")
      else if (next.needsTotp) setStep("totp")
      else if (next.needsEnrollment) setStep("enroll")
    } catch (err) {
      notify.error("Sign in failed", err instanceof ApiError ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const beginEnrollment = async () => {
    setBusy(true)
    try {
      setEnrollment(await post<{ secret: string; otpauthUrl: string }>("/auth/2fa/setup"))
    } catch (err) {
      notify.error("Could not start enrolment", err instanceof ApiError ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const submitEnrollment = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const res = await post<{ recoveryCodes: string[] }>("/auth/2fa/enable", { code })
      setRecoveryCodes(res.recoveryCodes)
      setCode("")
    } catch (err) {
      notify.error("Code rejected", err instanceof ApiError ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  const submitTotp = async (e: React.FormEvent) => {
    e.preventDefault()
    setBusy(true)
    try {
      const next = await verifyTotp(code)
      if (next.authenticated) router.replace("/")
    } catch (err) {
      notify.error("Code rejected", err instanceof ApiError ? err.message : String(err))
      setCode("")
    } finally {
      setBusy(false)
    }
  }

  // Enrolment consumes the entered code, so the operator signs in again with
  // a fresh one — which also proves the authenticator really works.
  const finishEnrollment = async () => {
    setRecoveryCodes(null)
    setEnrollment(null)
    setStep("credentials")
    setPassword("")
    await refresh().catch(() => undefined)
    notify.success("Two-factor enabled", {
      description: "Sign in again with a code from your app.",
    })
  }

  return (
    <div className="relative flex min-h-svh flex-col bg-background lg:grid lg:grid-cols-[1.05fr_1fr]">
      <div className="auth-backdrop pointer-events-none absolute inset-0" />
      <div className="auth-grid pointer-events-none absolute inset-0" />

      <BrandPanel />

      <main className="relative z-10 flex min-w-0 flex-1 items-center justify-center px-5 py-10 lg:py-12">
        <div className="w-full max-w-[26rem]">
          <Logo size="md" className="mb-6 lg:hidden" />

          <Stepper current={recoveryCodes ? "totp" : step} />

          <div className="raised mt-5 rounded-2xl border bg-card p-5 sm:p-6">
            {recoveryCodes ? (
              <RecoveryCodes codes={recoveryCodes} onDone={finishEnrollment} />
            ) : (
              <>
                <header className="mb-5 space-y-1">
                  <h1 className="text-lg leading-tight font-semibold">
                    {step === "credentials" && "Sign in"}
                    {step === "totp" && "Two-factor code"}
                    {step === "enroll" && "Set up two-factor"}
                  </h1>
                  <p className="text-[13px] leading-relaxed text-muted-foreground">
                    {step === "credentials" && "Administrator access to this server."}
                    {step === "totp" && "Enter the six-digit code from your authenticator app."}
                    {step === "enroll" &&
                      "Two-factor is mandatory here. Enrol an authenticator to continue."}
                  </p>
                </header>

                {step === "credentials" && (
                  <form onSubmit={submitCredentials} className="space-y-4">
                    <div className="space-y-1.5">
                      <Label htmlFor="username">Username</Label>
                      <Input
                        id="username"
                        autoFocus
                        autoComplete="username"
                        value={username}
                        onChange={(e) => setUsername(e.target.value)}
                        required
                      />
                    </div>
                    <div className="space-y-1.5">
                      <Label htmlFor="password">Password</Label>
                      <div className="relative">
                        <Input
                          id="password"
                          type={showPassword ? "text" : "password"}
                          autoComplete="current-password"
                          className="pr-9"
                          value={password}
                          onChange={(e) => setPassword(e.target.value)}
                          required
                        />
                        <button
                          type="button"
                          onClick={() => setShowPassword((v) => !v)}
                          aria-label={showPassword ? "Hide password" : "Show password"}
                          className="absolute top-1/2 right-1 flex size-7 -translate-y-1/2 items-center justify-center rounded-md text-muted-foreground transition-colors hover:text-foreground"
                        >
                          {showPassword ? (
                            <EyeOff className="size-3.5" />
                          ) : (
                            <Eye className="size-3.5" />
                          )}
                        </button>
                      </div>
                    </div>
                    <Button type="submit" className="w-full" disabled={busy}>
                      {busy ? <Spinner className="size-4" /> : null}
                      Continue
                      {!busy && <ArrowRight className="size-4" />}
                    </Button>
                  </form>
                )}

                {step === "totp" && (
                  <form onSubmit={submitTotp} className="space-y-4">
                    <div className="space-y-1.5">
                      <Label htmlFor="code">
                        {recoveryMode ? "Recovery code" : "Verification code"}
                      </Label>
                      <Input
                        id="code"
                        autoFocus
                        key={recoveryMode ? "recovery" : "totp"}
                        inputMode={recoveryMode ? "text" : "numeric"}
                        autoComplete="one-time-code"
                        placeholder={recoveryMode ? "xxxx-xxxx" : "000000"}
                        maxLength={recoveryMode ? 32 : 6}
                        className={cn(
                          "h-12 text-center font-mono",
                          recoveryMode ? "text-base" : "text-xl tracking-[0.45em]",
                        )}
                        value={code}
                        onChange={(e) => setCode(e.target.value)}
                        required
                      />
                    </div>
                    <Button type="submit" className="w-full" disabled={busy || !code}>
                      {busy && <Spinner className="size-4" />}
                      Verify
                    </Button>
                    <button
                      type="button"
                      className="block w-full text-center text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
                      onClick={() => {
                        setRecoveryMode((v) => !v)
                        setCode("")
                      }}
                    >
                      {recoveryMode
                        ? "Use your authenticator app instead"
                        : "Use a recovery code instead"}
                    </button>
                  </form>
                )}

                {step === "enroll" &&
                  (!enrollment ? (
                    <div className="space-y-4">
                      <Notice title="One secret, kept encrypted" icon={ShieldCheck}>
                        The seed is generated on the server and sealed with the dashboard&apos;s
                        master key. It is shown to you exactly once, here.
                      </Notice>
                      <Button className="w-full" onClick={beginEnrollment} disabled={busy}>
                        {busy && <Spinner className="size-4" />}
                        Generate a secret
                      </Button>
                    </div>
                  ) : (
                    <form onSubmit={submitEnrollment} className="space-y-4">
                      <SecretBlock secret={enrollment.secret} otpauthUrl={enrollment.otpauthUrl} />
                      <div className="space-y-1.5">
                        <Label htmlFor="enroll-code">Code from your app</Label>
                        <Input
                          id="enroll-code"
                          autoFocus
                          inputMode="numeric"
                          maxLength={6}
                          placeholder="000000"
                          className="h-12 text-center font-mono text-xl tracking-[0.45em]"
                          value={code}
                          onChange={(e) => setCode(e.target.value)}
                          required
                        />
                      </div>
                      <Button type="submit" className="w-full" disabled={busy || code.length < 6}>
                        {busy && <Spinner className="size-4" />}
                        Enable two-factor
                      </Button>
                    </form>
                  ))}
              </>
            )}
          </div>

          <p className="mt-5 text-center text-[11px] leading-relaxed text-muted-foreground">
            This host only accepts connections from its configured allowlist. Every sign-in attempt,
            successful or not, is recorded.
          </p>
        </div>
      </main>
    </div>
  )
}

/**
 * The left half on a wide screen.
 *
 * It is not decoration: someone reaching this page is about to hand root over
 * to a browser, and the three facts here are the reasons that is defensible.
 * Hidden below lg, where the form is the only thing worth the width.
 */
function BrandPanel() {
  const facts = [
    {
      icon: NetworkDevice,
      title: "Network allowlist first",
      body: "The allowlist runs before authentication, so an off-network attacker never reaches this form.",
    },
    {
      icon: ShieldCheck,
      title: "Two-factor, always",
      body: "A password alone gets a partial session that can reach nothing but the 2FA routes.",
    },
    {
      icon: Logs,
      title: "Everything is recorded",
      body: "Every state-changing request lands in the audit log with who, from where, and what happened.",
    },
  ]

  return (
    // One centred block rather than three pinned to top, middle and bottom: on
    // a tall window the pinned version leaves a hole where the eye expects the
    // product to be. The footnote is the exception — it belongs at the foot.
    <aside className="relative z-10 hidden flex-col justify-center border-r p-10 lg:flex xl:p-14">
      <div className="max-w-lg space-y-9">
        <Logo size="lg" />

        <h2 className="text-[28px] leading-[1.15] font-semibold tracking-tight text-balance xl:text-[32px]">
          One server. Metrics, containers, files, a real shell — behind one door.
        </h2>

        <ul className="space-y-5">
          {facts.map((fact) => (
            <li key={fact.title} className="flex gap-3">
              <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg border border-hairline bg-card text-primary">
                <fact.icon className="size-4" />
              </span>
              <div className="space-y-0.5">
                <p className="text-[13px] font-medium">{fact.title}</p>
                <p className="text-xs leading-relaxed text-muted-foreground">{fact.body}</p>
              </div>
            </li>
          ))}
        </ul>
      </div>

      <p className="absolute inset-x-10 bottom-8 text-[11px] text-muted-foreground xl:inset-x-14">
        This software is root-equivalent. Keep it off the public internet.
      </p>
    </aside>
  )
}

/** Where you are in a flow that can be one, two or three screens long. */
function Stepper({ current }: { current: Step }) {
  const index = STEPS.findIndex((s) => s.id === current)
  return (
    <ol className="flex items-center gap-2">
      {STEPS.map((step, i) => {
        const done = i < index
        const active = i === index
        return (
          <li key={step.id} className="flex min-w-0 flex-1 flex-col gap-1.5">
            <span
              className={cn(
                "h-0.5 w-full rounded-full transition-colors",
                done || active ? "bg-primary" : "bg-border",
              )}
            />
            <span
              className={cn(
                "flex items-center gap-1 text-[10px] font-semibold tracking-[0.14em] uppercase transition-colors",
                active ? "text-foreground" : "text-muted-foreground",
              )}
            >
              {done && <Check className="size-2.5 text-primary" />}
              {step.label}
            </span>
          </li>
        )
      })}
    </ol>
  )
}

/**
 * The TOTP seed, grouped in fours so it can be read aloud or typed without
 * losing your place, with the otpauth:// link beside it for the phone that is
 * already holding the authenticator.
 */
function SecretBlock({ secret, otpauthUrl }: { secret: string; otpauthUrl: string }) {
  const [copied, setCopied] = useState(false)
  const grouped = secret.replace(/\s+/g, "").match(/.{1,4}/g) ?? [secret]

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(secret)
      setCopied(true)
      setTimeout(() => setCopied(false), 1600)
    } catch {
      notify.error("Could not copy", "Select the text and copy it by hand.")
    }
  }

  return (
    <div className="space-y-2">
      <Label>Secret</Label>
      <div className="flex flex-wrap gap-1 rounded-lg border border-hairline bg-surface-sunken p-2.5">
        {grouped.map((chunk, i) => (
          <code key={i} className="font-mono text-[13px] tracking-widest">
            {chunk}
          </code>
        ))}
      </div>
      <div className="flex flex-wrap gap-2">
        <Button type="button" variant="outline" size="sm" onClick={copy}>
          {copied ? <Check className="size-3.5" /> : <Copy className="size-3.5" />}
          {copied ? "Copied" : "Copy secret"}
        </Button>
        <Button type="button" variant="ghost" size="sm" asChild>
          <a href={otpauthUrl}>
            <External className="size-3.5" />
            Open in authenticator
          </a>
        </Button>
      </div>
    </div>
  )
}

function RecoveryCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(codes.join("\n"))
      setCopied(true)
      setTimeout(() => setCopied(false), 1600)
    } catch {
      notify.error("Could not copy", "Select the codes and copy them by hand.")
    }
  }

  return (
    <div className="space-y-4">
      <header className="space-y-1">
        <h1 className="text-lg leading-tight font-semibold">Save your recovery codes</h1>
        <p className="text-[13px] leading-relaxed text-muted-foreground">
          Each one works once, in place of your authenticator. This is the only time they are shown.
        </p>
      </header>

      <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 rounded-lg border border-hairline bg-surface-sunken p-3 font-mono text-xs">
        {codes.map((c) => (
          <span key={c} className="tracking-wider">
            {c}
          </span>
        ))}
      </div>

      <div className="flex gap-2">
        <Button type="button" variant="outline" className="flex-1" onClick={copy}>
          {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
          {copied ? "Copied" : "Copy all"}
        </Button>
        <Button className="flex-1" onClick={onDone}>
          <Key className="size-4" />I have saved them
        </Button>
      </div>
    </div>
  )
}
