/**
 * Dashboard page — radstorm home screen.
 *
 * Purpose:
 *   Landing page showing recent runs and key metrics. In Wave 1 this renders
 *   a polished empty state: branded hero + capability summary + action prompt.
 *   Full run list and charts arrive in Wave 2/3.
 *
 * Related files:
 *   - src/components/layout/app-shell.tsx (wraps this page)
 *   - src/components/layout/page-container.tsx (page padding/header)
 *   - src/lib/api.ts (listRuns — hooked up in Wave 3)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

import type { Metadata } from "next";
import { PageContainer, PageHeader } from "@/components/layout/page-container";
import { LinkButton } from "@/components/ui/link-button";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { Plus, Zap, Shield, Activity, Clock } from "lucide-react";

export const metadata: Metadata = {
  title: "Dashboard",
};

const CAPABILITIES = [
  {
    icon: Zap,
    label: "Scale",
    value: "Up to 1M subscribers",
    desc: "Virtual PPPoE/MAC subscribers simulated concurrently",
  },
  {
    icon: Activity,
    label: "Protocols",
    value: "Access + Accounting + CoA",
    desc: "RFC 2865/2866/3576 with Huawei VSA support",
  },
  {
    icon: Shield,
    label: "Scenarios",
    value: "Cold start, uniform, pessimal",
    desc: "Outage-recovery, steady-state, and burst storm modes",
  },
  {
    icon: Clock,
    label: "Results",
    value: "Parquet + summary.json",
    desc: "Per-subscriber latency, retransmit distribution, CoA timing",
  },
];

export default function DashboardPage() {
  return (
    <PageContainer>
      <PageHeader
        title="Dashboard"
        description="Recent test runs and system overview"
        actions={
          <LinkButton href="/runs/new" size="sm" className="gap-1.5">
            <Plus className="h-3.5 w-3.5" />
            New Run
          </LinkButton>
        }
      />

      {/* Empty state hero */}
      <div className="relative overflow-hidden rounded-xl border border-border bg-card">
        {/* Subtle grid pattern */}
        <div
          className="pointer-events-none absolute inset-0 opacity-[0.03] dark:opacity-[0.06]"
          aria-hidden="true"
          style={{
            backgroundImage:
              "repeating-linear-gradient(0deg, transparent, transparent 24px, currentColor 24px, currentColor 25px), repeating-linear-gradient(90deg, transparent, transparent 24px, currentColor 24px, currentColor 25px)",
          }}
        />

        <div className="relative px-8 py-14 text-center">
          {/* Identity mark */}
          <div className="mb-6 flex justify-center">
            <div className="flex items-center gap-2 rounded-full border border-border bg-background px-4 py-1.5">
              <span className="h-1.5 w-1.5 rounded-full bg-signal" />
              <span className="font-mono text-xs text-muted-foreground">
                radstorm v0.1 — RADIUS stress tester
              </span>
            </div>
          </div>

          {/* Headline */}
          <h2 className="text-2xl font-semibold tracking-tight text-foreground">
            No runs yet.
          </h2>
          <p className="mx-auto mt-3 max-w-md text-sm leading-relaxed text-muted-foreground">
            Configure a scenario and trigger your first run to stress-test a
            RADIUS server at ISP scale. Results are stored locally and
            visualized here.
          </p>

          <div className="mt-6 flex justify-center gap-3">
            <LinkButton href="/runs/new" size="sm" className="gap-1.5">
              <Plus className="h-3.5 w-3.5" />
              Configure a run
            </LinkButton>
            <LinkButton href="/settings" variant="outline" size="sm">
              Settings
            </LinkButton>
          </div>
        </div>

        <Separator />

        {/* Capability grid */}
        <div className="grid grid-cols-2 divide-x divide-y divide-border lg:grid-cols-4">
          {CAPABILITIES.map((cap, i) => {
            const Icon = cap.icon;
            return (
              <div key={i} className="px-6 py-5">
                <div className="mb-2 flex items-center gap-2">
                  <Icon className="h-3.5 w-3.5 text-signal" aria-hidden="true" />
                  <Badge variant="secondary" className="font-mono text-[10px]">
                    {cap.label}
                  </Badge>
                </div>
                <p className="text-sm font-medium text-foreground">{cap.value}</p>
                <p className="mt-0.5 text-xs text-muted-foreground">{cap.desc}</p>
              </div>
            );
          })}
        </div>
      </div>

      {/* Stats placeholder — "Coming in Wave 2" */}
      <div className="mt-8 grid gap-4 sm:grid-cols-3">
        {(["Total runs", "Last run", "Success rate"] as const).map((label) => (
          <div
            key={label}
            className="rounded-lg border border-border bg-card p-5"
          >
            <p className="text-xs text-muted-foreground">{label}</p>
            <p className="mt-2 font-mono text-2xl font-semibold text-muted-foreground/30">
              —
            </p>
          </div>
        ))}
      </div>
    </PageContainer>
  );
}
