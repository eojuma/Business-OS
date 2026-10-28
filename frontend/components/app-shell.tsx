"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { BarChart3, Bell, Boxes, Brain, ChevronRight, CircleDollarSign, ClipboardList, Contact, LayoutDashboard, LogOut, Menu, Package, ScanLine, Settings, ShoppingCart, Truck, Users, X } from "lucide-react";
import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { ThemeToggle } from "@/components/theme-toggle";

const nav = [
  { href: "/dashboard", label: "Overview", icon: LayoutDashboard },
  { href: "/dashboard/sales", label: "Sales", icon: ShoppingCart },
  { href: "/dashboard/products", label: "Products", icon: Package },
  { href: "/dashboard/inventory", label: "Inventory", icon: Boxes },
  { href: "/dashboard/customers", label: "Customers", icon: Users },
  { href: "/dashboard/suppliers", label: "Suppliers", icon: Truck },
  { href: "/dashboard/purchases", label: "Purchases", icon: ClipboardList },
  { href: "/dashboard/receipts", label: "Receipt OCR", icon: ScanLine },
  { href: "/dashboard/finance", label: "Finance", icon: CircleDollarSign },
  { href: "/dashboard/analytics", label: "Analytics", icon: BarChart3 },
  { href: "/dashboard/reports", label: "Reports", icon: Contact },
  { href: "/dashboard/notifications", label: "Notifications", icon: Bell },
  { href: "/dashboard/assistant", label: "AI assistant", icon: Brain },
];

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname(); const router = useRouter(); const [open, setOpen] = useState(false);
  const [unread, setUnread] = useState(0);
  useEffect(() => {
    let active = true;
    const fetchUnread = () => {
      api.get("/notifications/unread-count")
        .then((r) => { if (active) setUnread(r.data.data?.unread || 0); })
        .catch(() => {});
    };
    fetchUnread();
    const id = setInterval(fetchUnread, 60000);
    return () => { active = false; clearInterval(id); };
  }, []);
  function logout() { localStorage.removeItem("token"); router.push("/login"); }
  return <div className="min-h-dvh bg-background">
    <header className="sticky top-0 z-30 flex h-16 items-center justify-between border-b border-border bg-card/95 px-4 backdrop-blur md:hidden">
      <button aria-label="Open navigation" onClick={() => setOpen(true)} className="rounded-lg p-2 text-muted hover:bg-hover"><Menu size={21}/></button>
      <Link href="/dashboard" className="font-bold tracking-tight">Business <span className="text-primary">OS</span></Link><ThemeToggle />
    </header>
    <aside className={`fixed inset-y-0 left-0 z-40 flex w-64 flex-col border-r border-border bg-card px-4 py-5 transition-transform md:translate-x-0 ${open ? "translate-x-0" : "-translate-x-full"}`}>
      <div className="mb-8 flex items-center justify-between px-2"><Link href="/dashboard" className="text-lg font-bold tracking-tight">Business <span className="text-primary">OS</span></Link><button className="p-1 md:hidden" onClick={() => setOpen(false)} aria-label="Close navigation"><X size={19}/></button></div>
      <p className="eyebrow px-2 pb-2">Workspace</p><nav className="space-y-0.5">{nav.map(({href,label,icon:Icon}) => { const active = href === "/dashboard" ? pathname === href : pathname.startsWith(href); return <Link key={href} href={href} onClick={() => setOpen(false)} className={`flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition ${active ? "bg-primary-soft text-primary-hover" : "text-muted hover:bg-hover hover:text-foreground"}`}><Icon size={17}/><span>{label}</span>{href === "/dashboard/notifications" && unread > 0 && <span className="ml-auto rounded-full bg-primary-solid px-1.5 py-0.5 text-[10px] font-bold text-white">{unread > 99 ? "99+" : unread}</span>}{active && <ChevronRight size={15} className={href === "/dashboard/notifications" && unread > 0 ? "" : "ml-auto"}/>}</Link>})}</nav>
      <div className="mt-auto space-y-1 border-t border-border-subtle pt-4"><div className="mb-2 px-1"><ThemeToggle className="w-full justify-center" /></div><Link href="/dashboard/settings" className="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-sm text-muted hover:bg-hover"><Settings size={17}/> Settings</Link><button onClick={logout} className="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-sm text-muted hover:bg-hover"><LogOut size={17}/> Sign out</button></div>
    </aside><div className="md:pl-64"><main className="mx-auto min-h-[calc(100dvh-4rem)] max-w-[1480px] p-4 sm:p-6 md:min-h-dvh lg:p-8">{children}</main></div>
    {open && <button aria-label="Close navigation overlay" className="fixed inset-0 z-30 bg-black/20 md:hidden" onClick={() => setOpen(false)}/>} 
  </div>;
}

export function PageHeader({ eyebrow, title, description, action }: { eyebrow?: string; title: string; description?: string; action?: React.ReactNode }) { return <div className="mb-7 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between"><div>{eyebrow && <p className="eyebrow mb-2">{eyebrow}</p>}<h1 className="text-2xl font-bold tracking-tight text-foreground sm:text-3xl">{title}</h1>{description && <p className="mt-1.5 text-sm text-muted">{description}</p>}</div>{action}</div>; }
export function StatCard({ label, value, note, icon: Icon, tone = "green" }: { label: string; value: string; note?: string; icon: React.ElementType; tone?: "green"|"amber"|"slate" }) { const styles = { green: "bg-primary-soft text-primary", amber: "bg-warning-soft text-warning-strong", slate: "bg-surface-muted text-muted" }; return <div className="panel p-5"><div className="flex items-start justify-between"><div><p className="text-sm text-muted">{label}</p><p className="mt-2 text-2xl font-bold tracking-tight">{value}</p>{note && <p className="mt-1 text-xs text-subtle">{note}</p>}</div><span className={`rounded-lg p-2.5 ${styles[tone]}`}><Icon size={18}/></span></div></div>; }
export function formatMoney(cents: number) { return `KSh ${(Number(cents || 0) / 100).toLocaleString("en-KE", {minimumFractionDigits: 2, maximumFractionDigits: 2})}`; }
