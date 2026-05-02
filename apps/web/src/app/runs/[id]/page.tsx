/**
 * Run detail page — live progress and results for a single test run.
 *
 * Purpose:
 *   Shows real-time progress (via SSE) during an active run, and the full
 *   results dashboard (establishment curve, latency histogram, subscriber
 *   stats, threshold evaluation) once complete.
 *   In Wave 1 this is a skeleton with layout shell. Full implementation
 *   in Wave 3.
 *
 * Related files:
 *   - src/lib/api.ts (getRun, getRunResults, subscribeRunEvents)
 *   - src/lib/types/api.ts (Run, RunProgress)
 *   - src/lib/types/results.ts (RunSummary)
 *   - src/components/layout/page-container.tsx
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

import type { Metadata } from "next";
import {
  PageContainer,
  PageHeader,
  WaveComingPlaceholder,
} from "@/components/layout/page-container";

export const metadata: Metadata = {
  title: "Run Detail",
};

interface RunDetailPageProps {
  params: Promise<{ id: string }>;
}

export default async function RunDetailPage({ params }: RunDetailPageProps) {
  const { id } = await params;

  return (
    <PageContainer>
      <PageHeader
        title="Run Detail"
        description={`Run ${id}`}
      />
      <WaveComingPlaceholder wave={3} />
    </PageContainer>
  );
}
