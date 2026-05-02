/**
 * RunsTable — sortable table of historical and active test runs.
 *
 * Purpose:
 *   Renders a polished table of Run records with status badges, relative
 *   timestamps (absolute on hover), duration, and subscriber stats. Clicking
 *   a row navigates to /runs/{id}. Handles empty state and loading skeleton.
 *   Polls the API every 5 seconds to refresh active runs.
 *
 * Related files:
 *   - src/lib/api.ts (listRuns)
 *   - src/lib/types/api.ts (Run type)
 *   - src/components/runs/status-badge.tsx (StatusBadge)
 *   - src/lib/format.ts (fmtRelative, fmtAbsolute, fmtInt, fmtDurationMs)
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
 *
 * Contract: Exports RunsTable component.
 */

"use client";

import { useState, useEffect, useCallback } from "react";
import { useRouter } from "next/navigation";
import { Plus, RefreshCw, Clock } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { StatusBadge } from "@/components/runs/status-badge";
import { LinkButton } from "@/components/ui/link-button";

import { listRuns, ApiError } from "@/lib/api";
import type { Run } from "@/lib/types/api";
import {
  fmtRelative,
  fmtAbsolute,
  fmtInt,
  fmtDurationMs,
  fmtPct,
} from "@/lib/format";
import { cn } from "@/lib/utils";

const POLL_INTERVAL_MS = 5_000;

// ── Column types ─────────────────────────────────────────────────────────────

type SortKey = "created_at" | "status" | "name";
type SortDir = "asc" | "desc";

function sortRuns(runs: Run[], key: SortKey, dir: SortDir): Run[] {
  return [...runs].sort((a, b) => {
    let cmp = 0;
    if (key === "created_at") {
      cmp = a.created_at.localeCompare(b.created_at);
    } else if (key === "status") {
      cmp = a.status.localeCompare(b.status);
    } else {
      const an = a.name ?? a.id;
      const bn = b.name ?? b.id;
      cmp = an.localeCompare(bn);
    }
    return dir === "asc" ? cmp : -cmp;
  });
}

function computeDuration(run: Run): string {
  if (!run.started_at) return "—";
  const startMs = new Date(run.started_at).getTime();
  const endMs = run.finished_at
    ? new Date(run.finished_at).getTime()
    : Date.now();
  return fmtDurationMs(endMs - startMs);
}

// ── Sub-components ────────────────────────────────────────────────────────────

function RelativeTime({ dateStr }: { dateStr: string }) {
  const [label, setLabel] = useState(fmtRelative(dateStr));

  useEffect(() => {
    const id = setInterval(() => setLabel(fmtRelative(dateStr)), 30_000);
    return () => clearInterval(id);
  }, [dateStr]);

  return (
    <span title={fmtAbsolute(dateStr)} className="cursor-default tabular-nums">
      {label}
    </span>
  );
}

function SortHeader({
  label,
  sortKey,
  currentKey,
  currentDir,
  onClick,
}: {
  label: string;
  sortKey: SortKey;
  currentKey: SortKey;
  currentDir: SortDir;
  onClick: (key: SortKey) => void;
}) {
  const active = currentKey === sortKey;
  return (
    <th
      className="h-9 px-4 text-left"
      aria-sort={active ? (currentDir === "asc" ? "ascending" : "descending") : "none"}
    >
      <button
        className={cn(
          "inline-flex items-center gap-1 text-xs font-medium uppercase tracking-wide transition-colors",
          active ? "text-foreground" : "text-muted-foreground hover:text-foreground"
        )}
        onClick={() => onClick(sortKey)}
      >
        {label}
        {active && (
          <span className="text-[10px] text-muted-foreground">
            {currentDir === "asc" ? "↑" : "↓"}
          </span>
        )}
      </button>
    </th>
  );
}

// ── Empty state ────────────────────────────────────────────────────────────────

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center py-20 text-center">
      <div className="mb-3 flex h-10 w-10 items-center justify-center rounded-xl border border-border bg-muted/30">
        <Clock className="h-5 w-5 text-muted-foreground" />
      </div>
      <p className="text-sm font-medium text-foreground">No runs yet</p>
      <p className="mt-1 max-w-xs text-xs text-muted-foreground">
        Configure a scenario and trigger a stress test to see results here.
      </p>
      <LinkButton href="/runs/new" size="sm" className="mt-4 gap-1.5">
        <Plus className="h-3.5 w-3.5" />
        New Run
      </LinkButton>
    </div>
  );
}

// ── Main component ────────────────────────────────────────────────────────────

export function RunsTable() {
  const router = useRouter();
  const [runs, setRuns] = useState<Run[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [sortKey, setSortKey] = useState<SortKey>("created_at");
  const [sortDir, setSortDir] = useState<SortDir>("desc");

  const fetchRuns = useCallback(async () => {
    try {
      const data = await listRuns({ limit: 50 });
      setRuns(data.runs);
      setError(null);
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message);
      } else {
        setError("Failed to load runs");
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const id = setInterval(fetchRuns, POLL_INTERVAL_MS);
    // Trigger initial fetch via timeout to avoid setState-in-effect lint rule
    const initialId = setTimeout(fetchRuns, 0);
    return () => {
      clearInterval(id);
      clearTimeout(initialId);
    };
  }, [fetchRuns]);

  const handleSort = (key: SortKey) => {
    if (key === sortKey) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir("desc");
    }
  };

  const sorted = sortRuns(runs, sortKey, sortDir);

  if (loading) {
    return (
      <div className="space-y-2 rounded-xl border border-border">
        <div className="border-b border-border px-4 py-3">
          <Skeleton className="h-4 w-24" />
        </div>
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className="flex items-center gap-4 px-4 py-3">
            <Skeleton className="h-3 w-40" />
            <Skeleton className="h-5 w-20 rounded-full" />
            <Skeleton className="h-3 w-24" />
            <Skeleton className="h-3 w-16" />
          </div>
        ))}
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center rounded-xl border border-border py-16 text-center">
        <p className="text-sm text-muted-foreground">{error}</p>
        <Button size="sm" variant="outline" className="mt-3 gap-1.5" onClick={fetchRuns}>
          <RefreshCw className="h-3.5 w-3.5" />
          Retry
        </Button>
      </div>
    );
  }

  if (runs.length === 0) {
    return (
      <div className="rounded-xl border border-border">
        <EmptyState />
      </div>
    );
  }

  return (
    <div className="overflow-hidden rounded-xl border border-border">
      <table className="w-full text-sm" role="grid" aria-label="Runs">
        <thead>
          <tr className="border-b border-border bg-muted/30">
            <SortHeader label="Name" sortKey="name" currentKey={sortKey} currentDir={sortDir} onClick={handleSort} />
            <SortHeader label="Status" sortKey="status" currentKey={sortKey} currentDir={sortDir} onClick={handleSort} />
            <SortHeader label="Created" sortKey="created_at" currentKey={sortKey} currentDir={sortDir} onClick={handleSort} />
            <th className="h-9 px-4 text-left text-xs font-medium uppercase tracking-wide text-muted-foreground">Duration</th>
            <th className="h-9 px-4 text-left text-xs font-medium uppercase tracking-wide text-muted-foreground">Established</th>
            <th className="h-9 px-4 text-right text-xs font-medium uppercase tracking-wide text-muted-foreground">p99 latency</th>
          </tr>
        </thead>
        <tbody>
          {sorted.map((run, idx) => {
            const sub = run.summary?.subscribers;
            const est = run.summary?.establishment?.latency_ms;
            const established = sub?.established ?? run.progress?.subscribers_established;
            const total = sub?.total ?? run.progress?.subscribers_total;

            return (
              <tr
                key={run.id}
                className={cn(
                  "cursor-pointer border-b border-border/50 transition-colors hover:bg-muted/40 focus:outline-none focus:bg-muted/40",
                  idx === sorted.length - 1 && "border-b-0"
                )}
                onClick={() => router.push(`/runs/${run.id}`)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" || e.key === " ") {
                    e.preventDefault();
                    router.push(`/runs/${run.id}`);
                  }
                }}
                tabIndex={0}
                role="row"
                aria-label={`Run ${run.name ?? run.id}`}
              >
                <td className="px-4 py-3">
                  <div className="flex flex-col">
                    <span className="font-medium text-foreground">
                      {run.name ?? "Untitled run"}
                    </span>
                    <span className="font-mono text-[10px] text-muted-foreground">
                      {run.id}
                    </span>
                  </div>
                </td>
                <td className="px-4 py-3">
                  <StatusBadge status={run.status} />
                </td>
                <td className="px-4 py-3 text-xs text-muted-foreground">
                  <RelativeTime dateStr={run.created_at} />
                </td>
                <td className="px-4 py-3 font-mono text-xs tabular-nums text-muted-foreground">
                  {computeDuration(run)}
                </td>
                <td className="px-4 py-3">
                  {established !== undefined && total !== undefined ? (
                    <span className="font-mono text-xs tabular-nums">
                      <span className="text-foreground">{fmtInt(established)}</span>
                      <span className="text-muted-foreground"> / {fmtInt(total)}</span>
                      <span className="ml-1 text-muted-foreground">
                        ({fmtPct(established, total)})
                      </span>
                    </span>
                  ) : (
                    <span className="text-muted-foreground">—</span>
                  )}
                </td>
                <td className="px-4 py-3 text-right font-mono text-xs tabular-nums text-muted-foreground">
                  {est?.p99 !== undefined ? (
                    <span>{est.p99.toFixed(0)}ms</span>
                  ) : "—"}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
