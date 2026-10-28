import { describe, expect, it } from "vitest";
import { isAuthRequest, shouldRedirectOnUnauthorized } from "./api";

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

  it("detects auth request urls", () => {
    expect(isAuthRequest("/auth/login")).toBe(true);
    expect(isAuthRequest("https://api.example.com/api/v1/auth/google")).toBe(true);
    expect(isAuthRequest("/assistant/interpret")).toBe(false);
    expect(isAuthRequest(undefined)).toBe(false);
  });
});
