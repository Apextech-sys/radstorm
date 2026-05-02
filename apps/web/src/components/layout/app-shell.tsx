/**
 * Main application shell layout component.
 *
 * Purpose:
 *   Provides the full-page layout frame: sidebar navigation (left) + top bar
 *   + scrollable main content area. All pages render inside this shell.
 *   Uses a flex-row layout that fills the viewport height.
 *
 * Related files:
 *   - src/app/layout.tsx (uses this as the root layout wrapper)
 *   - src/components/shell/sidebar-nav.tsx (sidebar component)
 *   - src/components/shell/top-bar.tsx (top bar component)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

import React from "react";
import { SidebarNav } from "@/components/shell/sidebar-nav";
import { TopBar } from "@/components/shell/top-bar";

interface AppShellProps {
  children: React.ReactNode;
  title?: string;
}

export function AppShell({ children, title }: AppShellProps) {
  return (
    <div className="flex h-screen w-full overflow-hidden bg-background">
      {/* Sidebar */}
      <SidebarNav />

      {/* Content area */}
      <div className="flex flex-1 flex-col overflow-hidden">
        <TopBar title={title} />
        <main
          className="flex-1 overflow-auto"
          id="main-content"
          tabIndex={-1}
        >
          {children}
        </main>
      </div>
    </div>
  );
}
