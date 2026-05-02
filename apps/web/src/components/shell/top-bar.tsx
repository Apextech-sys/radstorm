/**
 * Top bar component for the radstorm app shell.
 *
 * Purpose:
 *   Horizontal top bar containing the sidebar toggle button (mobile),
 *   current page breadcrumb area, API status indicator, and theme toggle.
 *   Rendered inside the main layout shell above the page content area.
 *
 * Related files:
 *   - src/components/shell/sidebar-nav.tsx (sidebar this controls)
 *   - src/components/shell/api-status.tsx (API health indicator)
 *   - src/components/shell/theme-toggle.tsx (theme switcher)
 *   - src/components/layout/app-shell.tsx (parent shell)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

import React from "react";
import { Separator } from "@/components/ui/separator";
import { ApiStatus } from "./api-status";
import { ThemeToggle } from "./theme-toggle";

interface TopBarProps {
  /** Optional breadcrumb or page title to display */
  title?: string;
}

export function TopBar({ title }: TopBarProps) {
  return (
    <header className="flex h-12 shrink-0 items-center gap-3 border-b border-border px-4">
      {/* Page title / breadcrumb */}
      <div className="flex flex-1 items-center gap-2">
        {title && (
          <span className="text-sm font-medium text-foreground">{title}</span>
        )}
      </div>

      {/* Right side actions */}
      <div className="flex items-center gap-3">
        <ApiStatus />
        <Separator orientation="vertical" className="h-4" />
        <ThemeToggle />
      </div>
    </header>
  );
}
