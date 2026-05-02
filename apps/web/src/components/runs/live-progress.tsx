/**
 * LiveProgress — real-time progress section for an active run.
 *
 * Purpose:
 *   Displays the SSE-driven live progress while a run is in status "running":
 *   a progress bar, four stat cards (established/in-flight/failed/retransmits),
 *   and a live establishment curve chart being drawn as events arrive.
 *
 * Related files:
 *   - src/lib/hooks/use-run-events.ts (SSE subscription hook)
 *   - src/lib/types/api.ts (SseProgressEvent)
 *   - src/lib/format.ts (fmtInt, fmtPct)
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
 *
 * Contract: Exports LiveProgress component.
 */

"use client";

import { useState, useCallback } from "react";
import {
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip as ReTooltip,
  ResponsiveContainer,
  Area,
  ComposedChart,
} from "recharts";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Progress } from "@/components/ui/progress";
import { useRunEvents } from "@/lib/hooks/use-run-events";
import type { SseProgressEvent, SseCompleteEvent } from "@/lib/types/api";
import { fmtInt } from "@/lib/format";

interface LiveProgressProps {
  runId: string;
  totalSubscribers: number;
  onComplete?: (event: SseCompleteEvent) => void;
  onLog?: (msg: string) => void;
}

interface ChartPoint {
  t: number;
  established: number;
  in_flight: number;
  activated: number;
}

const MAX_CHART_POINTS = 120;

export function LiveProgress({
  runId,
  totalSubscribers,
  onComplete,
  onLog,
}: LiveProgressProps) {
  const [latest, setLatest] = useState<SseProgressEvent | null>(null);
  const [chartData, setChartData] = useState<ChartPoint[]>([]);

  const handleProgress = useCallback((evt: SseProgressEvent) => {
    setLatest(evt);
    setChartData((prev) => {
      const point: ChartPoint = {
        t: Math.round(evt.offset_ms / 1000),
        established: evt.established,
        in_flight: evt.in_flight,
        activated: evt.activated,
      };
      const next = [...prev, point];
      return next.length > MAX_CHART_POINTS
        ? next.slice(next.length - MAX_CHART_POINTS)
        : next;
    });
  }, []);

  const handleLog = useCallback(
    (evt: { msg: string }) => {
      onLog?.(evt.msg);
    },
    [onLog]
  );

  useRunEvents({
    runId,
    enabled: true,
    onProgress: handleProgress,
    onLog: handleLog,
    onComplete,
  });

  const established = latest?.established ?? 0;
  const inFlight = latest?.in_flight ?? 0;
  const failed = latest?.failed ?? 0;
  const retransmits = latest?.retransmits ?? 0;
  const pct = totalSubscribers > 0 ? (established / totalSubscribers) * 100 : 0;

  return (
    <div className="space-y-4">
      {/* Progress bar */}
      <div className="space-y-1.5">
        <div className="flex items-center justify-between text-xs">
          <span className="text-muted-foreground">
            {fmtInt(established)} established
          </span>
          <span className="font-mono tabular-nums text-foreground">
            {pct.toFixed(1)}%
          </span>
        </div>
        <Progress value={pct} className="h-1.5" aria-label={`${pct.toFixed(1)}% established`} />
        <div className="text-right text-[10px] text-muted-foreground">
          {fmtInt(totalSubscribers)} total
        </div>
      </div>

      {/* Stat cards */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        {[
          { label: "Established", value: established, accent: true },
          { label: "In flight", value: inFlight, accent: false },
          { label: "Failed", value: failed, accent: false },
          { label: "Retransmits", value: retransmits, accent: false },
        ].map(({ label, value, accent }) => (
          <div
            key={label}
            className="flex flex-col gap-0.5 rounded-xl border border-border bg-card p-4"
          >
            <p className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
              {label}
            </p>
            <p
              className={
                accent
                  ? "font-mono text-2xl font-bold tabular-nums text-signal"
                  : "font-mono text-2xl font-bold tabular-nums text-foreground"
              }
            >
              {fmtInt(value)}
            </p>
          </div>
        ))}
      </div>

      {/* Live chart */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-semibold">Live establishment</CardTitle>
        </CardHeader>
        <CardContent>
          {chartData.length === 0 ? (
            <div className="flex h-48 items-center justify-center">
              <span className="flex items-center gap-2 text-xs text-muted-foreground">
                <span className="inline-block h-1.5 w-1.5 animate-pulse rounded-full bg-signal" />
                Waiting for first progress event…
              </span>
            </div>
          ) : (
            <ResponsiveContainer width="100%" height={200}>
              <ComposedChart data={chartData} margin={{ top: 4, right: 8, bottom: 4, left: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="oklch(0.52 0 0 / 15%)" />
                <XAxis
                  dataKey="t"
                  tick={{ fontSize: 10 }}
                  tickFormatter={(v) => `${v}s`}
                  stroke="oklch(0.52 0 0 / 40%)"
                />
                <YAxis
                  tick={{ fontSize: 10 }}
                  stroke="oklch(0.52 0 0 / 40%)"
                />
                <ReTooltip
                  labelFormatter={(v) => `t=${v}s`}
                  formatter={(v, name) => [typeof v === "number" ? fmtInt(v) : String(v), String(name)]}
                  contentStyle={{ fontSize: 11 }}
                />
                <Area
                  type="monotone"
                  dataKey="in_flight"
                  fill="oklch(0.60 0.18 240 / 20%)"
                  stroke="oklch(0.60 0.18 240)"
                  strokeWidth={1.5}
                  name="In flight"
                  dot={false}
                  isAnimationActive={false}
                />
                <Line
                  type="monotone"
                  dataKey="established"
                  stroke="oklch(0.78 0.18 145)"
                  strokeWidth={2}
                  name="Established"
                  dot={false}
                  isAnimationActive={false}
                />
              </ComposedChart>
            </ResponsiveContainer>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
