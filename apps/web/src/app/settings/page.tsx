/**
 * Settings page — radstorm frontend configuration.
 *
 * Purpose:
 *   Application settings: API server URL, default scenario preferences,
 *   UI preferences. In Wave 1 this is a skeleton with the layout shell.
 *   Actual settings form arrives in Wave 2/3.
 *
 * Related files:
 *   - src/components/shell/api-status.tsx (polls the configured API URL)
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
  title: "Settings",
};

export default function SettingsPage() {
  return (
    <PageContainer>
      <PageHeader
        title="Settings"
        description="API connection, defaults, and application preferences"
      />
      <WaveComingPlaceholder wave={2} />
    </PageContainer>
  );
}
