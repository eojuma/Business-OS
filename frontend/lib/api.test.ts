import { describe, expect, it } from "vitest";
import {
  isAuthRequest,
  isCurrentSession,
  shouldRedirectOnUnauthorized,
  shouldThrottleRedirect,
} from "./api";

describe("session expiry handling", () => {
  it("redirects a 401 from a protected endpoint", () => {
    expect(
      shouldRedirectOnUnauthorized(401, "/assistant/interpret", "/dashboard/assistant"),
    ).toBe(true);
  });

  it("ignores non-401 responses", () => {
    expect(shouldRedirectOnUnauthorized(400, "/assistant/interpret", "/dashboard")).toBe(false);
    expect(shouldRedirectOnUnauthorized(undefined, "/assistant/interpret", "/dashboard")).toBe(false);
  });

  it("lets auth endpoints report their own errors", () => {
    for (const url of ["/auth/login", "/auth/register", "/auth/google"]) {
      expect(shouldRedirectOnUnauthorized(401, url, "/dashboard")).toBe(false);
    }
  });

  it("does not redirect while already on an auth page", () => {
    expect(shouldRedirectOnUnauthorized(401, "/assistant/interpret", "/login")).toBe(false);
    expect(shouldRedirectOnUnauthorized(401, "/assistant/interpret", "/signup")).toBe(false);
    expect(shouldRedirectOnUnauthorized(401, "/assistant/interpret", "/login/nested")).toBe(false);
  });

  it("still redirects from a non-auth page that merely starts like one", () => {
    expect(shouldRedirectOnUnauthorized(401, "/assistant/interpret", "/logins")).toBe(true);
  });

  it("detects auth request urls by path", () => {
    expect(isAuthRequest("/auth/login")).toBe(true);
    expect(isAuthRequest("/auth/login?next=/dashboard")).toBe(true);
    expect(isAuthRequest("https://api.example.com/api/v1/auth/google")).toBe(true);
    expect(isAuthRequest("/assistant/interpret")).toBe(false);
    expect(isAuthRequest(undefined)).toBe(false);
  });

  it("does not treat protected endpoints under an auth prefix as auth calls", () => {
    expect(isAuthRequest("/auth/google/unlink")).toBe(false);
    expect(isAuthRequest("/auth/login-help")).toBe(false);
    expect(shouldRedirectOnUnauthorized(401, "/auth/google/unlink", "/dashboard")).toBe(true);
  });
});

describe("redirect cooldown", () => {
  it("does not throttle before the first redirect", () => {
    expect(shouldThrottleRedirect(0, 1000)).toBe(false);
  });

  it("collapses redirects inside the cooldown window", () => {
    expect(shouldThrottleRedirect(1000, 1500)).toBe(true);
  });

  it("allows a new redirect once the window has passed", () => {
    expect(shouldThrottleRedirect(1000, 5000)).toBe(false);
  });
});

describe("stale unauthorized responses", () => {
  it("treats a 401 from the current token as actionable", () => {
    expect(isCurrentSession("Bearer token-a", "token-a")).toBe(true);
  });

  it("ignores a 401 from a token that has since been replaced", () => {
    expect(isCurrentSession("Bearer token-a", "token-b")).toBe(false);
  });

  it("keeps the redirect path when the stored token is unavailable", () => {
    expect(isCurrentSession("Bearer token-a", null)).toBe(true);
    expect(isCurrentSession(undefined, null)).toBe(true);
  });

  it("keeps the redirect path when the failed request carried no header", () => {
    expect(isCurrentSession(undefined, "token-a")).toBe(true);
  });
});
