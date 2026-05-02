/**
 * 404 Not Found page for radstorm.
 *
 * Purpose:
 *   Custom not-found page maintaining the app shell layout. Shows a clean
 *   404 state with navigation back to the dashboard.
 *
 * Related files:
 *   - src/components/layout/page-container.tsx
 *   - src/app/layout.tsx (provides the shell wrapper)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

import { LinkButton } from "@/components/ui/link-button";
import { PageContainer } from "@/components/layout/page-container";

export default function NotFound() {
  return (
    <PageContainer>
      <div className="flex flex-col items-center justify-center py-24 text-center">
        <p className="font-mono text-5xl font-bold text-muted-foreground/30">
          404
        </p>
        <h1 className="mt-4 text-lg font-semibold text-foreground">
          Page not found
        </h1>
        <p className="mt-2 text-sm text-muted-foreground">
          This route does not exist. Check the URL or navigate back to the
          dashboard.
        </p>
        <LinkButton href="/" size="sm" className="mt-6">
          Go to Dashboard
        </LinkButton>
      </div>
    </PageContainer>
  );
}
