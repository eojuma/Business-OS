"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { AppShell } from "@/components/app-shell";
import { AuthProvider, useAuth } from "@/hooks/useAuth";

/** Waits for the user session before the permission-aware shell renders. */
function DashboardFrame({ children }: { children: React.ReactNode }) {
  const { loading } = useAuth();

  if (loading) {
    return (
      <main className="flex min-h-screen items-center justify-center bg-background p-6">
        <div className="text-center">
          <div className="mx-auto h-8 w-8 animate-spin rounded-full border-2 border-border border-t-primary" />
          <p className="mt-3 text-sm text-muted">Loading your permissions...</p>
        </div>
      </main>
    );
  }

  return <AppShell>{children}</AppShell>;
}

/** Guards the dashboard routes behind a stored session token. */
export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();
  const [checked, setChecked] = useState(false);

  useEffect(() => {
    const token = localStorage.getItem("token");
    if (!token) {
      router.push("/login");
    } else {
      setChecked(true);
    }
  }, [router]);

  if (!checked) {
    return <main className="flex min-h-screen items-center justify-center bg-background p-6"><div className="text-center"><div className="mx-auto h-8 w-8 animate-spin rounded-full border-2 border-border border-t-primary"/><p className="mt-3 text-sm text-muted">Opening your workspace...</p></div></main>;
  }

  return (
    <AuthProvider>
      <DashboardFrame>{children}</DashboardFrame>
    </AuthProvider>
  );
}
