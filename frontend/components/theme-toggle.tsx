"use client";

import { Monitor, Moon, Sun, type LucideIcon } from "lucide-react";
import clsx from "clsx";
import { useTheme, type Theme } from "@/lib/theme-provider";

const options: { value: Theme; label: string; icon: LucideIcon }[] = [
  { value: "light", label: "Light", icon: Sun },
  { value: "dark", label: "Dark", icon: Moon },
  { value: "system", label: "System", icon: Monitor },
];

/**
 * Three-way theme control (light / dark / system). Rendered as a segmented
 * group so each mode is visible, keyboard reachable and announced with
 * aria-pressed.
 */
export function ThemeToggle({ className }: { className?: string }) {
  const { theme, setTheme } = useTheme();

  return (
    <div
      role="group"
      aria-label="Color theme"
      className={clsx(
        "inline-flex items-center gap-1 rounded-lg border border-border bg-card p-1",
        className,
      )}
    >
      {options.map(({ value, label, icon: Icon }) => {
        const active = theme === value;
        return (
          <button
            key={value}
            type="button"
            onClick={() => setTheme(value)}
            aria-label={`Use ${label.toLowerCase()} theme`}
            aria-pressed={active}
            title={`${label} theme`}
            className={clsx(
              "inline-flex items-center justify-center rounded-md p-1.5 transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40",
              active
                ? "bg-primary-soft text-primary"
                : "text-muted hover:bg-hover hover:text-foreground",
            )}
          >
            <Icon size={15} aria-hidden="true" />
          </button>
        );
      })}
    </div>
  );
}
