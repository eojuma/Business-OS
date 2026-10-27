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
  /** Called when the button is ready, e.g. so pages can clear stale errors. */
  onReady?: () => void;
  text?: "signin_with" | "signup_with" | "continue_with";
  disabled?: boolean;
}

// The GIS script is loaded once in app/layout.tsx. initialize() must run at
// most once per client id: calling it again drops the previous credential
// callback, which makes the flow hang after account selection. A module-level
// guard survives React Strict Mode double-mounts and route changes.
let initializedClientId: string | null = null;
let credentialHandler: ((credential: string) => void) | null = null;
let credentialErrorHandler: ((message: string) => void) | null = null;

const GIS_SCRIPT_SRC = "https://accounts.google.com/gsi/client";
const GIS_LOAD_TIMEOUT_MS = 10000;

// The layout loads GIS once. If that load fails (offline, content blocker), the
// tag is inert and polling can never recover, so a retry drops the stale tag
// and injects a fresh one.
function reloadGoogleScript() {
  document
    .querySelectorAll(`script[src="${GIS_SCRIPT_SRC}"]`)
    .forEach((element) => element.remove());

  const script = document.createElement("script");
  script.id = "google-identity-services-retry";
  script.src = GIS_SCRIPT_SRC;
  script.async = true;
  script.defer = true;
  script.onerror = () => script.remove();
  document.head.appendChild(script);
}

export function GoogleSignIn({
  onWorkspaceRequired,
  onError,
  onReady,
  text = "continue_with",
  disabled = false,
}: GoogleSignInProps) {
  const router = useRouter();
  const containerRef = useRef<HTMLDivElement>(null);
  const workspaceRequiredRef = useRef(onWorkspaceRequired);
  const errorRef = useRef(onError);
  const readyRef = useRef(onReady);
  const [busy, setBusy] = useState(false);
  const [timedOut, setTimedOut] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    workspaceRequiredRef.current = onWorkspaceRequired;
  }, [onWorkspaceRequired]);

  useEffect(() => {
    errorRef.current = onError;
  }, [onError]);

  useEffect(() => {
    readyRef.current = onReady;
  }, [onReady]);

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
  // Errors go through the module too, otherwise a cancelled sign-in after a
  // route change would report to the unmounted page's state.
  useEffect(() => {
    const reportError = (message: string) => errorRef.current?.(message);
    credentialHandler = handleCredential;
    credentialErrorHandler = reportError;
    return () => {
      if (credentialHandler === handleCredential) {
        credentialHandler = null;
      }
      if (credentialErrorHandler === reportError) {
        credentialErrorHandler = null;
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
              credentialErrorHandler?.("Google sign-in was cancelled.");
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
      readyRef.current?.();
      return true;
    };

    // A layout-injected script that fails leaves an inert tag behind; drop it
    // so a retry can inject a working one.
    const layoutScript = document.querySelector<HTMLScriptElement>(
      `script[src="${GIS_SCRIPT_SRC}"]`,
    );
    const onLayoutScriptError = () => layoutScript?.remove();
    if (layoutScript && !window.google?.accounts?.id) {
      layoutScript.addEventListener("error", onLayoutScriptError);
    }

    // The script tag may not be injected yet (next/script) or may still be
    // loading, so poll briefly instead of waiting on an event that already
    // fired.
    const deadline = Date.now() + GIS_LOAD_TIMEOUT_MS;
    const check = () => {
      if (cancelled) return;
      if (setup()) return;
      if (Date.now() > deadline) {
        setTimedOut(true);
        errorRef.current?.("Could not load Google sign-in.");
        return;
      }
      timer = setTimeout(check, 100);
    };
    check();

    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
      layoutScript?.removeEventListener("error", onLayoutScriptError);
    };
  }, [clientId, text, attempt]);

  if (!clientId) {
    return (
      <p className="rounded-lg border border-dashed border-[#d9e0d9] p-3 text-center text-xs text-[#8a958d]">
        Google sign-in is not configured.
      </p>
    );
  }

  return (
    <>
      <div
        id="google-btn"
        ref={containerRef}
        className={`flex min-h-[40px] justify-center ${
          disabled || busy ? "pointer-events-none opacity-50" : ""
        }`}
      />
      {timedOut && (
        <button
          type="button"
          onClick={() => {
            setTimedOut(false);
            if (!window.google?.accounts?.id) {
              reloadGoogleScript();
            }
            setAttempt((n) => n + 1);
          }}
          className="mt-2 w-full text-center text-xs text-[#8a958d] underline"
        >
          Retry Google sign-in
        </button>
      )}
    </>
  );
}
