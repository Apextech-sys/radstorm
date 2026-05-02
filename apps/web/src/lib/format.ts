/**
 * Formatting utilities for the radstorm frontend.
 *
 * Purpose:
 *   Provides helpers for formatting numbers, durations, relative timestamps,
 *   and large subscriber counts for display across the UI. Uses date-fns for
 *   relative time and native Intl for number formatting.
 *
 * Related files:
 *   - src/app/runs/page.tsx (relative timestamps in run list)
 *   - src/app/runs/[id]/run-detail-client.tsx (elapsed time, counters)
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
 *
 * Contract: Pure utility functions — no React imports.
 */

import { formatDistanceToNow, format as formatDate } from "date-fns";

// ── Number formatting ─────────────────────────────────────────────────────────

const compact = new Intl.NumberFormat("en", {
  notation: "compact",
  maximumFractionDigits: 1,
});

const full = new Intl.NumberFormat("en");

/** Format a large integer compactly: 1234 → "1.2K", 1000000 → "1M" */
export function fmtCompact(n: number): string {
  return compact.format(n);
}

/** Format an integer with thousand separators: 12345 → "12,345" */
export function fmtInt(n: number): string {
  return full.format(Math.round(n));
}

/** Format a percentage to 1 decimal: 0.978 → "97.8%" */
export function fmtPct(value: number, total: number): string {
  if (total === 0) return "0%";
  return `${((value / total) * 100).toFixed(1)}%`;
}

// ── Duration formatting ───────────────────────────────────────────────────────

/** Format milliseconds as a human duration: 62500 → "1m 2.5s" */
export function fmtDurationMs(ms: number): string {
  if (ms < 1000) return `${ms.toFixed(0)}ms`;
  const totalSec = ms / 1000;
  if (totalSec < 60) return `${totalSec.toFixed(1)}s`;
  const m = Math.floor(totalSec / 60);
  const s = totalSec % 60;
  return `${m}m ${s.toFixed(0)}s`;
}

/** Format latency in ms to a display string with unit */
export function fmtLatency(ms: number): string {
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)}s`;
  return `${ms.toFixed(0)}ms`;
}

// ── Timestamp formatting ──────────────────────────────────────────────────────

/** Relative time from now: "3 minutes ago", "just now" */
export function fmtRelative(dateStr: string): string {
  try {
    const d = new Date(dateStr);
    if (isNaN(d.getTime())) return "—";
    return formatDistanceToNow(d, { addSuffix: true });
  } catch {
    return "—";
  }
}

/** Absolute datetime for tooltip: "2026-05-02 01:23:45 UTC" */
export function fmtAbsolute(dateStr: string): string {
  try {
    const d = new Date(dateStr);
    if (isNaN(d.getTime())) return "—";
    return formatDate(d, "yyyy-MM-dd HH:mm:ss 'UTC'");
  } catch {
    return "—";
  }
}

// ── Subscriber count presets ──────────────────────────────────────────────────

export const SUBSCRIBER_PRESETS = [
  { label: "100", value: 100 },
  { label: "1K", value: 1_000 },
  { label: "10K", value: 10_000 },
  { label: "100K", value: 100_000 },
  { label: "1M", value: 1_000_000 },
] as const;

/** Display label for a subscriber count preset */
export function subscriberLabel(count: number): string {
  if (count >= 1_000_000) return `${count / 1_000_000}M`;
  if (count >= 1_000) return `${count / 1_000}K`;
  return String(count);
}
