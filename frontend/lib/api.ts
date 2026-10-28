import axios from "axios";

const AUTH_ROUTES = ["/auth/login", "/auth/register", "/auth/google"];
const AUTH_PAGES = ["/login", "/signup"];

export const SESSION_EXPIRED_URL = "/login?expired=1";

/**
 * True for endpoints where a 401 means bad credentials, not an expired
 * session. Matching is done on the URL path so a protected endpoint such as
 * `/auth/google/unlink`, or a query string mentioning an auth route, is not
 * mistaken for the auth call itself.
 */
export function isAuthRequest(url?: string): boolean {
  if (!url) return false;

  let pathname: string;
  try {
    pathname = new URL(url, "http://localhost").pathname;
  } catch {
    pathname = url.split(/[?#]/)[0];
  }
  const normalized = pathname.replace(/\/+$/, "");
  return AUTH_ROUTES.some((route) => normalized === route || normalized.endsWith(route));
}

/**
 * A 401 on a protected request must send the user back to sign in. Auth
 * endpoints surface their own errors inline, and redirecting while already on
 * an auth page would loop.
 */
export function shouldRedirectOnUnauthorized(
  status: number | undefined,
  url: string | undefined,
  pathname: string,
): boolean {
  if (status !== 401) return false;
  if (isAuthRequest(url)) return false;
  return !AUTH_PAGES.some((page) => pathname === page || pathname.startsWith(`${page}/`));
}

/**
 * A 401 is only actionable if it came from the token currently in use. A late
 * response from an earlier token (for example a request in flight while the
 * user signs in again) must not clear the newer session. When the stored token
 * cannot be read, keep the redirect path: an expired session is the likelier
 * explanation than a race.
 */
export function isCurrentSession(
  failedAuth: string | undefined,
  currentToken: string | null,
): boolean {
  if (!currentToken || !failedAuth) return true;
  return failedAuth === `Bearer ${currentToken}`;
}

const REDIRECT_COOLDOWN_MS = 3000;
let lastRedirectAt = 0;

/** Collapses a burst of parallel 401s into one navigation without sticking. */
export function shouldThrottleRedirect(
  lastRedirectAt: number,
  now: number,
  cooldownMs = REDIRECT_COOLDOWN_MS,
): boolean {
  return lastRedirectAt !== 0 && now - lastRedirectAt < cooldownMs;
}

function handleUnauthorized(failedAuth: string | undefined): void {
  if (typeof window === "undefined") return;
  const now = Date.now();
  if (shouldThrottleRedirect(lastRedirectAt, now)) return;

  let currentToken: string | null = null;
  try {
    currentToken = window.localStorage.getItem("token");
  } catch {
    // Storage can be unavailable; fall through to the redirect below.
  }
  if (!isCurrentSession(failedAuth, currentToken)) return;

  // A cooldown rather than a permanent flag: it collapses parallel 401s but
  // still allows a later attempt if the navigation is blocked or the page is
  // restored from the back/forward cache.
  lastRedirectAt = now;
  try {
    window.localStorage.removeItem("token");
  } catch {
    // Storage can be unavailable; the redirect below still applies.
  }
  window.location.assign(SESSION_EXPIRED_URL);
}

export const api = axios.create({
  baseURL: process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080/api/v1",
  headers: {
    "Content-Type": "application/json",
  },
});

api.interceptors.request.use((config) => {
  if (typeof window !== "undefined") {
    const token = localStorage.getItem("token");
    if (token) {
      config.headers.Authorization = `Bearer ${token}`;
    }
  }
  return config;
});

api.interceptors.response.use(
  (response) => response,
  (error: unknown) => {
    const details = error as {
      config?: { url?: string; headers?: { Authorization?: string } };
      response?: { status?: number };
    };
    if (
      typeof window !== "undefined" &&
      shouldRedirectOnUnauthorized(
        details.response?.status,
        details.config?.url,
        window.location.pathname,
      )
    ) {
      handleUnauthorized(details.config?.headers?.Authorization);
    }
    return Promise.reject(error);
  },
);
