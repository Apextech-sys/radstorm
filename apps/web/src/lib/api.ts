/**
 * Typed API client for radstorm REST API.
 *
 * Purpose:
 *   Thin fetch wrappers for every endpoint in rest-api.md. All responses
 *   are validated with zod schemas defined in src/lib/types/api.ts.
 *   Does NOT implement SSE streaming (handled separately in Wave 3).
 *
 * Related files:
 *   - .orchestration/contracts/rest-api.md (canonical endpoint specs)
 *   - src/lib/types/api.ts (zod schemas + types for all payloads)
 *   - src/lib/types/config.ts (Config type)
 *   - src/lib/types/results.ts (RunSummary type)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: Public API client surface for all frontend data-fetching.
 */

import type { z } from "zod";
import {
  healthResponseSchema,
  scenarioTemplateSchema,
  runSchema,
  listRunsResponseSchema,
  cancelRunResponseSchema,
  artifactsResponseSchema,
  type CreateRunRequest,
  type RunStatus,
} from "./types/api";
import {
  runSummarySchema,
  establishmentCurveSchema,
  latencyHistogramSchema,
} from "./types/results";

// ── Config ───────────────────────────────────────────────────────────────────

const API_BASE =
  process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080";

const PREFIX = "/api/v1";

// ── Helpers ──────────────────────────────────────────────────────────────────

class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
    public readonly errors?: string[]
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function fetchJson<T>(
  schema: z.ZodType<T>,
  path: string,
  init?: RequestInit
): Promise<T> {
  const url = `${API_BASE}${PREFIX}${path}`;
  const res = await fetch(url, {
    headers: { "Content-Type": "application/json", ...init?.headers },
    ...init,
  });

  if (!res.ok) {
    let message = `HTTP ${res.status}`;
    let errors: string[] | undefined;
    try {
      const body = (await res.json()) as { message?: string; errors?: string[] };
      message = body.message ?? message;
      errors = body.errors;
    } catch {
      // ignore JSON parse failure
    }
    throw new ApiError(res.status, message, errors);
  }

  const json: unknown = await res.json();
  return schema.parse(json);
}

// ── Health ───────────────────────────────────────────────────────────────────

/** GET /api/v1/health */
export async function getHealth() {
  return fetchJson(healthResponseSchema, "/health");
}

// ── Scenario templates ────────────────────────────────────────────────────────

/** GET /api/v1/scenarios/templates */
export async function getScenarioTemplates() {
  return fetchJson(scenarioTemplateSchema.array(), "/scenarios/templates");
}

// ── Runs ─────────────────────────────────────────────────────────────────────

/** POST /api/v1/runs — trigger a new run */
export async function createRun(body: CreateRunRequest) {
  return fetchJson(runSchema, "/runs", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

/** GET /api/v1/runs — list runs (most recent first) */
export async function listRuns(opts?: { limit?: number; status?: RunStatus }) {
  const params = new URLSearchParams();
  if (opts?.limit) params.set("limit", String(opts.limit));
  if (opts?.status) params.set("status", opts.status);
  const qs = params.size > 0 ? `?${params.toString()}` : "";
  return fetchJson(listRunsResponseSchema, `/runs${qs}`);
}

/** GET /api/v1/runs/{id} — get run detail */
export async function getRun(id: string) {
  return fetchJson(runSchema, `/runs/${encodeURIComponent(id)}`);
}

/** POST /api/v1/runs/{id}/cancel */
export async function cancelRun(id: string) {
  return fetchJson(cancelRunResponseSchema, `/runs/${encodeURIComponent(id)}/cancel`, {
    method: "POST",
  });
}

// ── Results ───────────────────────────────────────────────────────────────────

/** GET /api/v1/runs/{id}/results — full summary.json */
export async function getRunResults(id: string) {
  return fetchJson(runSummarySchema, `/runs/${encodeURIComponent(id)}/results`);
}

/** GET /api/v1/runs/{id}/results/establishment-curve */
export async function getEstablishmentCurve(id: string) {
  return fetchJson(
    establishmentCurveSchema,
    `/runs/${encodeURIComponent(id)}/results/establishment-curve`
  );
}

/** GET /api/v1/runs/{id}/results/latency-histogram */
export async function getLatencyHistogram(id: string) {
  return fetchJson(
    latencyHistogramSchema,
    `/runs/${encodeURIComponent(id)}/results/latency-histogram`
  );
}

// ── Artifacts ────────────────────────────────────────────────────────────────

/** GET /api/v1/runs/{id}/artifacts */
export async function getArtifacts(id: string) {
  return fetchJson(
    artifactsResponseSchema,
    `/runs/${encodeURIComponent(id)}/artifacts`
  );
}

/** Build a direct download URL for an artifact */
export function artifactDownloadUrl(runId: string, name: string): string {
  return `${API_BASE}${PREFIX}/runs/${encodeURIComponent(runId)}/artifacts/${encodeURIComponent(name)}`;
}

// ── SSE ───────────────────────────────────────────────────────────────────────

/**
 * Subscribe to run progress SSE stream.
 * Returns an EventSource instance — caller is responsible for closing it.
 * Full parsing/typing is deferred to Wave 3.
 */
export function subscribeRunEvents(id: string, afterMs = 0): EventSource {
  const url = `${API_BASE}${PREFIX}/runs/${encodeURIComponent(id)}/events?after=${afterMs}`;
  return new EventSource(url);
}

// Re-export error class for callers
export { ApiError };
