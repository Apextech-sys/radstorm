/**
 * TypeScript types for radstorm REST API payloads.
 *
 * Purpose:
 *   Zod schemas and TypeScript types for every request/response shape
 *   defined in .orchestration/contracts/rest-api.md. The API client
 *   (src/lib/api.ts) validates all responses against these schemas.
 *
 * Related files:
 *   - .orchestration/contracts/rest-api.md (canonical)
 *   - src/lib/api.ts (uses these schemas for response validation)
 *   - src/lib/types/config.ts (Config type used in Run)
 *   - src/lib/types/results.ts (RunSummary used in Run)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: Exports Run, RunProgress, HealthResponse, SSE event types.
 */

import { z } from "zod";
import { configSchema } from "./config";
import { runSummarySchema } from "./results";

// ── Run status ───────────────────────────────────────────────────────────────

export const runStatusSchema = z.enum([
  "queued",
  "running",
  "succeeded",
  "failed",
  "cancelling",
  "cancelled",
]);
export type RunStatus = z.infer<typeof runStatusSchema>;

// ── Run progress ─────────────────────────────────────────────────────────────

export const runProgressSchema = z.object({
  elapsed_ms: z.number(),
  subscribers_total: z.number(),
  subscribers_activated: z.number(),
  subscribers_established: z.number(),
  subscribers_failed: z.number(),
});
export type RunProgress = z.infer<typeof runProgressSchema>;

// ── Run object ───────────────────────────────────────────────────────────────

export const runSchema = z.object({
  id: z.string(),
  name: z.string().optional(),
  status: runStatusSchema,
  created_at: z.string(),
  started_at: z.string().nullable().optional(),
  finished_at: z.string().nullable().optional(),
  config: configSchema.optional(),
  progress: runProgressSchema.nullable().optional(),
  summary: runSummarySchema.nullable().optional(),
});
export type Run = z.infer<typeof runSchema>;

// ── List runs response ───────────────────────────────────────────────────────

export const listRunsResponseSchema = z.object({
  runs: z.array(runSchema),
});
export type ListRunsResponse = z.infer<typeof listRunsResponseSchema>;

// ── Create run request / response ────────────────────────────────────────────

export const createRunRequestSchema = z.object({
  name: z.string().optional(),
  config: configSchema,
});
export type CreateRunRequest = z.infer<typeof createRunRequestSchema>;

// ── Health response ──────────────────────────────────────────────────────────

export const healthResponseSchema = z.object({
  status: z.string(),
  version: z.string(),
});
export type HealthResponse = z.infer<typeof healthResponseSchema>;

// ── Scenario template ────────────────────────────────────────────────────────

export const scenarioTemplateSchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string(),
  config: configSchema.partial(),
});
export type ScenarioTemplate = z.infer<typeof scenarioTemplateSchema>;

// ── Cancel run response ──────────────────────────────────────────────────────

export const cancelRunResponseSchema = z.object({
  id: z.string(),
  status: runStatusSchema,
});
export type CancelRunResponse = z.infer<typeof cancelRunResponseSchema>;

// ── Artifact ─────────────────────────────────────────────────────────────────

export const artifactSchema = z.object({
  name: z.string(),
  size: z.number(),
  url: z.string(),
});
export const artifactsResponseSchema = z.object({
  artifacts: z.array(artifactSchema),
});
export type Artifact = z.infer<typeof artifactSchema>;

// ── SSE events ───────────────────────────────────────────────────────────────

export interface SseProgressEvent {
  offset_ms: number;
  activated: number;
  established: number;
  failed: number;
  in_flight: number;
  retransmits: number;
}

export interface SseLogEvent {
  offset_ms: number;
  level: "debug" | "info" | "warn" | "error";
  msg: string;
}

export interface SseCompleteEvent {
  summary: z.infer<typeof runSummarySchema>;
}

export type SseEvent =
  | { type: "progress"; data: SseProgressEvent }
  | { type: "log"; data: SseLogEvent }
  | { type: "complete"; data: SseCompleteEvent };

// ── API error ────────────────────────────────────────────────────────────────

export interface ApiError {
  status: number;
  message: string;
  errors?: string[];
}
