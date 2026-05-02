/**
 * API health status indicator for the top bar.
 *
 * Purpose:
 *   Polls GET /api/v1/health every 10 seconds and renders a small
 *   "API: connected / disconnected" dot with label. Provides at-a-glance
 *   visibility that the radstorm API server is reachable.
 *
 * Related files:
 *   - src/lib/api.ts (getHealth function)
 *   - src/components/shell/top-bar.tsx (renders this component)
 *   - .orchestration/contracts/rest-api.md (health endpoint)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal
 */

"use client";

import React, { useEffect, useCallback, useState } from "react";
import { getHealth } from "@/lib/api";
import { cn } from "@/lib/utils";

type Status = "unknown" | "connected" | "disconnected";

const POLL_INTERVAL_MS = 10_000;

export function ApiStatus({ className }: { className?: string }) {
  const [status, setStatus] = useState<Status>("unknown");
  const [version, setVersion] = useState<string | null>(null);

  const check = useCallback(() => {
    getHealth().then((data) => {
      setStatus("connected");
      setVersion(data.version);
    }).catch(() => {
      setStatus("disconnected");
      setVersion(null);
    });
  }, []);

  useEffect(() => {
    const interval = setInterval(check, POLL_INTERVAL_MS);
    check();
    return () => clearInterval(interval);
  }, [check]);

  return (
    <div
      className={cn("flex items-center gap-1.5 text-xs font-mono", className)}
      title={
        status === "connected"
          ? `API online${version ? ` v${version}` : ""}`
          : status === "disconnected"
          ? "API unreachable — is radstorm-api running on :8080?"
          : "Checking API status…"
      }
    >
      <span
        className={cn("inline-block h-1.5 w-1.5 rounded-full", {
          "bg-signal animate-pulse": status === "connected",
          "bg-destructive": status === "disconnected",
          "bg-muted-foreground": status === "unknown",
        })}
        aria-hidden="true"
      />
      <span
        className={cn("text-muted-foreground", {
          "text-signal": status === "connected",
          "text-destructive": status === "disconnected",
        })}
      >
        {status === "connected"
          ? "API connected"
          : status === "disconnected"
          ? "API offline"
          : "API…"}
      </span>
    </div>
  );
}
