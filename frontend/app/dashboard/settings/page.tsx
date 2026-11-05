"use client";

import { useEffect, useState } from "react";
import { UserPlus, Users, Link2, Unlink } from "lucide-react";
import { api } from "@/lib/api";
import { PageHeader } from "@/components/app-shell";
import { GoogleSignIn } from "@/components/google-signin";
import { usePermissions } from "@/hooks/usePermissions";
import { PERMISSIONS } from "@/lib/permissions";
import { ROLES, ROLE_LABELS, roleLabel } from "@/lib/permissions";

type User = {
  id: string;
  name: string;
  email: string;
  role: string;
  active: boolean;
  email_verified: boolean;
  google_linked: boolean;
};

/** Account linking and role-gated team management. */
export default function SettingsPage() {
  const { can } = usePermissions();
  const canReadTeam = can(PERMISSIONS.teamRead);
  const canManageTeam = can(PERMISSIONS.teamManage);
  const [users, setUsers] = useState<User[]>([]);
  const [me, setMe] = useState<User | null>(null);
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState("cashier");
  const [error, setError] = useState("");
  const [teamError, setTeamError] = useState("");
  const [accountNotice, setAccountNotice] = useState("");
  const [unlinking, setUnlinking] = useState(false);
  const [updatingRoleIds, setUpdatingRoleIds] = useState<Set<string>>(new Set());

  /** Loads the signed-in account details. */
  async function loadMe() {
    try {
      const res = await api.get("/auth/me");
      setMe(res.data.data);
    } catch (e: any) {
      setError(e.response?.data?.error || "Failed to load your account");
    }
  }

  /** Loads workspace members for team viewers. */
  async function loadTeam() {
    try {
      const res = await api.get("/users");
      setUsers(res.data.data || []);
      setTeamError("");
    } catch (e: any) {
      setTeamError(e.response?.data?.error || "Only owners can manage users");
    }
  }

  useEffect(() => {
    loadMe();
    if (canReadTeam) loadTeam();
  }, [canReadTeam]);

  /** Creates a team member with the selected role. */
  async function add(e: React.FormEvent) {
    e.preventDefault();
    try {
      await api.post("/users", { name, email, password, role });
      setName("");
      setEmail("");
      setPassword("");
      await loadTeam();
    } catch (e: any) {
      setTeamError(e.response?.data?.error || "Failed to add user");
    }
  }

  /** Changes a member role and tracks that member independently. */
  async function updateRole(userId: string, nextRole: string) {
    setUpdatingRoleIds((current) => new Set(current).add(userId));
    setTeamError("");
    try {
      const res = await api.patch(`/users/${userId}/role`, { role: nextRole });
      setUsers((current) => current.map((user) => (user.id === userId ? { ...user, ...res.data.data } : user)));
    } catch (e: any) {
      setTeamError(e.response?.data?.error || "Failed to update role");
    } finally {
      setUpdatingRoleIds((current) => {
        const next = new Set(current);
        next.delete(userId);
        return next;
      });
    }
  }

  /** Detaches the linked Google identity. */
  async function unlinkGoogle() {
    setUnlinking(true);
    setAccountNotice("");
    try {
      const res = await api.post("/auth/google/unlink", {});
      setMe(res.data.data);
      setAccountNotice("Google disconnected.");
    } catch (e: any) {
      setAccountNotice(
        e.response?.data?.error || "Could not disconnect Google. Try again.",
      );
    } finally {
      setUnlinking(false);
    }
  }

  return (
    <>
      <PageHeader
        eyebrow="Workspace"
        title="Settings"
        description="Manage your account and the people who can access this business workspace."
      />

      <section className="mb-6">
        <div className="panel p-5">
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="eyebrow">Connected accounts</p>
              <h2 className="mt-1 text-lg font-bold">Sign-in methods</h2>
              {me && (
                <p className="mt-1 text-sm text-muted">
                  {me.email} ·{" "}
                  {me.email_verified ? "email verified" : "email not verified"}
                </p>
              )}
            </div>
            <span className="rounded-lg bg-primary-soft p-2.5 text-primary">
              <Link2 size={18} />
            </span>
          </div>

          {accountNotice && (
            <p className="mt-4 rounded-lg bg-hover p-3 text-sm text-muted">
              {accountNotice}
            </p>
          )}

          {error && (
            <p className="mt-4 rounded-lg bg-danger-soft p-3 text-sm text-danger">{error}</p>
          )}

          <div className="mt-5">
            {!me ? null : me.google_linked ? (
              <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-border-subtle p-4">
                <div>
                  <p className="text-sm font-semibold">Google is connected</p>
                  <p className="text-xs text-muted">
                    You can sign in with Google.
                  </p>
                </div>
                <button
                  type="button"
                  onClick={unlinkGoogle}
                  disabled={unlinking}
                  className="btn-secondary py-1.5"
                >
                  <Unlink size={15} /> {unlinking ? "Disconnecting…" : "Disconnect"}
                </button>
              </div>
            ) : (
              <div className="rounded-lg border border-border-subtle p-4">
                <p className="text-sm font-semibold">Connect Google</p>
                <p className="mb-3 text-xs text-muted">
                  Link your Google account to sign in with one tap. We only link it
                  when the Google email matches this account.
                </p>
                <GoogleSignIn
                  mode="link"
                  text="continue_with"
                  onLinked={(user) => {
                    setMe(user as User);
                    setAccountNotice("Google connected.");
                  }}
                  onError={(message) => setAccountNotice(message)}
                  onReady={() => setAccountNotice("")}
                />
              </div>
            )}
          </div>
        </div>
      </section>

      {canReadTeam && <section className="grid gap-6 xl:grid-cols-[.8fr_1.2fr]">
        {canManageTeam && <form onSubmit={add} className="panel space-y-4 p-5">
          <div>
            <p className="eyebrow">Team access</p>
            <h2 className="mt-1 text-lg font-bold">Add a team member</h2>
          </div>
          <input
            className="field"
            placeholder="Full name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
          />
          <input
            className="field"
            type="email"
            placeholder="Email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
          />
          <input
            className="field"
            type="password"
            minLength={8}
            placeholder="Temporary password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
          <select className="field" value={role} onChange={(e) => setRole(e.target.value)}>
            {ROLES.map((option) => (
              <option key={option} value={option}>{ROLE_LABELS[option]}</option>
            ))}
          </select>
          <button className="btn-primary">
            <UserPlus size={16} /> Add member
          </button>
        </form>}

        <div className={`panel overflow-hidden ${canManageTeam ? "" : "xl:col-span-2"}`}>
          <div className="border-b border-border-subtle p-5">
            <p className="eyebrow">Access list</p>
            <h2 className="mt-1 text-lg font-bold">Workspace members</h2>
          </div>
          {teamError && (
            <p className="mx-5 mt-5 rounded-lg bg-danger-soft p-3 text-sm text-danger">{teamError}</p>
          )}
          {users.length === 0 ? (
            <div className="p-8 text-center text-sm text-muted">
              <Users className="mx-auto mb-2" />
              No members found.
            </div>
          ) : (
            <div>
              {users.map((user) => (
                <div
                  key={user.id}
                  className="flex items-center gap-3 border-t border-border-subtle px-5 py-4"
                >
                  <span className="flex h-9 w-9 items-center justify-center rounded-full bg-primary-soft text-sm font-bold text-primary">
                    {user.name.slice(0, 1).toUpperCase()}
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-semibold">{user.name}</p>
                    <p className="text-xs text-muted">{user.email}</p>
                  </div>
                  {canManageTeam ? (
                    <select
                      className="field w-40 py-1.5 text-xs capitalize"
                      value={user.role}
                      disabled={updatingRoleIds.has(user.id)}
                      onChange={(e) => updateRole(user.id, e.target.value)}
                      aria-label={`Role for ${user.name}`}
                    >
                      {ROLES.map((option) => (
                        <option key={option} value={option}>{ROLE_LABELS[option]}</option>
                      ))}
                    </select>
                  ) : (
                    <span className="rounded-full bg-hover px-2.5 py-1 text-xs font-bold text-muted">
                      {roleLabel(user.role)}
                    </span>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      </section>}
    </>
  );
}
