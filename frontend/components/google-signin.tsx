"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { api } from "@/lib/api";

type GoogleCredentialResponse = { credential?: string };

interface GoogleAccountsId {
  initialize: (config: {
    client_id: string;
    callback: (response: GoogleCredentialResponse) => void;
  }) => void;
  renderButton: (parent: HTMLElement, options: Record<string, unknown>) => void;
}

interface GoogleGlobal {
  accounts?: { id?: GoogleAccountsId };
}

declare global {
  interface Window {
    google?: GoogleGlobal;
  }
}

interface GoogleSignInProps {
  /** Called when an authenticated Google user has no workspace yet. */
  onWorkspaceRequired?: (credential: string) => void;
  onError?: (message: string) => void;
  text?: "signin_with" | "signup_with" | "continue_with";
  disabled?: boolean;
}

// The GIS script is loaded once in app/layout.tsx. initialize() must run at
// most once per client id: calling it again drops the previous credential
// callback, which makes the flow hang after account selection. A module-level
// guard survives React Strict Mode double-mounts and route changes.
let initializedClientId: string | null = null;
let credentialHandler: ((credential: string) => void) | null = null;

const GIS_LOAD_TIMEOUT_MS = 10000;

export function GoogleSignIn({
  onWorkspaceRequired,
  onError,
  text = "continue_with",
  disabled = false,
}: GoogleSignInProps) {
  const router = useRouter();
  const containerRef = useRef<HTMLDivElement>(null);
  const workspaceRequiredRef = useRef(onWorkspaceRequired);
  const errorRef = useRef(onError);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    workspaceRequiredRef.current = onWorkspaceRequired;
  }, [onWorkspaceRequired]);

  useEffect(() => {
    errorRef.current = onError;
  }, [onError]);

  const handleCredential = useCallback(
    async (credential: string) => {
      setBusy(true);
      try {
        // api's baseURL already includes /api/v1 (NEXT_PUBLIC_API_URL).
        const response = await api.post("/auth/google", { credential });
        localStorage.setItem("token", response.data.data.token);
        router.replace("/dashboard");
      } catch (e: any) {
        const code = e.response?.data?.code;
        if (code === "workspace_required" && workspaceRequiredRef.current) {
          workspaceRequiredRef.current(credential);
        } else {
          errorRef.current?.(
            e.response?.data?.error || "Unable to sign in with Google",
          );
        }
      } finally {
        setBusy(false);
      }
    },
    [router],
  );

  // Keep the single GIS callback pointed at the latest component instance.
  useEffect(() => {
    credentialHandler = handleCredential;
    return () => {
      if (credentialHandler === handleCredential) {
        credentialHandler = null;
      }
    };
  }, [handleCredential]);

  const clientId = process.env.NEXT_PUBLIC_GOOGLE_CLIENT_ID;

  useEffect(() => {
    if (!clientId) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const setup = (): boolean => {
      if (cancelled || !containerRef.current) return false;
      const googleId = window.google?.accounts?.id;
      if (!googleId) return false;

      if (initializedClientId !== clientId) {
        googleId.initialize({
          client_id: clientId,
          callback: (response) => {
            if (response.credential) {
              credentialHandler?.(response.credential);
            } else {
              errorRef.current?.("Google sign-in was cancelled.");
            }
          },
        });
        initializedClientId = clientId;
      }

      containerRef.current.innerHTML = "";
      googleId.renderButton(containerRef.current, {
        type: "standard",
        theme: "outline",
        size: "large",
        text,
        shape: "rectangular",
        width: 320,
        logo_alignment: "left",
      });
      return true;
    };

    // The script tag may not be injected yet (next/script) or may still be
    // loading, so poll briefly instead of waiting on an event that already
    // fired.
    const deadline = Date.now() + GIS_LOAD_TIMEOUT_MS;
    const check = () => {
      if (cancelled) return;
      if (setup()) return;
      if (Date.now() > deadline) {
        errorRef.current?.("Could not load Google sign-in.");
        return;
      }
      timer = setTimeout(check, 100);
    };
    check();

    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [clientId, text]);

  if (!clientId) {
    return (
      <p className="rounded-lg border border-dashed border-[#d9e0d9] p-3 text-center text-xs text-[#8a958d]">
        Google sign-in is not configured.
      </p>
    );
  }

  return (
    <div
      id="google-btn"
      ref={containerRef}
      className={`flex min-h-[40px] justify-center ${
        disabled || busy ? "pointer-events-none opacity-50" : ""
      }`}
    />
  );
}
