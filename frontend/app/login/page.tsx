"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { ArrowRight, Store } from "lucide-react";
import { api } from "@/lib/api";
import { GoogleSignIn } from "@/components/google-signin";
import { ThemeToggle } from "@/components/theme-toggle";

export default function LoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [needsWorkspace, setNeedsWorkspace] = useState(false);
  const [sessionExpired, setSessionExpired] = useState(false);
  const [pendingEmail, setPendingEmail] = useState("");
  const [resendNotice, setResendNotice] = useState("");
  const [resending, setResending] = useState(false);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    // Read the query in an effect so the page stays hydration-safe without a
    // useSearchParams Suspense boundary.
    setSessionExpired(new URLSearchParams(window.location.search).get("expired") === "1");
  }, []);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setNeedsWorkspace(false);
    setPendingEmail("");
    setResendNotice("");
    setLoading(true);
    try {
      const r = await api.post("/auth/login", { email, password });
      localStorage.setItem("token", r.data.data.token);
      router.push("/dashboard");
    } catch (e: any) {
      const code = e.response?.data?.code;
      if (code === "email_not_verified") {
        setPendingEmail(email);
        setError(e.response?.data?.error || "Verify your email before signing in.");
      } else {
        setError(e.response?.data?.error || "Unable to sign in");
      }
    } finally {
      setLoading(false);
    }
  }

  async function resendVerification() {
    if (!pendingEmail || resending) return;
    setResendNotice("");
    setResending(true);
    try {
      await api.post("/auth/email/resend", { email: pendingEmail });
      setResendNotice("Verification email sent. Check your inbox.");
    } catch {
      setResendNotice("Could not send the email. Try again shortly.");
    } finally {
      setResending(false);
    }
  }

  return (
    <main className="relative grid min-h-screen lg:grid-cols-2">
      <div className="absolute right-4 top-4 z-10">
        <ThemeToggle />
      </div>
      <section className="hidden bg-primary-deep p-10 text-white lg:flex lg:flex-col lg:justify-between">
        <Link href="/" className="text-xl font-bold">
          Business <span className="text-primary-light">OS</span>
        </Link>
        <div className="max-w-md">
          <Store size={30} className="mb-8 text-primary-light" />
          <h1 className="text-5xl font-bold leading-tight tracking-tight">
            Your store, with a clearer head.
          </h1>
          <p className="mt-5 leading-7 text-primary-light-muted">
            Keep the floor moving while Business OS keeps the numbers, stock and
            decisions in view.
          </p>
        </div>
        <p className="text-sm text-primary-light-muted">
          Built for the work behind every growing store.
        </p>
      </section>

      <section className="flex items-center justify-center p-6 sm:p-12">
        <div className="w-full max-w-md">
          <Link href="/" className="mb-12 inline-block text-xl font-bold lg:hidden">
            Business <span className="text-primary">OS</span>
          </Link>
          <p className="eyebrow mb-3">Welcome back</p>
          <h1 className="text-3xl font-bold tracking-tight">Sign in to your workspace</h1>
          <p className="mt-2 text-sm text-muted">Your operating picture is waiting.</p>

          <form onSubmit={submit} className="mt-8 space-y-4">
            {sessionExpired && (
              <p className="rounded-lg bg-warning-soft p-3 text-sm text-warning-strong">
                Your session expired. Please sign in again.
              </p>
            )}
            {error && (
              <p className="rounded-lg bg-danger-soft p-3 text-sm text-danger">{error}</p>
            )}
            {needsWorkspace && (
              <p className="rounded-lg bg-warning-soft p-3 text-sm text-warning-strong">
                No workspace is linked to that Google account.{" "}
                <Link href="/signup" className="font-semibold underline">
                  Create a workspace
                </Link>{" "}
                to continue.
              </p>
            )}
            {pendingEmail && (
              <div className="rounded-lg bg-warning-soft p-3 text-sm text-warning-strong">
                <button
                  type="button"
                  onClick={resendVerification}
                  disabled={resending}
                  className="font-semibold underline disabled:opacity-50"
                >
                  {resending ? "Sending…" : "Resend verification email"}
                </button>
                {resendNotice && <p className="mt-1">{resendNotice}</p>}
              </div>
            )}

            <GoogleSignIn
              onWorkspaceRequired={() => setNeedsWorkspace(true)}
              onError={setError}
              onReady={() => setError("")}
              text="signin_with"
              disabled={loading}
            />

            <div className="flex items-center gap-3 text-xs text-subtle">
              <span className="h-px flex-1 bg-border" />
              or
              <span className="h-px flex-1 bg-border" />
            </div>

            <label className="block text-sm font-semibold">
              Email
              <input
                className="field mt-1.5"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
            </label>
            <label className="block text-sm font-semibold">
              Password
              <input
                className="field mt-1.5"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </label>
            <button className="btn-primary w-full py-3" disabled={loading}>
              {loading ? "Signing in" : "Sign in"}
              <ArrowRight size={16} />
            </button>
          </form>

          <p className="mt-7 text-center text-sm text-muted">
            New to Business OS?{" "}
            <Link href="/signup" className="font-semibold text-primary">
              Create a workspace
            </Link>
          </p>
        </div>
      </section>
    </main>
  );
}
