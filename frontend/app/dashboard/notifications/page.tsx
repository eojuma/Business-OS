"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/api";
import { PageHeader } from "@/components/app-shell";
import { AlertTriangle, Bell, BellOff, CheckCheck, CreditCard, Package } from "lucide-react";

type Notification = {
  id: string;
  type: string;
  severity: "info" | "warning" | "critical";
  recipient: string;
  title: string;
  message: string;
  entity_name: string;
  read: boolean;
  created_at: string;
};

const severityStyles: Record<string, string> = {
  info: "bg-[#eef1f4] text-[#52616a]",
  warning: "bg-[#fff3df] text-[#b36f16]",
  critical: "bg-[#fdeeea] text-[#a3543f]",
};

function typeIcon(type: string) {
  if (type === "low_stock") return Package;
  if (type === "credit_limit") return CreditCard;
  return AlertTriangle;
}

export default function NotificationsPage() {
  const [items, setItems] = useState<Notification[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [unreadOnly, setUnreadOnly] = useState(false);

  async function load(onlyUnread: boolean) {
    setLoading(true);
    try {
      const res = await api.get(`/notifications${onlyUnread ? "?unread=true" : ""}`);
      setItems(res.data.data || []);
    } catch (err: any) {
      setError(err.response?.data?.error || "Failed to load notifications");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load(unreadOnly);
  }, [unreadOnly]);

  async function markRead(id: string) {
    try {
      await api.post(`/notifications/${id}/read`);
      setItems((prev) => (unreadOnly ? prev.filter((n) => n.id !== id) : prev.map((n) => (n.id === id ? { ...n, read: true } : n))));
    } catch (err: any) {
      setError(err.response?.data?.error || "Failed to update notification");
    }
  }

  async function markAllRead() {
    try {
      await api.post("/notifications/read-all");
      setItems((prev) => (unreadOnly ? [] : prev.map((n) => ({ ...n, read: true }))));
    } catch (err: any) {
      setError(err.response?.data?.error || "Failed to update notifications");
    }
  }

  const unreadCount = items.filter((n) => !n.read).length;

  return (
    <>
      <PageHeader
        eyebrow="Workspace"
        title="Notifications"
        description="Low-stock alerts and credit-limit reminders, refreshed automatically."
        action={
          <div className="flex items-center gap-2">
            <button className="btn-secondary" onClick={() => setUnreadOnly((v) => !v)}>
              {unreadOnly ? <Bell size={15} /> : <BellOff size={15} />}
              {unreadOnly ? "Showing unread" : "Show unread only"}
            </button>
            <button className="btn-primary" onClick={markAllRead} disabled={unreadCount === 0}>
              <CheckCheck size={15} /> Mark all read
            </button>
          </div>
        }
      />

      {error && <p className="mb-4 rounded-lg bg-red-50 p-3 text-sm text-red-700">{error}</p>}

      <div className="panel overflow-hidden">
        {loading ? (
          <p className="p-6 text-sm text-[#718078]">Loading notifications...</p>
        ) : items.length === 0 ? (
          <div className="p-12 text-center">
            <Bell className="mx-auto text-[#9aa59d]" />
            <p className="mt-3 font-semibold">You are all caught up</p>
            <p className="mt-1 text-sm text-[#718078]">
              {unreadOnly ? "No unread notifications." : "Nothing needs your attention right now."}
            </p>
          </div>
        ) : (
          <div className="divide-y divide-[#eef1ed]">
            {items.map((n) => {
              const Icon = typeIcon(n.type);
              return (
                <div key={n.id} className={`flex items-start gap-3 p-4 ${n.read ? "opacity-70" : ""}`}>
                  <span className={`mt-0.5 shrink-0 rounded-lg p-2 ${severityStyles[n.severity] || severityStyles.info}`}>
                    <Icon size={16} />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <p className="text-sm font-semibold">{n.title || n.entity_name || "Notification"}</p>
                      {!n.read && <span className="h-2 w-2 rounded-full bg-[#16794c]" />}
                    </div>
                    <p className="mt-0.5 text-sm text-[#718078]">{n.message}</p>
                    <p className="mt-1 text-xs text-[#9aa59d]">{new Date(n.created_at).toLocaleString()}</p>
                  </div>
                  {!n.read && (
                    <button className="btn-secondary py-1.5 text-xs" onClick={() => markRead(n.id)}>
                      Mark read
                    </button>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </>
  );
}
