/**
 * ResultsDashboard — full results visualization for a completed run.
 *
 * Purpose:
 *   Renders the complete post-run analysis: pass/fail summary, threshold table,
 *   subscriber outcome donut, establishment curve, latency histogram with
 *   percentile markers, retransmit distribution, CoA/Disconnect cards,
 *   server health, and artifacts download list.
 *
 * Related files:
 *   - src/lib/types/results.ts (RunSummary, all result types)
 *   - src/lib/api.ts (getEstablishmentCurve, getLatencyHistogram, getArtifacts)
 *   - src/app/runs/[id]/run-detail-client.tsx (consumer)
 *   - src/lib/format.ts (fmtInt, fmtDurationMs, fmtLatency)
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
 *
 * Contract: Exports ResultsDashboard component.
 */

"use client";

import { useState, useEffect } from "react";
import {
  PieChart,
  Pie,
  Cell,
  Line,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip as ReTooltip,
  Legend,
  ResponsiveContainer,
  Area,
  ComposedChart,
} from "recharts";
import { Download, Check, X, AlertCircle, FileText } from "lucide-react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";

import type { RunSummary, CurvePoint } from "@/lib/types/results";
import { getEstablishmentCurve, getLatencyHistogram, getArtifacts, artifactDownloadUrl } from "@/lib/api";
import type { Artifact } from "@/lib/types/api";
import { fmtInt, fmtDurationMs, fmtLatency, fmtPct } from "@/lib/format";
import { cn } from "@/lib/utils";

// ── Palette (matches globals.css chart tokens) ────────────────────────────────

const CHART_COLORS = {
  signal: "oklch(0.78 0.18 145)",   // chart-1: signal green
  azure: "oklch(0.60 0.18 240)",    // chart-2: azure
  amber: "oklch(0.72 0.16 60)",     // chart-3: amber
  rose: "oklch(0.65 0.18 320)",     // chart-4: rose
  teal: "oklch(0.58 0.14 200)",     // chart-5: teal
  muted: "oklch(0.52 0 0)",
} as const;

// ── Pass/fail banner ──────────────────────────────────────────────────────────

function OutcomeBanner({ summary }: { summary: RunSummary }) {
  const pass = summary.thresholds.overall === "pass";

  return (
    <div
      className={cn(
        "flex items-center gap-3 rounded-xl border p-4",
        pass
          ? "border-emerald-500/20 bg-emerald-500/5"
          : "border-destructive/20 bg-destructive/5"
      )}
    >
      <div
        className={cn(
          "flex h-8 w-8 shrink-0 items-center justify-center rounded-full",
          pass ? "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" : "bg-destructive/15 text-destructive"
        )}
      >
        {pass ? <Check className="h-4 w-4" /> : <X className="h-4 w-4" />}
      </div>
      <div className="flex-1">
        <p className={cn("font-semibold", pass ? "text-emerald-700 dark:text-emerald-400" : "text-destructive")}>
          {pass ? "All thresholds passed" : "One or more thresholds failed"}
        </p>
        <p className="text-xs text-muted-foreground">
          {summary.scenario_type} · {fmtDurationMs(summary.duration_ms)} ·{" "}
          {fmtInt(summary.subscribers.total)} subscribers
        </p>
      </div>
      <div className="text-right">
        <p className="font-mono text-2xl font-bold tabular-nums text-foreground">
          {fmtPct(summary.subscribers.established, summary.subscribers.total)}
        </p>
        <p className="text-[11px] text-muted-foreground">established</p>
      </div>
    </div>
  );
}

// ── Stat card ────────────────────────────────────────────────────────────────

function StatCard({
  label,
  value,
  sub,
  accent,
}: {
  label: string;
  value: string;
  sub?: string;
  accent?: boolean;
}) {
  return (
    <div className="flex flex-col gap-0.5 rounded-xl border border-border bg-card p-4">
      <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{label}</p>
      <p
        className={cn(
          "font-mono text-2xl font-bold tabular-nums",
          accent ? "text-signal" : "text-foreground"
        )}
      >
        {value}
      </p>
      {sub && <p className="text-xs text-muted-foreground">{sub}</p>}
    </div>
  );
}

// ── Threshold table ───────────────────────────────────────────────────────────

function ThresholdTable({ summary }: { summary: RunSummary }) {
  const { evaluated } = summary.thresholds;
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-semibold">Thresholds</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        <table className="w-full text-xs" aria-label="Threshold evaluation results">
          <thead>
            <tr className="border-b border-border bg-muted/20">
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">Threshold</th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">Expected</th>
              <th className="px-4 py-2.5 text-left font-medium text-muted-foreground">Actual</th>
              <th className="px-4 py-2.5 text-right font-medium text-muted-foreground">Result</th>
            </tr>
          </thead>
          <tbody>
            {evaluated.map((t, i) => (
              <tr
                key={i}
                className={cn(
                  "border-b border-border/50",
                  i === evaluated.length - 1 && "border-b-0"
                )}
              >
                <td className="px-4 py-2.5 font-mono text-foreground">{t.name}</td>
                <td className="px-4 py-2.5 text-muted-foreground">{t.expected}</td>
                <td className="px-4 py-2.5 font-mono tabular-nums">
                  {String(t.actual)}
                </td>
                <td className="px-4 py-2.5 text-right">
                  {t.pass ? (
                    <span className="inline-flex items-center gap-1 rounded-full border border-emerald-500/20 bg-emerald-500/10 px-2 py-0.5 text-[10px] font-medium text-emerald-600 dark:text-emerald-400">
                      <Check className="h-2.5 w-2.5" />
                      pass
                    </span>
                  ) : (
                    <span className="inline-flex items-center gap-1 rounded-full border border-destructive/20 bg-destructive/10 px-2 py-0.5 text-[10px] font-medium text-destructive">
                      <X className="h-2.5 w-2.5" />
                      fail
                    </span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </CardContent>
    </Card>
  );
}

// ── Subscriber donut ──────────────────────────────────────────────────────────

function SubscriberDonut({ summary }: { summary: RunSummary }) {
  const { subscribers: s } = summary;
  const data = [
    { name: "Established", value: s.established, color: CHART_COLORS.signal },
    { name: "Auth failed", value: s.auth_failed, color: CHART_COLORS.rose },
    { name: "Acct failed", value: s.acct_failed, color: CHART_COLORS.amber },
    { name: "Terminated", value: s.terminated, color: CHART_COLORS.azure },
    { name: "In flight", value: s.still_in_flight_at_end, color: CHART_COLORS.muted },
  ].filter((d) => d.value > 0);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-semibold">Subscriber outcomes</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex items-center gap-4">
          <ResponsiveContainer width={160} height={160}>
            <PieChart>
              <Pie
                data={data}
                innerRadius={50}
                outerRadius={76}
                paddingAngle={2}
                dataKey="value"
                aria-label="Subscriber outcome distribution"
              >
                {data.map((entry, i) => (
                  <Cell key={i} fill={entry.color} stroke="transparent" />
                ))}
              </Pie>
              <ReTooltip
                formatter={(v, name) => [typeof v === "number" ? fmtInt(v) : String(v), String(name)]}
                contentStyle={{ fontSize: 11 }}
              />
            </PieChart>
          </ResponsiveContainer>
          <div className="flex flex-col gap-2">
            {data.map((d, i) => (
              <div key={i} className="flex items-center gap-2">
                <span
                  className="inline-block h-2.5 w-2.5 shrink-0 rounded-full"
                  style={{ background: d.color }}
                />
                <span className="text-xs text-muted-foreground">{d.name}</span>
                <span className="ml-auto font-mono text-xs tabular-nums text-foreground">
                  {fmtInt(d.value)}
                </span>
              </div>
            ))}
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

// ── Establishment curve ───────────────────────────────────────────────────────

function EstablishmentCurve({ curve }: { curve: CurvePoint[] }) {
  const data = curve.map((p) => ({
    t: p.offset_ms / 1000,
    established: p.established,
    activated: p.activated,
    in_flight: p.in_flight,
  }));

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-semibold">Establishment curve</CardTitle>
      </CardHeader>
      <CardContent>
        <ResponsiveContainer width="100%" height={220}>
          <ComposedChart data={data} margin={{ top: 4, right: 8, bottom: 4, left: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="oklch(0.52 0 0 / 15%)" />
            <XAxis
              dataKey="t"
              tick={{ fontSize: 10 }}
              tickFormatter={(v) => `${v}s`}
              stroke="oklch(0.52 0 0 / 40%)"
            />
            <YAxis tick={{ fontSize: 10 }} stroke="oklch(0.52 0 0 / 40%)" />
            <ReTooltip
              labelFormatter={(v) => `t=${v}s`}
              formatter={(v, name) => [typeof v === "number" ? fmtInt(v) : String(v), String(name)]}
              contentStyle={{ fontSize: 11 }}
            />
            <Legend iconSize={8} wrapperStyle={{ fontSize: 11 }} />
            <Area
              type="monotone"
              dataKey="in_flight"
              fill={`${CHART_COLORS.azure}33`}
              stroke={CHART_COLORS.azure}
              strokeWidth={1.5}
              name="In flight"
              dot={false}
            />
            <Line
              type="monotone"
              dataKey="activated"
              stroke={CHART_COLORS.amber}
              strokeWidth={1.5}
              name="Activated"
              dot={false}
            />
            <Line
              type="monotone"
              dataKey="established"
              stroke={CHART_COLORS.signal}
              strokeWidth={2}
              name="Established"
              dot={false}
            />
          </ComposedChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  );
}

// ── Latency histogram ─────────────────────────────────────────────────────────

function LatencyHistogram({
  buckets,
  latencyMs,
}: {
  buckets: { le_ms: number; count: number }[];
  latencyMs: RunSummary["establishment"]["latency_ms"];
}) {
  // Convert cumulative buckets to per-bucket counts
  const data = buckets.map((b, i) => ({
    le: `${b.le_ms}ms`,
    count: i === 0 ? b.count : b.count - (buckets[i - 1]?.count ?? 0),
    le_ms: b.le_ms,
  }));

  const markers = [
    { value: latencyMs.p50, label: "p50", color: CHART_COLORS.azure },
    { value: latencyMs.p95, label: "p95", color: CHART_COLORS.amber },
    { value: latencyMs.p99, label: "p99", color: CHART_COLORS.rose },
    { value: latencyMs.p999, label: "p99.9", color: CHART_COLORS.rose },
  ];

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-semibold">Latency distribution</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="mb-3 flex flex-wrap gap-3">
          {markers.map((m) => (
            <div key={m.label} className="flex items-baseline gap-1">
              <span
                className="inline-block h-2 w-2 rounded-full"
                style={{ background: m.color }}
              />
              <span className="text-[11px] text-muted-foreground">{m.label}</span>
              <span className="font-mono text-xs tabular-nums text-foreground">
                {fmtLatency(m.value)}
              </span>
            </div>
          ))}
        </div>
        <ResponsiveContainer width="100%" height={200}>
          <BarChart data={data} margin={{ top: 4, right: 8, bottom: 4, left: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="oklch(0.52 0 0 / 15%)" />
            <XAxis dataKey="le" tick={{ fontSize: 10 }} stroke="oklch(0.52 0 0 / 40%)" />
            <YAxis tick={{ fontSize: 10 }} stroke="oklch(0.52 0 0 / 40%)" />
            <ReTooltip
              formatter={(v) => [typeof v === "number" ? fmtInt(v) : String(v), "Count"]}
              contentStyle={{ fontSize: 11 }}
            />
            <Bar dataKey="count" fill={CHART_COLORS.azure} name="Count" radius={[2, 2, 0, 0]} />
          </BarChart>
        </ResponsiveContainer>
      </CardContent>
    </Card>
  );
}

// ── CoA / Disconnect cards ────────────────────────────────────────────────────

function CoaCard({
  title,
  stats,
}: {
  title: string;
  stats: RunSummary["coa"];
}) {
  const total = stats.received;
  if (total === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-semibold">{title}</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-xs text-muted-foreground">No {title.toLowerCase()} events received</p>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-semibold">{title}</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-4 gap-3">
          {[
            { label: "Received", value: stats.received },
            { label: "ACKed", value: stats.acked },
            { label: "NAKed", value: stats.naked },
            { label: "Dropped", value: stats.dropped },
          ].map((item) => (
            <div key={item.label} className="flex flex-col gap-0.5">
              <p className="text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
                {item.label}
              </p>
              <p className="font-mono text-lg font-bold tabular-nums text-foreground">
                {fmtInt(item.value)}
              </p>
            </div>
          ))}
        </div>
        {stats.response_latency_us && (
          <div className="mt-3 border-t border-border pt-3">
            <p className="mb-1.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
              Response latency
            </p>
            <div className="flex flex-wrap gap-3">
              {[
                { label: "p50", value: stats.response_latency_us.p50 / 1000 },
                { label: "p95", value: stats.response_latency_us.p95 / 1000 },
                { label: "p99", value: stats.response_latency_us.p99 / 1000 },
              ].map((m) => (
                <div key={m.label} className="flex items-baseline gap-1">
                  <span className="text-[10px] text-muted-foreground">{m.label}</span>
                  <span className="font-mono text-xs tabular-nums">{fmtLatency(m.value)}</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

// ── Server health ─────────────────────────────────────────────────────────────

function ServerHealthCard({ summary }: { summary: RunSummary }) {
  const { server_health: h } = summary;
  const healthy = h.error_responses === 0 && h.unresponsive_periods.length === 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-semibold">Server health</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex items-center gap-3">
          <div
            className={cn(
              "flex h-7 w-7 shrink-0 items-center justify-center rounded-full",
              healthy
                ? "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
                : "bg-destructive/10 text-destructive"
            )}
          >
            {healthy ? <Check className="h-3.5 w-3.5" /> : <AlertCircle className="h-3.5 w-3.5" />}
          </div>
          <div>
            <p className="text-sm font-medium text-foreground">
              {healthy ? "No issues detected" : "Issues detected"}
            </p>
            <p className="text-xs text-muted-foreground">
              {h.error_responses} error response{h.error_responses !== 1 ? "s" : ""} ·{" "}
              {h.unresponsive_periods.length} unresponsive period{h.unresponsive_periods.length !== 1 ? "s" : ""}
            </p>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}

// ── Artifacts list ────────────────────────────────────────────────────────────

function ArtifactsList({ runId, artifacts }: { runId: string; artifacts: Artifact[] }) {
  function formatBytes(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-semibold">Artifacts</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        {artifacts.length === 0 ? (
          <p className="px-4 pb-4 text-xs text-muted-foreground">No artifacts available</p>
        ) : (
          <ul className="divide-y divide-border">
            {artifacts.map((a) => (
              <li key={a.name} className="flex items-center gap-3 px-4 py-3">
                <FileText className="h-4 w-4 shrink-0 text-muted-foreground" />
                <div className="flex flex-1 items-baseline gap-2">
                  <span className="font-mono text-xs text-foreground">{a.name}</span>
                  <span className="text-[10px] text-muted-foreground">{formatBytes(a.size)}</span>
                </div>
                <a
                  href={artifactDownloadUrl(runId, a.name)}
                  download={a.name}
                  className="inline-flex items-center gap-1 rounded-lg border border-border px-2 py-1 text-[11px] text-muted-foreground transition-colors hover:border-signal/40 hover:text-signal"
                  aria-label={`Download ${a.name}`}
                >
                  <Download className="h-3 w-3" />
                  Download
                </a>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

// ── Main dashboard ────────────────────────────────────────────────────────────

interface ResultsDashboardProps {
  runId: string;
  summary: RunSummary;
}

export function ResultsDashboard({ runId, summary }: ResultsDashboardProps) {
  const [curve, setCurve] = useState<CurvePoint[] | null>(null);
  const [buckets, setBuckets] = useState<{ le_ms: number; count: number }[] | null>(null);
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);

  useEffect(() => {
    // Use embedded curve if available, else fetch from API
    const embeddedCurve = summary.establishment.curve;
    const curvePromise =
      embeddedCurve.length > 0
        ? Promise.resolve(embeddedCurve)
        : getEstablishmentCurve(runId)
            .then((c) => c.points)
            .catch(() => [] as CurvePoint[]);

    curvePromise.then(setCurve).catch(() => setCurve([]));

    getLatencyHistogram(runId)
      .then((h) => setBuckets(h.buckets))
      .catch(() => setBuckets([]));

    getArtifacts(runId)
      .then((a) => setArtifacts(a.artifacts))
      .catch(() => setArtifacts([]));
  }, [runId, summary]);

  return (
    <div className="space-y-5">
      {/* Pass/fail banner */}
      <OutcomeBanner summary={summary} />

      {/* Summary stat cards */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <StatCard
          label="Established"
          value={fmtInt(summary.subscribers.established)}
          sub={`of ${fmtInt(summary.subscribers.total)}`}
          accent
        />
        <StatCard
          label="p99 latency"
          value={fmtLatency(summary.establishment.latency_ms.p99)}
          sub={`p50 ${fmtLatency(summary.establishment.latency_ms.p50)}`}
        />
        <StatCard
          label="Duration"
          value={fmtDurationMs(summary.duration_ms)}
          sub={`first at ${fmtDurationMs(summary.establishment.time_to_first_ms)}`}
        />
        <StatCard
          label="Retransmits"
          value={fmtInt(summary.retransmits.total)}
          sub={`${fmtInt(summary.retransmits.subscribers_with_retransmit)} subscribers`}
        />
      </div>

      {/* Thresholds */}
      <ThresholdTable summary={summary} />

      {/* Charts row */}
      <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
        <SubscriberDonut summary={summary} />
        {buckets !== null && buckets.length > 0 ? (
          <LatencyHistogram
            buckets={buckets}
            latencyMs={summary.establishment.latency_ms}
          />
        ) : buckets === null ? (
          <Card>
            <CardHeader><CardTitle className="text-sm font-semibold">Latency distribution</CardTitle></CardHeader>
            <CardContent><Skeleton className="h-48 w-full" /></CardContent>
          </Card>
        ) : null}
      </div>

      {/* Establishment curve */}
      {curve !== null && curve.length > 0 ? (
        <EstablishmentCurve curve={curve} />
      ) : curve === null ? (
        <Card>
          <CardHeader><CardTitle className="text-sm font-semibold">Establishment curve</CardTitle></CardHeader>
          <CardContent><Skeleton className="h-56 w-full" /></CardContent>
        </Card>
      ) : null}

      {/* CoA + Disconnect */}
      <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
        <CoaCard title="CoA" stats={summary.coa} />
        <CoaCard title="Disconnect" stats={summary.disconnect} />
      </div>

      {/* Server health */}
      <ServerHealthCard summary={summary} />

      {/* Artifacts */}
      <ArtifactsList runId={runId} artifacts={artifacts} />
    </div>
  );
}
