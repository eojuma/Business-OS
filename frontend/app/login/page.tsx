"use client";

import Link from "next/link";
import { useState } from "react";
import { useRouter } from "next/navigation";
import { ArrowRight, Store } from "lucide-react";
import { api } from "@/lib/api";
import { GoogleSignIn } from "@/components/google-signin";

export default function LoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [needsWorkspace, setNeedsWorkspace] = useState(false);
  const [loading, setLoading] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    setNeedsWorkspace(false);
    setLoading(true);
    try {
      const r = await api.post("/auth/login", { email, password });
      localStorage.setItem("token", r.data.data.token);
      router.push("/dashboard");
    } catch (e: any) {
      setError(e.response?.data?.error || "Unable to sign in");
    } finally {
      setLoading(false);
    }
  }

  return (
    <main className="grid min-h-screen lg:grid-cols-2">
      <section className="hidden bg-[#153d2b] p-10 text-white lg:flex lg:flex-col lg:justify-between">
        <Link href="/" className="text-xl font-bold">
          Business <span className="text-[#8ed2aa]">OS</span>
        </Link>
        <div className="max-w-md">
          <Store size={30} className="mb-8 text-[#8ed2aa]" />
          <h1 className="text-5xl font-bold leading-tight tracking-tight">
            Your store, with a clearer head.
          </h1>
          <p className="mt-5 leading-7 text-[#c5dfcf]">
            Keep the floor moving while Business OS keeps the numbers, stock and
            decisions in view.
          </p>
        </div>
        <p className="text-sm text-[#a8c8b5]">
          Built for the work behind every growing store.
        </p>
      </section>

      <section className="flex items-center justify-center p-6 sm:p-12">
        <div className="w-full max-w-md">
          <Link href="/" className="mb-12 inline-block text-xl font-bold lg:hidden">
            Business <span className="text-[#16794c]">OS</span>
          </Link>
          <p className="eyebrow mb-3">Welcome back</p>
          <h1 className="text-3xl font-bold tracking-tight">Sign in to your workspace</h1>
          <p className="mt-2 text-sm text-[#718078]">Your operating picture is waiting.</p>

          <form onSubmit={submit} className="mt-8 space-y-4">
            {error && (
              <p className="rounded-lg bg-red-50 p-3 text-sm text-red-700">{error}</p>
            )}
            {needsWorkspace && (
              <p className="rounded-lg bg-amber-50 p-3 text-sm text-amber-800">
                No workspace is linked to that Google account.{" "}
                <Link href="/signup" className="font-semibold underline">
                  Create a workspace
                </Link>{" "}
                to continue.
              </p>
            )}

            <GoogleSignIn
              onWorkspaceRequired={() => setNeedsWorkspace(true)}
              onError={setError}
              onReady={() => setError("")}
              text="signin_with"
              disabled={loading}
            />

            <div className="flex items-center gap-3 text-xs text-[#8a958d]">
              <span className="h-px flex-1 bg-[#e5e9e4]" />
              or
              <span className="h-px flex-1 bg-[#e5e9e4]" />
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

          <p className="mt-7 text-center text-sm text-[#718078]">
            New to Business OS?{" "}
            <Link href="/signup" className="font-semibold text-[#16794c]">
              Create a workspace
            </Link>
          </p>
        </div>
      </section>
    </main>
  );
}
