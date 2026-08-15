"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { BarChart3, Bell, Boxes, Brain, ChevronRight, CircleDollarSign, ClipboardList, Contact, LayoutDashboard, LogOut, Menu, Package, Settings, ShoppingCart, Truck, Users, X } from "lucide-react";
import { useState } from "react";

const nav = [
  { href: "/dashboard", label: "Overview", icon: LayoutDashboard },
  { href: "/dashboard/sales", label: "Sales", icon: ShoppingCart },
  { href: "/dashboard/products", label: "Products", icon: Package },
  { href: "/dashboard/inventory", label: "Inventory", icon: Boxes },
  { href: "/dashboard/customers", label: "Customers", icon: Users },
  { href: "/dashboard/suppliers", label: "Suppliers", icon: Truck },
  { href: "/dashboard/purchases", label: "Purchases", icon: ClipboardList },
  { href: "/dashboard/finance", label: "Finance", icon: CircleDollarSign },
  { href: "/dashboard/analytics", label: "Analytics", icon: BarChart3 },
  { href: "/dashboard/reports", label: "Reports", icon: Contact },
  { href: "/dashboard/assistant", label: "AI assistant", icon: Brain },
];

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname(); const router = useRouter(); const [open, setOpen] = useState(false);
  function logout() { localStorage.removeItem("token"); router.push("/login"); }
  return <div className="min-h-screen bg-[#f7f9f6]">
    <header className="sticky top-0 z-30 flex h-16 items-center justify-between border-b border-[#e5e9e4] bg-white/95 px-4 backdrop-blur md:hidden">
      <button aria-label="Open navigation" onClick={() => setOpen(true)} className="rounded-lg p-2 text-[#4f5c53] hover:bg-[#f2f5f1]"><Menu size={21}/></button>
      <Link href="/dashboard" className="font-bold tracking-tight">Business <span className="text-[#16794c]">OS</span></Link><div className="w-8" />
    </header>
    <aside className={`fixed inset-y-0 left-0 z-40 flex w-64 flex-col border-r border-[#e5e9e4] bg-white px-4 py-5 transition-transform md:translate-x-0 ${open ? "translate-x-0" : "-translate-x-full"}`}>
      <div className="mb-8 flex items-center justify-between px-2"><Link href="/dashboard" className="text-lg font-bold tracking-tight">Business <span className="text-[#16794c]">OS</span></Link><button className="p-1 md:hidden" onClick={() => setOpen(false)} aria-label="Close navigation"><X size={19}/></button></div>
      <p className="eyebrow px-2 pb-2">Workspace</p><nav className="space-y-0.5">{nav.map(({href,label,icon:Icon}) => { const active = href === "/dashboard" ? pathname === href : pathname.startsWith(href); return <Link key={href} href={href} onClick={() => setOpen(false)} className={`flex items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition ${active ? "bg-[#e7f4ec] text-[#12633e]" : "text-[#647168] hover:bg-[#f2f5f1] hover:text-[#17211b]"}`}><Icon size={17}/><span>{label}</span>{active && <ChevronRight size={15} className="ml-auto"/>}</Link>})}</nav>
      <div className="mt-auto space-y-1 border-t border-[#eef1ed] pt-4"><Link href="/dashboard/settings" className="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-sm text-[#647168] hover:bg-[#f2f5f1]"><Settings size={17}/> Settings</Link><button onClick={logout} className="flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-sm text-[#647168] hover:bg-[#f2f5f1]"><LogOut size={17}/> Sign out</button></div>
    </aside><div className="md:pl-64"><main className="mx-auto min-h-screen max-w-[1480px] p-4 sm:p-6 lg:p-8">{children}</main></div>
    {open && <button aria-label="Close navigation overlay" className="fixed inset-0 z-30 bg-black/20 md:hidden" onClick={() => setOpen(false)}/>} 
  </div>;
}

export function PageHeader({ eyebrow, title, description, action }: { eyebrow?: string; title: string; description?: string; action?: React.ReactNode }) { return <div className="mb-7 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between"><div>{eyebrow && <p className="eyebrow mb-2">{eyebrow}</p>}<h1 className="text-2xl font-bold tracking-tight text-[#17211b] sm:text-3xl">{title}</h1>{description && <p className="mt-1.5 text-sm text-[#718078]">{description}</p>}</div>{action}</div>; }
export function StatCard({ label, value, note, icon: Icon, tone = "green" }: { label: string; value: string; note?: string; icon: React.ElementType; tone?: "green"|"amber"|"slate" }) { const styles = { green: "bg-[#e7f4ec] text-[#16794c]", amber: "bg-[#fff3df] text-[#b36f16]", slate: "bg-[#eef1f4] text-[#52616a]" }; return <div className="panel p-5"><div className="flex items-start justify-between"><div><p className="text-sm text-[#718078]">{label}</p><p className="mt-2 text-2xl font-bold tracking-tight">{value}</p>{note && <p className="mt-1 text-xs text-[#8a958d]">{note}</p>}</div><span className={`rounded-lg p-2.5 ${styles[tone]}`}><Icon size={18}/></span></div></div>; }
export function formatMoney(cents: number) { return `KSh ${(Number(cents || 0) / 100).toLocaleString("en-KE", {minimumFractionDigits: 2, maximumFractionDigits: 2})}`; }
