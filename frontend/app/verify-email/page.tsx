"use client";

import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import { ThemeToggle } from "@/components/theme-toggle";

type State = "verifying" | "success" | "invalid" | "expired" | "retry";

export default function VerifyEmailPage() {
  const [state, setState] = useState<State>("verifying");
  const [email, setEmail] = useState("");
  const [notice, setNotice] = useState("");
  const [sending, setSending] = useState(false);
  // Strict Mode replays effects on the same instance; this ref makes the
  // single-use token verification fire exactly once per page instance.
  const startedRef = useRef(false);

  useEffect(() => {
    if (startedRef.current) return;
    startedRef.current = true;

    const token = new URLSearchParams(window.location.search).get("token") || "";
    if (!token) {
      setState("invalid");
      return;
    }
    (async () => {
      try {
        await api.post("/auth/email/verify", { token });
        setState("success");
      } catch (e: any) {
        const code = e.response?.data?.code;
        if (code === "verification_expired") {
          setState("expired");
        } else if (code === "verification_invalid") {
          setState("invalid");
        } else {
          // Server/network failure: the token is still unused, so retrying the
          // same link is the right guidance.
          setState("retry");
        }
      }
    })();
  }, []);

  async function resend(e: React.FormEvent) {
    e.preventDefault();
    if (!email) return;
    setSending(true);
    setNotice("");
    try {
      await api.post("/auth/email/resend", { email });
      setNotice("If that address needs verification, a new link is on its way.");
    } catch {
      setNotice("Could not send the email. Try again shortly.");
    } finally {
      setSending(false);
    }
  }

  return (
    <main className="min-h-screen bg-background">
      <nav className="mx-auto flex max-w-7xl items-center justify-between px-5 py-5 lg:px-8">
        <Link href="/" className="text-xl font-bold">
          Business <span className="text-primary">OS</span>
        </Link>
        <ThemeToggle />
      </nav>

      <div className="mx-auto max-w-md px-5 py-16">
        <div className="panel p-8 text-center">
          {state === "verifying" && (
            <>
              <h1 className="text-2xl font-bold">Verifying your email…</h1>
              <p className="mt-3 text-sm text-muted">One moment.</p>
            </>
          )}

          {state === "success" && (
            <>
              <h1 className="text-2xl font-bold">Email verified</h1>
              <p className="mt-3 text-sm text-muted">
                Your address is confirmed. You can sign in now.
              </p>
              <Link href="/login" className="btn-primary mt-6 inline-flex">
                Go to sign in
              </Link>
            </>
          )}

          {state === "retry" && (
            <>
              <h1 className="text-2xl font-bold">Could not verify your email</h1>
              <p className="mt-3 text-sm text-muted">
                Something went wrong on our side. Your link is still valid — try the
                same link again.
              </p>
              <button
                type="button"
                onClick={() => window.location.reload()}
                className="btn-primary mt-6 inline-flex"
              >
                Try again
              </button>
            </>
          )}

          {(state === "invalid" || state === "expired") && (
            <>
              <h1 className="text-2xl font-bold">
                {state === "expired" ? "This link has expired" : "This link is not valid"}
              </h1>
              <p className="mt-3 text-sm text-muted">
                {state === "expired"
                  ? "Verification links expire after 24 hours. Enter your email to get a new one."
                  : "The link may have already been used. Enter your email to get a new one."}
              </p>
              <form onSubmit={resend} className="mt-6 space-y-3 text-left">
                <input
                  className="field"
                  type="email"
                  placeholder="Email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  required
                />
                <button className="btn-primary w-full" disabled={sending}>
                  {sending ? "Sending…" : "Resend verification email"}
                </button>
              </form>
              {notice && (
                <p className="mt-3 text-sm text-muted">{notice}</p>
              )}
            </>
          )}
        </div>

        <p className="mt-6 text-center text-sm text-muted">
          <Link href="/login" className="font-semibold text-primary">
            Back to sign in
          </Link>
        </p>
      </div>
    </main>
  );
}
