/**
 * Global error boundary page for radstorm.
 *
 * Purpose:
 *   Next.js App Router error boundary — catches unhandled errors in the
 *   route tree and renders a user-friendly error state with a reset option.
 *   Maintains the app shell so navigation remains available.
 *
 * Related files:
 *   - src/app/layout.tsx (root layout, parent of this boundary)
 *   - src/components/layout/page-container.tsx
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

"use client";

import { useEffect } from "react";
import { Button } from "@/components/ui/button";
import { PageContainer } from "@/components/layout/page-container";

interface ErrorPageProps {
  error: Error & { digest?: string };
  reset: () => void;
}

export default function ErrorPage({ error, reset }: ErrorPageProps) {
  useEffect(() => {
    // Log to an error-reporting service in production
    console.error("[radstorm] Unhandled error:", error);
  }, [error]);

  return (
    <PageContainer>
      <div className="flex flex-col items-center justify-center py-24 text-center">
        <p className="font-mono text-xs text-muted-foreground">
          UNHANDLED ERROR
        </p>
        <h1 className="mt-3 text-lg font-semibold text-foreground">
          Something went wrong
        </h1>
        <p className="mt-2 max-w-sm text-sm text-muted-foreground">
          {error.message ?? "An unexpected error occurred. Try resetting the page or navigating back."}
        </p>
        {error.digest && (
          <p className="mt-2 font-mono text-[10px] text-muted-foreground">
            digest: {error.digest}
          </p>
        )}
        <div className="mt-6 flex gap-3">
          <Button size="sm" onClick={reset}>
            Reset page
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => (window.location.href = "/")}
          >
            Go to Dashboard
          </Button>
        </div>
      </div>
    </PageContainer>
  );
}
