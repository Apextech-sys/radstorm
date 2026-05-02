/**
 * StatusBadge — visual badge for run status values.
 *
 * Purpose:
 *   Renders a color-coded badge for each RunStatus value. Running state
 *   includes an animated pulse indicator. Used in the runs list table and
 *   run detail header.
 *
 * Related files:
 *   - src/lib/types/api.ts (RunStatus enum)
 *   - src/app/runs/page.tsx (runs list consumer)
 *   - src/app/runs/[id]/run-detail-client.tsx (header consumer)
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
 *
 * Contract: Exports StatusBadge component.
 */

"use client";

import { cn } from "@/lib/utils";
import type { RunStatus } from "@/lib/types/api";

interface StatusBadgeProps {
  status: RunStatus;
  className?: string;
}

const STATUS_CONFIG: Record<
  RunStatus,
  { label: string; className: string; dot?: string }
> = {
  queued: {
    label: "Queued",
    className:
      "bg-muted text-muted-foreground border-border",
  },
  running: {
    label: "Running",
    className:
      "bg-signal-muted text-signal border-signal/30",
    dot: "bg-signal animate-pulse",
  },
  succeeded: {
    label: "Succeeded",
    className:
      "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400 border-emerald-500/20",
  },
  failed: {
    label: "Failed",
    className:
      "bg-destructive/10 text-destructive border-destructive/20",
  },
  cancelling: {
    label: "Cancelling",
    className:
      "bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/20",
  },
  cancelled: {
    label: "Cancelled",
    className:
      "bg-muted text-muted-foreground border-border",
  },
};

export function StatusBadge({ status, className }: StatusBadgeProps) {
  const cfg = STATUS_CONFIG[status] ?? STATUS_CONFIG.queued;

  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs font-medium tabular-nums",
        cfg.className,
        className
      )}
    >
      {cfg.dot && (
        <span
          className={cn("inline-block h-1.5 w-1.5 rounded-full", cfg.dot)}
          aria-hidden="true"
        />
      )}
      {cfg.label}
    </span>
  );
}
