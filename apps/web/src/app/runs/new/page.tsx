/**
 * New Run page — scenario configuration form.
 *
 * Purpose:
 *   Two-column layout: left column shows a description + template picker
 *   sidebar; right column holds the full editable config form. Submitting
 *   the form triggers POST /api/v1/runs and navigates to /runs/{id}.
 *
 * Related files:
 *   - src/components/runs/scenario-form.tsx (the full form component)
 *   - src/lib/types/config.ts (configSchema for form validation)
 *   - src/lib/api.ts (createRun)
 *   - src/components/layout/page-container.tsx
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
 *
 * Contract: internal
 */

import type { Metadata } from "next";
import { PageContainer, PageHeader } from "@/components/layout/page-container";
import { ScenarioForm } from "@/components/runs/scenario-form";

export const metadata: Metadata = {
  title: "New Run — radstorm",
};

export default function NewRunPage() {
  return (
    <PageContainer constrained={false} className="max-w-4xl mx-auto">
      <PageHeader
        title="New Run"
        description="Configure a scenario and trigger a stress test against a RADIUS server"
      />
      <ScenarioForm />
    </PageContainer>
  );
}
