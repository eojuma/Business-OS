// @vitest-environment jsdom
import type { ReactNode } from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { navStateKey } from "@/lib/navigation";

const mocks = vi.hoisted(() => ({
  pathname: "/dashboard",
  push: vi.fn(),
  get: vi.fn(),
}));

vi.mock("next/navigation", () => ({
  usePathname: () => mocks.pathname,
  useRouter: () => ({ push: mocks.push }),
}));

vi.mock("next/link", () => ({
  default: ({
    href,
    children,
    ...props
  }: { href: unknown; children?: ReactNode } & Record<string, unknown>) => (
    <a href={String(href)} {...props}>
      {children}
    </a>
  ),
}));

vi.mock("@/lib/api", () => ({ api: { get: mocks.get } }));
vi.mock("@/components/theme-toggle", () => ({ ThemeToggle: () => null }));

import { AppShell } from "@/components/app-shell";

function shell() {
  return (
    <AppShell>
      <div>Dashboard content</div>
    </AppShell>
  );
}

function renderShell() {
  return render(shell());
}

function groupButton(name: string) {
  return screen.getByRole("button", { name });
}

function groupPanel(button: HTMLElement) {
  const panelId = button.getAttribute("aria-controls");
  return panelId ? document.getElementById(panelId) : null;
}

beforeEach(() => {
  mocks.pathname = "/dashboard";
  mocks.push.mockReset();
  mocks.get.mockReset().mockResolvedValue({ data: { data: { unread: 0 } } });
  window.localStorage.clear();
  window.innerWidth = 1024;
});

afterEach(cleanup);

describe("grouped navigation", () => {
  it("collapses and expands a group through its ARIA control", () => {
    renderShell();
    const button = groupButton("Finance");
    const panel = groupPanel(button);

    expect(button.getAttribute("aria-expanded")).toBe("true");
    expect(panel?.hidden).toBe(false);

    fireEvent.click(button);

    expect(button.getAttribute("aria-expanded")).toBe("false");
    expect(panel?.hidden).toBe(true);
  });

  it("supports keyboard toggling with Enter and Space", async () => {
    const user = userEvent.setup();
    renderShell();
    const button = groupButton("Finance");
    button.focus();

    await user.keyboard("{Enter}");
    expect(button.getAttribute("aria-expanded")).toBe("false");

    await user.keyboard(" ");
    expect(button.getAttribute("aria-expanded")).toBe("true");
  });

  it("expands the active group on a direct load even when saved collapsed", async () => {
    const token = `header.${Buffer.from(
      JSON.stringify({ user_id: "user-1" }),
    ).toString("base64url")}.signature`;
    window.localStorage.setItem("token", token);
    window.localStorage.setItem(
      navStateKey("user-1"),
      JSON.stringify(["finance", "people"]),
    );
    mocks.pathname = "/dashboard/finance";

    renderShell();

    await waitFor(() =>
      expect(groupButton("Finance").getAttribute("aria-expanded")).toBe("true"),
    );
    expect(groupButton("People").getAttribute("aria-expanded")).toBe("false");
  });

  it("keeps a saved collapse preference when revealing the active group", async () => {
    const token = `header.${Buffer.from(
      JSON.stringify({ user_id: "user-1" }),
    ).toString("base64url")}.signature`;
    window.localStorage.setItem("token", token);
    window.localStorage.setItem(navStateKey("user-1"), JSON.stringify(["finance"]));
    const view = renderShell();

    await waitFor(() =>
      expect(groupButton("Finance").getAttribute("aria-expanded")).toBe("false"),
    );

    mocks.pathname = "/dashboard/finance";
    view.rerender(shell());

    await waitFor(() =>
      expect(groupButton("Finance").getAttribute("aria-expanded")).toBe("true"),
    );
    expect(JSON.parse(window.localStorage.getItem(navStateKey("user-1")) ?? "[]")).toEqual([
      "finance",
    ]);

    mocks.pathname = "/dashboard";
    view.rerender(shell());

    await waitFor(() =>
      expect(groupButton("Finance").getAttribute("aria-expanded")).toBe("false"),
    );
  });
});

describe("mobile drawer accessibility", () => {
  it("moves focus back to the menu button when Escape closes it", () => {
    window.innerWidth = 500;
    renderShell();
    const openButton = screen.getByRole("button", { name: "Open navigation" });

    fireEvent.click(openButton);
    expect(openButton.getAttribute("aria-expanded")).toBe("true");

    fireEvent.keyDown(window, { key: "Escape" });

    expect(openButton.getAttribute("aria-expanded")).toBe("false");
    expect(document.activeElement).toBe(openButton);
  });

  it("moves focus back to the menu button when the close button is used", () => {
    window.innerWidth = 500;
    renderShell();
    const openButton = screen.getByRole("button", { name: "Open navigation" });

    fireEvent.click(openButton);
    const closeButton = screen.getByRole("button", { name: "Close navigation" });
    closeButton.focus();
    fireEvent.click(closeButton);

    expect(document.activeElement).toBe(openButton);
  });

  it("moves focus back to the menu button when the overlay is used", () => {
    window.innerWidth = 500;
    renderShell();
    const openButton = screen.getByRole("button", { name: "Open navigation" });

    fireEvent.click(openButton);
    fireEvent.click(screen.getByRole("button", { name: "Close navigation overlay" }));

    expect(document.activeElement).toBe(openButton);
  });

  it("makes the closed mobile drawer inert and restores it when open", async () => {
    window.innerWidth = 500;
    renderShell();
    const aside = document.getElementById("workspace-navigation");

    expect(aside).not.toBeNull();
    await waitFor(() => expect(aside?.hasAttribute("inert")).toBe(true));

    fireEvent.click(screen.getByRole("button", { name: "Open navigation" }));
    await waitFor(() => expect(aside?.hasAttribute("inert")).toBe(false));
  });

  it("leaves the desktop sidebar interactive", async () => {
    window.innerWidth = 1024;
    renderShell();
    const aside = document.getElementById("workspace-navigation");

    expect(aside).not.toBeNull();
    await waitFor(() => expect(aside?.hasAttribute("inert")).toBe(false));
  });
});
