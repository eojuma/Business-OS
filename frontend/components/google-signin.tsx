"use client";

import { useEffect, useRef } from "react";

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
  onCredential: (credential: string) => void;
  onError?: (message: string) => void;
  text?: "signin_with" | "signup_with" | "continue_with";
  disabled?: boolean;
}

const SCRIPT_ID = "google-identity-services";

export function GoogleSignIn({
  onCredential,
  onError,
  text = "continue_with",
  disabled = false,
}: GoogleSignInProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const credentialRef = useRef(onCredential);
  const errorRef = useRef(onError);

  useEffect(() => {
    credentialRef.current = onCredential;
  }, [onCredential]);

  useEffect(() => {
    errorRef.current = onError;
  }, [onError]);

  const clientId = process.env.NEXT_PUBLIC_GOOGLE_CLIENT_ID;

  useEffect(() => {
    if (!clientId) return;
    let cancelled = false;
    let script: HTMLScriptElement | null = null;

    const render = () => {
      if (cancelled || !containerRef.current) return;
      const googleId = window.google?.accounts?.id;
      if (!googleId) {
        errorRef.current?.("Google sign-in could not be loaded.");
        return;
      }

      googleId.initialize({
        client_id: clientId,
        callback: (response) => {
          if (response.credential) {
            credentialRef.current(response.credential);
          } else {
            errorRef.current?.("Google sign-in was cancelled.");
          }
        },
      });

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
    };

    // Always drop a script that failed to load, even if this mount has already
    // been unmounted. Otherwise a later mount would attach a load listener to
    // an element that never fires again and the button would stay blank.
    const reportLoadFailure = () => {
      script?.remove();
      if (!cancelled) {
        errorRef.current?.("Could not load Google sign-in.");
      }
    };

    const existing = document.getElementById(SCRIPT_ID) as HTMLScriptElement | null;
    if (existing) {
      if (window.google?.accounts?.id) {
        render();
        return () => {
          cancelled = true;
        };
      }
      script = existing;
      const onLoad = () => {
        if (!cancelled) render();
      };
      existing.addEventListener("load", onLoad);
      existing.addEventListener("error", reportLoadFailure);
      return () => {
        cancelled = true;
        existing.removeEventListener("load", onLoad);
        existing.removeEventListener("error", reportLoadFailure);
      };
    }

    script = document.createElement("script");
    script.id = SCRIPT_ID;
    script.src = "https://accounts.google.com/gsi/client";
    script.async = true;
    script.defer = true;
    script.onload = () => {
      if (!cancelled) render();
    };
    script.onerror = reportLoadFailure;
    document.head.appendChild(script);

    return () => {
      cancelled = true;
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
      ref={containerRef}
      className={`flex min-h-[40px] justify-center ${disabled ? "pointer-events-none opacity-50" : ""}`}
    />
  );
}
