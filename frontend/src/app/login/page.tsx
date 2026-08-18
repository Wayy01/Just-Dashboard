"use client"

import { useEffect, useState } from "react"
import { useRouter } from "next/navigation"
import { KeyRound, Loader2, ShieldCheck } from "lucide-react"
import { toast } from "sonner"
import { ApiError, post } from "@/lib/api"
import { useAuth } from "@/hooks/use-auth"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"

type Step = "credentials" | "totp" | "enroll"

export default function LoginPage() {
  const router = useRouter()
  const { status, loading, login, verifyTotp, refresh } = useAuth()
  // "auto" defers to whatever the session says; the explicit values are set
  // only once the user has moved the flow forward themselves.
  const [chosenStep, setChosenStep] = useState<Step | "auto">("auto")
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
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
      toast.error("Sign in failed", {
        description: err instanceof ApiError ? err.message : String(err),
      })
    } finally {
      setBusy(false)
    }
  }

  const beginEnrollment = async () => {
    setBusy(true)
    try {
      setEnrollment(await post<{ secret: string; otpauthUrl: string }>("/auth/2fa/setup"))
    } catch (err) {
      toast.error("Could not start enrollment", {
        description: err instanceof ApiError ? err.message : String(err),
      })
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
      toast.error("Code rejected", {
        description: err instanceof ApiError ? err.message : String(err),
      })
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
      toast.error("Code rejected", {
        description: err instanceof ApiError ? err.message : String(err),
      })
      setCode("")
    } finally {
      setBusy(false)
    }
  }

  // Enrollment consumes the entered code, so the operator signs in again with
  // a fresh one — which also proves the authenticator really works.
  const finishEnrollment = async () => {
    setRecoveryCodes(null)
    setEnrollment(null)
    setStep("credentials")
    setPassword("")
    await refresh().catch(() => undefined)
    toast.success("Two-factor enabled", { description: "Sign in again with a code from your app." })
  }

  return (
    <div className="auth-backdrop flex min-h-screen items-center justify-center bg-background p-4">
      <Card className="w-full max-w-md">
        <CardHeader>
          <div className="mb-2 flex size-10 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            {step === "credentials" ? (
              <KeyRound className="size-5" />
            ) : (
              <ShieldCheck className="size-5" />
            )}
          </div>
          <CardTitle>
            {step === "credentials" && "Sign in"}
            {step === "totp" && "Two-factor code"}
            {step === "enroll" && "Set up two-factor"}
          </CardTitle>
          <CardDescription>
            {step === "credentials" && "VPS Dashboard administrator access."}
            {step === "totp" && "Enter the 6-digit code from your authenticator app."}
            {step === "enroll" &&
              "Two-factor authentication is mandatory. Enroll an authenticator to continue."}
          </CardDescription>
        </CardHeader>

        {step === "credentials" && (
          <form onSubmit={submitCredentials} className="flex flex-col gap-6">
            <CardContent className="space-y-4">
              <div className="space-y-2">
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
              <div className="space-y-2">
                <Label htmlFor="password">Password</Label>
                <Input
                  id="password"
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                />
              </div>
            </CardContent>
            <CardFooter>
              <Button type="submit" className="w-full" disabled={busy}>
                {busy && <Loader2 className="size-4 animate-spin" />}
                Continue
              </Button>
            </CardFooter>
          </form>
        )}

        {step === "totp" && (
          <form onSubmit={submitTotp} className="flex flex-col gap-6">
            <CardContent className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="code">Verification code</Label>
                <Input
                  id="code"
                  autoFocus
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  placeholder="000000"
                  className="text-center font-mono text-lg tracking-[0.4em]"
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  required
                />
                <p className="text-xs text-muted-foreground">
                  A recovery code works here too, and can be used once.
                </p>
              </div>
            </CardContent>
            <CardFooter>
              <Button type="submit" className="w-full" disabled={busy}>
                {busy && <Loader2 className="size-4 animate-spin" />}
                Verify
              </Button>
            </CardFooter>
          </form>
        )}

        {step === "enroll" && !recoveryCodes && (
          <CardContent className="space-y-4">
            {!enrollment ? (
              <Button className="w-full" onClick={beginEnrollment} disabled={busy}>
                {busy && <Loader2 className="size-4 animate-spin" />}
                Generate a secret
              </Button>
            ) : (
              <form onSubmit={submitEnrollment} className="flex flex-col gap-4">
                <div className="space-y-2">
                  <Label>Secret</Label>
                  <code className="block rounded-md border bg-muted p-3 font-mono text-xs break-all">
                    {enrollment.secret}
                  </code>
                  <p className="text-xs text-muted-foreground">
                    Add this to your authenticator app, then enter the code it shows.
                  </p>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="enroll-code">Verification code</Label>
                  <Input
                    id="enroll-code"
                    autoFocus
                    inputMode="numeric"
                    placeholder="000000"
                    className="text-center font-mono text-lg tracking-[0.4em]"
                    value={code}
                    onChange={(e) => setCode(e.target.value)}
                    required
                  />
                </div>
                <Button type="submit" className="w-full" disabled={busy}>
                  {busy && <Loader2 className="size-4 animate-spin" />}
                  Enable two-factor
                </Button>
              </form>
            )}
          </CardContent>
        )}

        {recoveryCodes && (
          <>
            <CardContent className="space-y-4">
              <Alert>
                <ShieldCheck className="size-4" />
                <AlertTitle>Save your recovery codes</AlertTitle>
                <AlertDescription>
                  Each code works once, in place of your authenticator. They are shown only now.
                </AlertDescription>
              </Alert>
              <div className="grid grid-cols-2 gap-2 rounded-md border bg-muted p-3 font-mono text-xs">
                {recoveryCodes.map((c) => (
                  <span key={c}>{c}</span>
                ))}
              </div>
            </CardContent>
            <CardFooter>
              <Button className="w-full" onClick={finishEnrollment}>
                I have saved them
              </Button>
            </CardFooter>
          </>
        )}
      </Card>
    </div>
  )
}
