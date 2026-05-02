/**
 * Run detail page — live progress and results for a single test run.
 *
 * Purpose:
 *   Server component wrapper that extracts the run ID from route params and
 *   renders the RunDetailClient component. RunDetailClient handles all
 *   data fetching, SSE streaming, and progressive enhancement from
 *   live progress → results dashboard when the run finishes.
 *
 * Related files:
 *   - src/app/runs/[id]/run-detail-client.tsx (interactive client component)
 *   - src/lib/api.ts (getRun, cancelRun, getRunResults, subscribeRunEvents)
 *   - src/lib/types/api.ts (Run, RunProgress)
 *   - src/lib/types/results.ts (RunSummary)
 *   - src/components/layout/page-container.tsx
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
 *
 * Contract: internal
 */

import type { Metadata } from "next";
import { PageContainer } from "@/components/layout/page-container";
import { RunDetailClient } from "@/app/runs/[id]/run-detail-client";

export const metadata: Metadata = {
  title: "Run Detail — radstorm",
};

interface RunDetailPageProps {
  params: Promise<{ id: string }>;
}

export default async function RunDetailPage({ params }: RunDetailPageProps) {
  const { id } = await params;

  return (
    <PageContainer>
      <RunDetailClient runId={id} />
    </PageContainer>
  );
}
