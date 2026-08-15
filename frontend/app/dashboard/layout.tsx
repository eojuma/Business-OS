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
    return <main className="flex min-h-screen items-center justify-center bg-[#f7f9f6] p-6"><div className="text-center"><div className="mx-auto h-8 w-8 animate-spin rounded-full border-2 border-[#d9e0d9] border-t-[#16794c]"/><p className="mt-3 text-sm text-[#718078]">Opening your workspace...</p></div></main>;
  }

  return <AppShell>{children}</AppShell>;
}
