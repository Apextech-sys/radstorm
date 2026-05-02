/**
 * New Run page — scenario configuration form.
 *
 * Purpose:
 *   Provides the UI for configuring a new test scenario: target RADIUS server,
 *   subscriber count, scenario type, ramp parameters, NAS config, etc.
 *   In Wave 1 this is a skeleton showing the layout shell. The full form
 *   with validation and API submission arrives in Wave 2/3.
 *
 * Related files:
 *   - src/lib/types/config.ts (Config zod schema for form validation)
 *   - src/lib/api.ts (createRun)
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
  title: "New Run",
};

export default function NewRunPage() {
  return (
    <PageContainer>
      <PageHeader
        title="New Run"
        description="Configure a scenario and trigger a stress test against a RADIUS server"
      />
      <WaveComingPlaceholder wave={2} />
    </PageContainer>
  );
}
