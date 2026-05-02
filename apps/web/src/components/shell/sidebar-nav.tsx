/**
 * Sidebar navigation for the radstorm app shell.
 *
 * Purpose:
 *   Left sidebar containing logo/wordmark, nav links (Dashboard, New Run,
 *   Runs, Settings), and active link highlighting. Uses Next.js Link and
 *   usePathname for client-side active state. Clean, minimal data-tool
 *   aesthetic with the electric green signal accent on active items.
 *
 * Related files:
 *   - src/components/layout/app-shell.tsx (parent layout)
 *   - src/components/shell/top-bar.tsx (top bar sibling)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

"use client";

import React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  LayoutDashboard,
  Plus,
  List,
  Settings,
  Zap,
} from "lucide-react";
import { cn } from "@/lib/utils";

interface NavItem {
  href: string;
  label: string;
  icon: React.ElementType;
  exact?: boolean;
}

const NAV_ITEMS: NavItem[] = [
  {
    href: "/",
    label: "Dashboard",
    icon: LayoutDashboard,
    exact: true,
  },
  {
    href: "/runs/new",
    label: "New Run",
    icon: Plus,
  },
  {
    href: "/runs",
    label: "Runs",
    icon: List,
  },
  {
    href: "/settings",
    label: "Settings",
    icon: Settings,
  },
];

export function SidebarNav() {
  const pathname = usePathname();

  function isActive(item: NavItem): boolean {
    if (item.exact) return pathname === item.href;
    return pathname === item.href || pathname.startsWith(item.href + "/");
  }

  return (
    <nav
      className="flex h-full w-56 shrink-0 flex-col border-r border-border bg-sidebar"
      aria-label="Main navigation"
    >
      {/* Wordmark */}
      <div className="flex h-12 items-center gap-2 border-b border-sidebar-border px-4">
        <span className="text-signal" aria-hidden="true">
          <Zap className="h-4 w-4 fill-current" />
        </span>
        <span className="text-sm font-semibold tracking-tight text-sidebar-foreground">
          radstorm
        </span>
        <span className="ml-auto font-mono text-[10px] text-muted-foreground">
          v0.1
        </span>
      </div>

      {/* Nav links */}
      <div className="flex flex-1 flex-col gap-0.5 p-2 pt-3">
        <p className="mb-1 px-2 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
          Navigation
        </p>
        {NAV_ITEMS.map((item) => {
          const active = isActive(item);
          const Icon = item.icon;
          return (
            <Link
              key={item.href}
              href={item.href}
              className={cn(
                "flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm transition-colors",
                "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sidebar-ring",
                active
                  ? "bg-sidebar-accent text-sidebar-accent-foreground font-medium"
                  : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
              )}
              aria-current={active ? "page" : undefined}
            >
              <Icon
                className={cn(
                  "h-4 w-4 shrink-0",
                  active ? "text-signal" : "text-muted-foreground"
                )}
              />
              {item.label}
              {active && (
                <span className="ml-auto h-1 w-1 rounded-full bg-signal" aria-hidden="true" />
              )}
            </Link>
          );
        })}
      </div>

      {/* Footer */}
      <div className="border-t border-sidebar-border p-3">
        <p className="font-mono text-[10px] text-muted-foreground">
          ISP RADIUS stress tester
        </p>
      </div>
    </nav>
  );
}
