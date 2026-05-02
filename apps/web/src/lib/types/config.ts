/**
 * TypeScript types and zod schemas mirroring the radstorm Config contract.
 *
 * Purpose:
 *   Provides fully-typed, runtime-validated Config and sub-struct types for
 *   the frontend scenario configuration form and API client. Every field
 *   mirrors the Go struct in .orchestration/contracts/config-schema.md.
 *
 * Related files:
 *   - .orchestration/contracts/config-schema.md (canonical source of truth)
 *   - src/lib/api.ts (uses Config for POST /runs request body)
 *   - src/app/runs/new/page.tsx (config form, Wave 2)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: Exports `Config` TypeScript type and `configSchema` zod schema.
 *           Changes here must be coordinated with config-schema.md.
 */

import { z } from "zod";

// ── Sub-schemas ──────────────────────────────────────────────────────────────

export const targetSchema = z.object({
  auth_address: z.string().min(1, "Auth address required"),
  acct_address: z.string().min(1, "Acct address required"),
  shared_secret: z.string().min(1, "Shared secret required"),
});

export const coaListenerSchema = z.object({
  bind_address: z.string().min(1, "Bind address required"),
  shared_secret: z.string().min(1, "Shared secret required"),
});

export const subscribersSchema = z.object({
  count: z.number().int().min(1).max(1_000_000),
  credentials_file: z.string().min(1, "Credentials file required"),
  auth_method_pap_pct: z.number().int().min(0).max(100).default(100),
  type_pppoe_pct: z.number().int().min(0).max(100).default(100),
  include_acct_start: z.boolean().default(true),
});

export const sourceSchema = z.object({
  ips: z.array(z.ipv4().or(z.ipv6())).min(1, "At least one source IP required"),
  port_range: z
    .tuple([z.number().int().min(1).max(65535), z.number().int().min(1).max(65535)])
    .default([10000, 60000]),
});

export const nasSchema = z.object({
  ip_address: z.ipv4().or(z.ipv6()),
  identifier: z.string().min(1, "NAS identifier required"),
  huawei: z.record(z.string(), z.string()).optional(),
});

export const retransmitSchema = z.object({
  initial_timeout_ms: z.number().int().min(0).default(5000),
  max_retries: z.number().int().min(0).default(3),
  backoff: z.enum(["exponential", "linear", "constant"]).default("exponential"),
  backoff_base_ms: z.number().int().min(0).default(1000),
});

export const coldStartCfgSchema = z.object({
  mu_sec: z.number().positive(),
  sigma_sec: z.number().positive(),
  truncate_sigma: z.number().min(1),
});

export const uniformCfgSchema = z.object({
  duration_sec: z.number().positive(),
});

export const pessimalCfgSchema = z.object({
  burst_window_ms: z.number().int().min(0),
});

export const coaStormCfgSchema = z.object({});

export const scenarioSchema = z.object({
  type: z.enum(["cold_start", "uniform", "pessimal", "coa_storm"]),
  hard_timeout_sec: z.number().int().min(0).default(300),
  cold_start: coldStartCfgSchema.optional(),
  uniform: uniformCfgSchema.optional(),
  pessimal: pessimalCfgSchema.optional(),
  coa_storm: coaStormCfgSchema.optional(),
});

export const outputSchema = z.object({
  directory: z.string().min(1).default("results"),
  flush_interval_sec: z.number().int().min(0).default(30),
});

// ── Root config schema ───────────────────────────────────────────────────────

export const configSchema = z.object({
  target: targetSchema,
  coa_listener: coaListenerSchema.optional(),
  subscribers: subscribersSchema,
  source: sourceSchema,
  nas: nasSchema,
  retransmit: retransmitSchema.optional(),
  scenario: scenarioSchema,
  output: outputSchema.optional(),
});

// ── TypeScript types ─────────────────────────────────────────────────────────

export type Target = z.infer<typeof targetSchema>;
export type CoaListener = z.infer<typeof coaListenerSchema>;
export type Subscribers = z.infer<typeof subscribersSchema>;
export type Source = z.infer<typeof sourceSchema>;
export type NAS = z.infer<typeof nasSchema>;
export type Retransmit = z.infer<typeof retransmitSchema>;
export type ColdStartCfg = z.infer<typeof coldStartCfgSchema>;
export type UniformCfg = z.infer<typeof uniformCfgSchema>;
export type PessimalCfg = z.infer<typeof pessimalCfgSchema>;
export type CoaStormCfg = z.infer<typeof coaStormCfgSchema>;
export type Scenario = z.infer<typeof scenarioSchema>;
export type Output = z.infer<typeof outputSchema>;
export type Config = z.infer<typeof configSchema>;

// ── Defaults helper ──────────────────────────────────────────────────────────

/** Returns a minimal valid Config with all defaults applied, ready to pre-fill a form. */
export function defaultConfig(): Partial<Config> {
  return {
    retransmit: {
      initial_timeout_ms: 5000,
      max_retries: 3,
      backoff: "exponential",
      backoff_base_ms: 1000,
    },
    source: {
      ips: [],
      port_range: [10000, 60000],
    },
    subscribers: {
      count: 1000,
      credentials_file: "",
      auth_method_pap_pct: 100,
      type_pppoe_pct: 100,
      include_acct_start: true,
    },
    output: {
      directory: "results",
      flush_interval_sec: 30,
    },
    scenario: {
      type: "uniform",
      hard_timeout_sec: 300,
    },
  };
}
