/**
 * Custom hook for consuming the run progress SSE stream.
 *
 * Purpose:
 *   Opens a persistent EventSource connection to GET /api/v1/runs/{id}/events
 *   and dispatches typed events to caller via callbacks. Handles reconnection
 *   on transient errors, cleanup on unmount, and the terminal `complete` event.
 *
 * Related files:
 *   - src/lib/api.ts (subscribeRunEvents — builds the EventSource URL)
 *   - src/lib/types/api.ts (SseProgressEvent, SseLogEvent, SseCompleteEvent)
 *   - src/app/runs/[id]/run-detail-client.tsx (primary consumer)
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
 *
 * Contract: Exports useRunEvents hook with typed event callbacks.
 */

"use client";

import { useEffect, useRef } from "react";
import type { SseProgressEvent, SseLogEvent, SseCompleteEvent } from "@/lib/types/api";

const API_BASE =
  process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080";

interface UseRunEventsOptions {
  runId: string;
  enabled?: boolean;
  onProgress?: (event: SseProgressEvent) => void;
  onLog?: (event: SseLogEvent) => void;
  onComplete?: (event: SseCompleteEvent) => void;
  onError?: (err: Event) => void;
}

/**
 * Opens an SSE stream for the given run ID and forwards typed events to
 * the provided callbacks. Automatically closes the stream when `enabled`
 * becomes false or the component unmounts.
 *
 * Reconnects once on transient error (e.g. brief network blip), then gives up
 * to avoid spamming a completed run.
 */
export function useRunEvents({
  runId,
  enabled = true,
  onProgress,
  onLog,
  onComplete,
  onError,
}: UseRunEventsOptions): void {
  // Use layout effect ref pattern — store callbacks without causing re-renders
  const callbacksRef = useRef({ onProgress, onLog, onComplete, onError });

  useEffect(() => {
    callbacksRef.current = { onProgress, onLog, onComplete, onError };
  });

  useEffect(() => {
    if (!enabled) return;

    const doneRef = { current: false };
    const reconnectedRef = { current: false };
    let es: EventSource | null = null;

    function open() {
      if (doneRef.current) return;

      const url = `${API_BASE}/api/v1/runs/${encodeURIComponent(runId)}/events`;
      const source = new EventSource(url);
      es = source;

      source.addEventListener("progress", (e: MessageEvent) => {
        try {
          const data = JSON.parse(e.data as string) as SseProgressEvent;
          callbacksRef.current.onProgress?.(data);
        } catch {
          // malformed event — skip
        }
      });

      source.addEventListener("log", (e: MessageEvent) => {
        try {
          const data = JSON.parse(e.data as string) as SseLogEvent;
          callbacksRef.current.onLog?.(data);
        } catch {
          // malformed event — skip
        }
      });

      source.addEventListener("complete", (e: MessageEvent) => {
        try {
          const data = JSON.parse(e.data as string) as SseCompleteEvent;
          doneRef.current = true;
          callbacksRef.current.onComplete?.(data);
          source.close();
          es = null;
        } catch {
          // malformed event — skip
        }
      });

      source.onerror = (e) => {
        callbacksRef.current.onError?.(e);
        source.close();
        es = null;

        // Reconnect once on transient error
        if (!reconnectedRef.current && !doneRef.current) {
          reconnectedRef.current = true;
          setTimeout(open, 2000);
        }
      };
    }

    open();

    return () => {
      doneRef.current = true;
      es?.close();
      es = null;
    };
  }, [enabled, runId]);
}
