import axios from "axios";

const AUTH_ROUTES = ["/auth/login", "/auth/register", "/auth/google"];
const AUTH_PAGES = ["/login", "/signup"];

export const SESSION_EXPIRED_URL = "/login?expired=1";

/** True for endpoints where a 401 means bad credentials, not an expired session. */
export function isAuthRequest(url?: string): boolean {
  if (!url) return false;
  return AUTH_ROUTES.some((route) => url.includes(route));
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

let redirecting = false;

function handleUnauthorized(): void {
  if (typeof window === "undefined" || redirecting) return;
  // Guard against a burst of parallel 401s triggering several navigations.
  redirecting = true;
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
      config?: { url?: string };
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
      handleUnauthorized();
    }
    return Promise.reject(error);
  },
);
