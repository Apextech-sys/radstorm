/**
 * TypeScript types for radstorm run results (summary.json schema).
 *
 * Purpose:
 *   Mirrors the results contract from .orchestration/contracts/results-schema.md.
 *   Used by the results dashboard, charts, and API client to type the
 *   summary.json payload returned by GET /api/v1/runs/{id}/results.
 *
 * Related files:
 *   - .orchestration/contracts/results-schema.md (canonical)
 *   - src/lib/api.ts (typed fetch wrappers)
 *   - src/app/runs/[id]/page.tsx (renders result data, Wave 3)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: Exports result TypeScript types and zod schemas.
 */

import { z } from "zod";

// ── Latency distribution ─────────────────────────────────────────────────────

export const latencyDistMsSchema = z.object({
  min: z.number(),
  p50: z.number(),
  p95: z.number(),
  p99: z.number(),
  p999: z.number(),
  max: z.number(),
});
export type LatencyDistMs = z.infer<typeof latencyDistMsSchema>;

// ── Establishment curve point ────────────────────────────────────────────────

export const curvePointSchema = z.object({
  offset_ms: z.number(),
  activated: z.number(),
  established: z.number(),
  in_flight: z.number(),
});
export type CurvePoint = z.infer<typeof curvePointSchema>;

// ── Establishment curve (pre-computed endpoint) ───────────────────────────────

export const establishmentCurveSchema = z.object({
  interval_ms: z.number(),
  points: z.array(curvePointSchema),
});
export type EstablishmentCurve = z.infer<typeof establishmentCurveSchema>;

// ── Latency histogram ────────────────────────────────────────────────────────

export const latencyBucketSchema = z.object({
  le_ms: z.number(),
  count: z.number(),
});
export const latencyHistogramSchema = z.object({
  buckets: z.array(latencyBucketSchema),
});
export type LatencyHistogram = z.infer<typeof latencyHistogramSchema>;

// ── Summary sections ─────────────────────────────────────────────────────────

export const subscriberStatsSchema = z.object({
  total: z.number(),
  established: z.number(),
  auth_failed: z.number(),
  acct_failed: z.number(),
  terminated: z.number(),
  still_in_flight_at_end: z.number(),
});

export const establishmentStatsSchema = z.object({
  time_to_first_ms: z.number(),
  time_to_full_ms: z.number(),
  latency_ms: latencyDistMsSchema,
  curve: z.array(curvePointSchema),
});

export const retransmitStatsSchema = z.object({
  total: z.number(),
  subscribers_with_retransmit: z.number(),
  per_subscriber_distribution: z.object({
    p50: z.number(),
    p95: z.number(),
    p99: z.number(),
    max: z.number(),
  }),
});

export const coaStatsSchema = z.object({
  received: z.number(),
  acked: z.number(),
  naked: z.number(),
  dropped: z.number(),
  response_latency_us: latencyDistMsSchema.nullable(),
});

export const serverHealthSchema = z.object({
  unresponsive_periods: z.array(z.unknown()),
  error_responses: z.number(),
});

export const thresholdResultSchema = z.object({
  name: z.string(),
  expected: z.string(),
  actual: z.union([z.string(), z.number()]),
  pass: z.boolean(),
});

export const thresholdsSchema = z.object({
  evaluated: z.array(thresholdResultSchema),
  overall: z.enum(["pass", "fail"]),
});

export const artifactsSchema = z.object({
  events_parquet: z.string().optional(),
  subscribers_parquet: z.string().optional(),
  run_log: z.string().optional(),
});

// ── Full summary.json schema ─────────────────────────────────────────────────

export const runSummarySchema = z.object({
  run_id: z.string(),
  config_hash: z.string(),
  started_at: z.string(),
  finished_at: z.string(),
  duration_ms: z.number(),
  scenario_type: z.string(),
  outcome: z.enum(["succeeded", "failed", "cancelled"]),
  subscribers: subscriberStatsSchema,
  establishment: establishmentStatsSchema,
  retransmits: retransmitStatsSchema,
  coa: coaStatsSchema,
  disconnect: coaStatsSchema,
  server_health: serverHealthSchema,
  thresholds: thresholdsSchema,
  artifacts: artifactsSchema,
});

export type RunSummary = z.infer<typeof runSummarySchema>;
export type SubscriberStats = z.infer<typeof subscriberStatsSchema>;
export type EstablishmentStats = z.infer<typeof establishmentStatsSchema>;
export type RetransmitStats = z.infer<typeof retransmitStatsSchema>;
export type CoaStats = z.infer<typeof coaStatsSchema>;
export type ThresholdResult = z.infer<typeof thresholdResultSchema>;
