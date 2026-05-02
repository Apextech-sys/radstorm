/**
 * Runs list page — all historical and active test runs.
 *
 * Purpose:
 *   Sortable table of all runs with status badges, relative timestamps,
 *   duration, and subscriber establishment stats. Polls every 5 seconds
 *   for updates. Includes a polished empty state with CTA to create a run.
 *
 * Related files:
 *   - src/components/runs/runs-table.tsx (the data table component)
 *   - src/lib/api.ts (listRuns)
 *   - src/lib/types/api.ts (Run type)
 *   - src/components/layout/page-container.tsx
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
 *
 * Contract: internal
 */

import type { Metadata } from "next";
import { PageContainer, PageHeader } from "@/components/layout/page-container";
import { LinkButton } from "@/components/ui/link-button";
import { Plus } from "lucide-react";
import { RunsTable } from "@/components/runs/runs-table";

export const metadata: Metadata = {
  title: "Runs — radstorm",
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
      <RunsTable />
    </PageContainer>
  );
}
