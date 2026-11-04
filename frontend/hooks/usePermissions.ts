"use client";

import { useAuth } from "@/hooks/useAuth";
import { can as roleCan, type Permission } from "@/lib/permissions";

/** Exposes the signed-in user's role and permission checks to dashboard UI. */
export function usePermissions() {
  const { user, loading } = useAuth();
  const role = user?.role ?? null;

  return {
    role,
    loading,
    can: (permission: Permission) => roleCan(role, permission),
  };
}
