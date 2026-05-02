/**
 * Runs list page — all historical test runs.
 *
 * Purpose:
 *   Lists all past and active test runs with status, timestamps, and summary
 *   metrics. In Wave 1 this is a skeleton with the layout shell and a
 *   "Coming in Wave 2" placeholder. Real run list arrives in Wave 2/3.
 *
 * Related files:
 *   - src/lib/api.ts (listRuns)
 *   - src/lib/types/api.ts (Run type)
 *   - src/components/layout/page-container.tsx
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

import type { Metadata } from "next";
import { PageContainer, PageHeader, WaveComingPlaceholder } from "@/components/layout/page-container";
import { LinkButton } from "@/components/ui/link-button";
import { Plus } from "lucide-react";

export const metadata: Metadata = {
  title: "Runs",
};

export default function RunsPage() {
  return (
    <PageContainer>
      <PageHeader
        title="Runs"
        description="All scenario test runs — most recent first"
        actions={
          <LinkButton href="/runs/new" size="sm" className="gap-1.5">
            <Plus className="h-3.5 w-3.5" />
            New Run
          </LinkButton>
        }
      />
      <WaveComingPlaceholder wave={2} />
    </PageContainer>
  );
}
