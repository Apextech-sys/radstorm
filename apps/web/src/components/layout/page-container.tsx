/**
 * Page content container with consistent padding and max-width.
 *
 * Purpose:
 *   Wraps page content with generous padding, maximum width constraint,
 *   and breathing room. Every page uses this for visual consistency.
 *   Handles the "coming in Wave 2" skeleton display when content is pending.
 *
 * Related files:
 *   - src/components/layout/app-shell.tsx (renders this in the main area)
 *   - src/app/page.tsx and all route pages (use this wrapper)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

import React from "react";
import { cn } from "@/lib/utils";

interface PageContainerProps {
  children: React.ReactNode;
  className?: string;
  /** Constrain maximum width (default: true) */
  constrained?: boolean;
}

export function PageContainer({
  children,
  className,
  constrained = true,
}: PageContainerProps) {
  return (
    <div
      className={cn(
        "px-6 py-8 md:px-8 lg:px-10",
        constrained && "mx-auto max-w-5xl",
        className
      )}
    >
      {children}
    </div>
  );
}

interface PageHeaderProps {
  title: string;
  description?: string;
  actions?: React.ReactNode;
}

export function PageHeader({ title, description, actions }: PageHeaderProps) {
  return (
    <div className="mb-8 flex items-start justify-between gap-4">
      <div>
        <h1 className="text-xl font-semibold tracking-tight text-foreground">
          {title}
        </h1>
        {description && (
          <p className="mt-1 text-sm text-muted-foreground">{description}</p>
        )}
      </div>
      {actions && (
        <div className="flex shrink-0 items-center gap-2">{actions}</div>
      )}
    </div>
  );
}

/** "Coming in Wave 2" placeholder shown on skeleton pages */
export function WaveComingPlaceholder({ wave = 2 }: { wave?: number }) {
  return (
    <div className="flex flex-col items-center justify-center rounded-lg border border-dashed border-border bg-muted/30 py-20 text-center">
      <p className="font-mono text-xs text-muted-foreground">
        ⟶ Implementation arriving in Wave {wave}
      </p>
      <p className="mt-2 text-sm text-muted-foreground">
        The layout shell, types, and API client are ready.
      </p>
    </div>
  );
}
