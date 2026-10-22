"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { AppShell } from "@/components/app-shell";

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

  return <AppShell>{children}</AppShell>;
}
