"use client";

import Link from "next/link";
import { useCallback, useState } from "react";
import { useRouter } from "next/navigation";
import { ArrowRight, Check, Store } from "lucide-react";
import { api } from "@/lib/api";
import { GoogleSignIn } from "@/components/google-signin";
import { ThemeToggle } from "@/components/theme-toggle";

type GoogleClaims = { email?: string; name?: string };

// Used only to prefill the form; the backend re-verifies the credential and
// never trusts these values for identity.
function decodeGoogleClaims(credential: string): GoogleClaims {
  try {
    const part = credential.split(".")[1];
    if (!part) return {};
    const base64 = part.replace(/-/g, "+").replace(/_/g, "/");
    const padded = base64.padEnd(base64.length + ((4 - (base64.length % 4)) % 4), "=");
    const bytes = Uint8Array.from(atob(padded), (c) => c.charCodeAt(0));
    return JSON.parse(new TextDecoder().decode(bytes)) as GoogleClaims;
  } catch {
    return {};
  }
}

export default function SignupPage() {
  const router = useRouter();
  const [businessName, setBusinessName] = useState("");
  const [phone, setPhone] = useState("");
  const [ownerName, setOwnerName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [credential, setCredential] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [loading, setLoading] = useState(false);
  const [verificationEmail, setVerificationEmail] = useState("");

  const googleMode = credential !== "";

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setNotice("");
    setLoading(true);
    try {
      if (googleMode) {
        const r = await api.post("/auth/google", {
          credential,
          business_name: businessName,
          business_phone: phone,
        });
        localStorage.setItem("token", r.data.data.token);
        router.push("/dashboard");
        return;
      }

      const b = await api.post("/business", { name: businessName, phone });
      await api.post("/auth/register", {
        business_id: b.data.data.id,
        name: ownerName,
        email,
        password,
      });
      // No session until the owner confirms their email address.
      setVerificationEmail(email);
    } catch (e: any) {
      setError(e.response?.data?.error || "Unable to create workspace");
    } finally {
      setLoading(false);
    }
  }

  const handleWorkspaceRequired = useCallback((credentialValue: string) => {
    const claims = decodeGoogleClaims(credentialValue);
    if (claims.email) setEmail(claims.email);
    if (claims.name) setOwnerName(claims.name);
    setCredential(credentialValue);
    setError("");
    setNotice("Add your store details to finish creating the workspace.");
  }, []);

  function useEmailInstead() {
    setCredential("");
    setEmail("");
    setOwnerName("");
    setError("");
    setNotice("");
  }

  if (verificationEmail) {
    return (
      <main className="min-h-screen bg-background">
        <nav className="mx-auto flex max-w-7xl items-center justify-between px-5 py-5 lg:px-8">
          <Link href="/" className="text-xl font-bold">
            Business <span className="text-primary">OS</span>
          </Link>
          <ThemeToggle />
        </nav>
        <div className="mx-auto max-w-md px-5 py-16 text-center">
          <div className="panel p-8">
            <h1 className="text-2xl font-bold">Check your email</h1>
            <p className="mt-3 text-sm text-muted">
              We sent a verification link to{" "}
              <strong className="text-foreground">{verificationEmail}</strong>. Open
              it to finish setting up your workspace.
            </p>
            <p className="mt-2 text-xs text-subtle">
              The link expires in 24 hours. You can request a new one from the sign-in
              page.
            </p>
            <Link href="/login" className="btn-primary mt-6 inline-flex">
              Go to sign in
            </Link>
          </div>
        </div>
      </main>
    );
  }

  return (
    <main className="relative min-h-screen bg-background">
      <div className="absolute right-4 top-4 z-10">
        <ThemeToggle />
      </div>
      <nav className="mx-auto flex max-w-7xl items-center justify-between px-5 py-5 lg:px-8">
        <Link href="/" className="text-xl font-bold">
          Business <span className="text-primary">OS</span>
        </Link>
      </nav>

      <div className="mx-auto grid max-w-5xl gap-12 px-5 pb-16 pt-8 lg:grid-cols-[.8fr_1.2fr] lg:items-center lg:px-8">
        <div>
          <span className="mb-6 inline-flex rounded-xl bg-primary-soft p-3 text-primary">
            <Store size={23} />
          </span>
          <h1 className="text-4xl font-bold tracking-tight">
            A better operating rhythm starts here.
          </h1>
          <p className="mt-4 leading-7 text-muted">
            Set up your store workspace and bring the everyday work into one place.
          </p>
          <div className="mt-8 space-y-3 text-sm text-muted">
            <p className="flex gap-2">
              <Check size={17} className="text-primary" /> Products and stock in one view
            </p>
            <p className="flex gap-2">
              <Check size={17} className="text-primary" /> Sales, credit and supplier balances
            </p>
            <p className="flex gap-2">
              <Check size={17} className="text-primary" /> Business insights when you need them
            </p>
          </div>
        </div>

        <form onSubmit={submit} className="panel space-y-4 p-6 sm:p-8">
          <div>
            <p className="eyebrow mb-2">Get started</p>
            <h2 className="text-2xl font-bold">Create your workspace</h2>
          </div>

          {error && <p className="rounded-lg bg-danger-soft p-3 text-sm text-danger">{error}</p>}
          {notice && (
            <p className="rounded-lg bg-warning-soft p-3 text-sm text-warning-strong">{notice}</p>
          )}

          {!googleMode && (
            <>
              <GoogleSignIn
                onWorkspaceRequired={handleWorkspaceRequired}
                onError={setError}
                onReady={() => setError("")}
                text="signup_with"
                disabled={loading}
              />
              <div className="flex items-center gap-3 text-xs text-subtle">
                <span className="h-px flex-1 bg-border" />
                or
                <span className="h-px flex-1 bg-border" />
              </div>
            </>
          )}

          {googleMode && (
            <div className="rounded-lg bg-primary-soft p-3 text-sm text-primary-hover">
              Signing up with Google as <strong>{email}</strong>.
              <button
                type="button"
                onClick={useEmailInstead}
                className="ml-2 font-semibold underline"
              >
                Use email instead
              </button>
            </div>
          )}

          <div className="grid gap-4 sm:grid-cols-2">
            <label className="text-sm font-semibold sm:col-span-2">
              Store name
              <input
                className="field mt-1.5"
                value={businessName}
                onChange={(e) => setBusinessName(e.target.value)}
                required
              />
            </label>

            {googleMode ? (
              <label className="text-sm font-semibold sm:col-span-2">
                Phone
                <input
                  className="field mt-1.5"
                  value={phone}
                  onChange={(e) => setPhone(e.target.value)}
                />
              </label>
            ) : (
              <>
                <label className="text-sm font-semibold">
                  Your name
                  <input
                    className="field mt-1.5"
                    value={ownerName}
                    onChange={(e) => setOwnerName(e.target.value)}
                    required
                  />
                </label>
                <label className="text-sm font-semibold">
                  Phone
                  <input
                    className="field mt-1.5"
                    value={phone}
                    onChange={(e) => setPhone(e.target.value)}
                  />
                </label>
                <label className="text-sm font-semibold sm:col-span-2">
                  Email
                  <input
                    className="field mt-1.5"
                    type="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    required
                  />
                </label>
                <label className="text-sm font-semibold sm:col-span-2">
                  Password
                  <input
                    className="field mt-1.5"
                    type="password"
                    minLength={8}
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    required
                  />
                  <span className="mt-1 block text-xs font-normal text-subtle">
                    Use at least 8 characters.
                  </span>
                </label>
              </>
            )}
          </div>

          <button className="btn-primary w-full py-3" disabled={loading}>
            {loading
              ? "Creating workspace"
              : googleMode
                ? "Create workspace with Google"
                : "Create workspace"}
            <ArrowRight size={16} />
          </button>
        </form>
      </div>

      <div className="lg:col-span-2 flex justify-center">
        <p className="mt-2 text-center text-sm text-muted">
          Already have an account?{" "}
          <Link href="/login" className="font-semibold text-primary">
            Sign in
          </Link>
        </p>
      </div>
    </main>
  );
}
