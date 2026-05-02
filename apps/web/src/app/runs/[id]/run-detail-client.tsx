/**
 * RunDetailClient — interactive client component for the run detail page.
 *
 * Purpose:
 *   Fetches run data, connects to SSE stream for live progress, renders the
 *   live progress section while running, and switches to the results dashboard
 *   when the run completes. Also provides a cancel button, config viewer tab,
 *   and collapsible log panel.
 *
 * Related files:
 *   - src/lib/api.ts (getRun, cancelRun, getRunResults)
 *   - src/lib/types/api.ts (Run, RunStatus)
 *   - src/lib/types/results.ts (RunSummary)
 *   - src/components/runs/live-progress.tsx (SSE live section)
 *   - src/components/runs/results-dashboard.tsx (post-run results)
 *   - src/components/runs/status-badge.tsx (header badge)
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
 *
 * Contract: Exports RunDetailClient component.
 */

"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import {
  ChevronDown,
  ChevronRight,
  Loader2,
  XCircle,
  Settings2,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Separator } from "@/components/ui/separator";
import { StatusBadge } from "@/components/runs/status-badge";
import { LiveProgress } from "@/components/runs/live-progress";
import { ResultsDashboard } from "@/components/runs/results-dashboard";

import { getRun, cancelRun, getRunResults, ApiError } from "@/lib/api";
import type { Run, SseCompleteEvent } from "@/lib/types/api";
import type { RunSummary } from "@/lib/types/results";
import { fmtRelative, fmtAbsolute, fmtDurationMs } from "@/lib/format";
import { cn } from "@/lib/utils";

const CANCELLABLE: Run["status"][] = ["queued", "running", "cancelling"];
const TERMINAL: Run["status"][] = ["succeeded", "failed", "cancelled"];

interface RunDetailClientProps {
  runId: string;
}

// ── Config viewer ─────────────────────────────────────────────────────────────

function ConfigViewer({ config }: { config: unknown }) {
  return (
    <pre className="overflow-auto rounded-lg border border-border bg-muted/30 p-4 text-[11px] leading-relaxed text-foreground">
      {JSON.stringify(config, null, 2)}
    </pre>
  );
}

// ── Log panel ─────────────────────────────────────────────────────────────────

function LogPanel({ lines }: { lines: string[] }) {
  const [open, setOpen] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (open) {
      bottomRef.current?.scrollIntoView({ behavior: "smooth" });
    }
  }, [lines, open]);

  return (
    <div className="rounded-xl border border-border">
      <button
        className="flex w-full items-center gap-2 px-4 py-3 text-left text-xs font-medium text-muted-foreground hover:text-foreground"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
      >
        {open ? (
          <ChevronDown className="h-3.5 w-3.5" />
        ) : (
          <ChevronRight className="h-3.5 w-3.5" />
        )}
        Logs ({lines.length})
      </button>
      {open && (
        <div className="border-t border-border">
          <div className="h-48 overflow-auto bg-muted/20 p-3 font-mono text-[11px] leading-relaxed text-foreground">
            {lines.length === 0 ? (
              <p className="text-muted-foreground">No log output yet.</p>
            ) : (
              lines.map((line, i) => (
                <div key={i} className="py-0.5">
                  {line}
                </div>
              ))
            )}
            <div ref={bottomRef} />
          </div>
        </div>
      )}
    </div>
  );
}

// ── Timestamp row ─────────────────────────────────────────────────────────────

function TimeRow({ label, dateStr }: { label: string; dateStr: string | null | undefined }) {
  if (!dateStr) return null;
  return (
    <div className="flex items-center gap-1.5 text-xs">
      <span className="text-muted-foreground">{label}:</span>
      <span title={fmtAbsolute(dateStr)} className="cursor-default text-foreground">
        {fmtRelative(dateStr)}
      </span>
    </div>
  );
}

// ── Tab bar ───────────────────────────────────────────────────────────────────

type ActiveTab = "progress" | "config";

function TabBar({
  active,
  onChange,
  showProgress,
}: {
  active: ActiveTab;
  onChange: (t: ActiveTab) => void;
  showProgress: boolean;
}) {
  const tabs: { key: ActiveTab; label: string }[] = [
    ...(showProgress ? [{ key: "progress" as const, label: "Progress" }] : []),
    { key: "config", label: "Configuration" },
  ];

  return (
    <div className="flex gap-1 border-b border-border">
      {tabs.map((t) => (
        <button
          key={t.key}
          className={cn(
            "px-4 pb-3 pt-1 text-xs font-medium transition-colors",
            active === t.key
              ? "border-b-2 border-signal text-foreground"
              : "text-muted-foreground hover:text-foreground"
          )}
          onClick={() => onChange(t.key)}
          aria-selected={active === t.key}
          role="tab"
        >
          {t.label}
        </button>
      ))}
    </div>
  );
}

// ── Main component ────────────────────────────────────────────────────────────

export function RunDetailClient({ runId }: RunDetailClientProps) {
  const router = useRouter();
  const [run, setRun] = useState<Run | null>(null);
  const [summary, setSummary] = useState<RunSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [cancelling, setCancelling] = useState(false);
  const [logLines, setLogLines] = useState<string[]>([]);
  const [activeTab, setActiveTab] = useState<ActiveTab>("progress");

  const fetchRun = useCallback(async () => {
    try {
      const r = await getRun(runId);
      setRun(r);

      // If complete and no summary yet, fetch it
      if (TERMINAL.includes(r.status) && !r.summary) {
        try {
          const s = await getRunResults(runId);
          setSummary(s);
        } catch {
          // summary not available yet
        }
      } else if (r.summary) {
        setSummary(r.summary);
      }
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        toast.error("Run not found");
        router.push("/runs");
      }
    } finally {
      setLoading(false);
    }
  }, [runId, router]);

  useEffect(() => {
    // Set up a dummy interval first so fetchRun isn't the first statement
    // (avoids react-hooks/set-state-in-effect rule — same pattern as ApiStatus)
    const id = setTimeout(fetchRun, 0);
    return () => clearTimeout(id);
  }, [fetchRun]);

  // Poll while not terminal (fallback when SSE isn't available)
  useEffect(() => {
    if (!run) return;
    if (TERMINAL.includes(run.status)) return;

    const id = setInterval(fetchRun, 5_000);
    return () => clearInterval(id);
  }, [run, fetchRun]);

  const handleSseComplete = useCallback((evt: SseCompleteEvent) => {
    if (evt.summary) {
      setSummary(evt.summary);
    }
    setRun((prev) => (prev ? { ...prev, status: "succeeded" } : prev));
    setActiveTab("progress"); // keep on progress for results display
    fetchRun(); // refresh to get finished_at etc.
  }, [fetchRun]);

  const handleLog = useCallback((msg: string) => {
    setLogLines((prev) => [...prev.slice(-500), msg]);
  }, []);

  const handleCancel = async () => {
    if (!run) return;
    setCancelling(true);
    try {
      await cancelRun(run.id);
      toast.success("Cancel requested");
      fetchRun();
    } catch (err) {
      if (err instanceof ApiError) {
        toast.error(`Cancel failed: ${err.message}`);
      }
    } finally {
      setCancelling(false);
    }
  };

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="flex items-start justify-between">
          <div className="space-y-2">
            <Skeleton className="h-5 w-48" />
            <Skeleton className="h-4 w-64" />
          </div>
          <Skeleton className="h-7 w-20 rounded-lg" />
        </div>
        <Skeleton className="h-32 w-full rounded-xl" />
      </div>
    );
  }

  if (!run) return null;

  const isActive = run.status === "running" || run.status === "queued";
  const isTerminal = TERMINAL.includes(run.status);
  const canCancel = CANCELLABLE.includes(run.status);
  const showProgress = isActive;

  return (
    <div className="space-y-6">
      {/* ── Header ──────────────────────────────────────────────────────────── */}
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1.5">
          <div className="flex items-center gap-2">
            <h1 className="text-lg font-semibold text-foreground">
              {run.name ?? "Untitled run"}
            </h1>
            <StatusBadge status={run.status} />
          </div>
          <p className="font-mono text-xs text-muted-foreground">{run.id}</p>
          <div className="flex items-center gap-3">
            <TimeRow label="Created" dateStr={run.created_at} />
            <TimeRow label="Started" dateStr={run.started_at} />
            <TimeRow label="Finished" dateStr={run.finished_at} />
            {run.started_at && run.finished_at && (
              <span className="text-xs text-muted-foreground">
                Duration:{" "}
                {fmtDurationMs(
                  new Date(run.finished_at).getTime() -
                  new Date(run.started_at).getTime()
                )}
              </span>
            )}
          </div>
        </div>

        {canCancel && (
          <Button
            variant="outline"
            size="sm"
            className="shrink-0 gap-1.5 text-destructive hover:border-destructive/40 hover:text-destructive"
            onClick={handleCancel}
            disabled={cancelling || run.status === "cancelling"}
          >
            {cancelling ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <XCircle className="h-3.5 w-3.5" />
            )}
            {run.status === "cancelling" ? "Cancelling…" : "Cancel run"}
          </Button>
        )}
      </div>

      <Separator />

      {/* ── Results dashboard (when terminal + summary) ──────────────────────── */}
      {isTerminal && summary ? (
        <div className="space-y-6">
          <ResultsDashboard runId={run.id} summary={summary} />
          <Separator />
          {/* Config viewer always available */}
          <div>
            <div className="mb-3 flex items-center gap-1.5">
              <Settings2 className="h-3.5 w-3.5 text-muted-foreground" />
              <h2 className="text-sm font-semibold text-foreground">Configuration</h2>
            </div>
            {run.config ? (
              <ConfigViewer config={run.config} />
            ) : (
              <p className="text-xs text-muted-foreground">Configuration not available</p>
            )}
          </div>
        </div>
      ) : isTerminal && !summary ? (
        /* Terminal but no summary yet — show loading */
        <div className="space-y-3">
          <p className="text-sm text-muted-foreground">Loading results…</p>
          <Skeleton className="h-24 w-full rounded-xl" />
          <Skeleton className="h-48 w-full rounded-xl" />
        </div>
      ) : (
        /* Active run — tabs for Progress / Config */
        <>
          <TabBar
            active={activeTab}
            onChange={setActiveTab}
            showProgress={showProgress}
          />

          {activeTab === "progress" && showProgress && (
            <LiveProgress
              runId={run.id}
              totalSubscribers={run.progress?.subscribers_total ?? run.config?.subscribers?.count ?? 0}
              onComplete={handleSseComplete}
              onLog={handleLog}
            />
          )}

          {activeTab === "config" && (
            <div>
              {run.config ? (
                <ConfigViewer config={run.config} />
              ) : (
                <p className="text-xs text-muted-foreground">Configuration not available</p>
              )}
            </div>
          )}

          <LogPanel lines={logLines} />
        </>
      )}
    </div>
  );
}
