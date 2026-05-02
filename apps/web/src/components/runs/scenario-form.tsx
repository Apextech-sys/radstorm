/**
 * ScenarioForm — full scenario configuration form for /runs/new.
 *
 * Purpose:
 *   React-hook-form + zod form covering all Config sections:
 *   Target, CoA listener, Subscribers (with count slider and presets),
 *   Source, NAS, Retransmit, Scenario (with conditional sub-sections),
 *   and Output. Submits POST /api/v1/runs and navigates to /runs/{id}.
 *
 * Related files:
 *   - src/lib/types/config.ts (configSchema, defaultConfig)
 *   - src/lib/api.ts (createRun, getScenarioTemplates)
 *   - src/app/runs/new/page.tsx (page wrapper)
 *   - src/components/runs/form-section.tsx (FormSection, FieldRow, FieldGroup)
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
 *
 * Contract: Exports ScenarioForm component.
 */

"use client";

import { useState, useEffect, useCallback } from "react";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { toast } from "sonner";
import { Loader2, Play } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Slider } from "@/components/ui/slider";
import { Checkbox } from "@/components/ui/checkbox";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { FormSection, FieldRow, FieldGroup } from "@/components/runs/form-section";

import {
  configSchema,
  defaultConfig,
} from "@/lib/types/config";
import type { z } from "zod";

// Use the input type (with defaults as optional) for the form
type FormConfig = z.input<typeof configSchema>;
import { createRun, getScenarioTemplates, ApiError } from "@/lib/api";
import type { ScenarioTemplate } from "@/lib/types/api";
import { SUBSCRIBER_PRESETS } from "@/lib/format";
import { cn } from "@/lib/utils";

// ── Helpers ──────────────────────────────────────────────────────────────────

function getDefaultValues(): FormConfig {
  const d = defaultConfig();
  return {
    target: { auth_address: "127.0.0.1:1812", acct_address: "127.0.0.1:1813", shared_secret: "testing123" },
    coa_listener: { bind_address: "0.0.0.0:3799", shared_secret: "testing123" },
    subscribers: {
      count: d.subscribers?.count ?? 1000,
      credentials_file: "credentials.csv",
      auth_method_pap_pct: 100,
      type_pppoe_pct: 100,
      include_acct_start: true,
    },
    source: { ips: ["127.0.0.1"], port_range: [10000, 60000] },
    nas: { ip_address: "127.0.0.1", identifier: "nas-01" },
    retransmit: {
      initial_timeout_ms: 5000,
      max_retries: 3,
      backoff: "exponential",
      backoff_base_ms: 1000,
    },
    scenario: {
      type: "uniform",
      hard_timeout_sec: 300,
      uniform: { duration_sec: 60 },
    },
    output: { directory: "results", flush_interval_sec: 30 },
  };
}

// ── Component ─────────────────────────────────────────────────────────────────

export function ScenarioForm() {
  const router = useRouter();
  const [templates, setTemplates] = useState<ScenarioTemplate[]>([]);
  const [loadingTemplates, setLoadingTemplates] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [runName, setRunName] = useState("");
  const [subscriberInput, setSubscriberInput] = useState("1000");

  const {
    register,
    handleSubmit,
    setValue,
    watch,
    formState: { errors },
  } = useForm<FormConfig>({
    resolver: zodResolver(configSchema),
    defaultValues: getDefaultValues(),
  });

  const scenarioType = watch("scenario.type");
  const subscriberCount = watch("subscribers.count");
  const sourceIPs = watch("source.ips");

  // Load templates
  useEffect(() => {
    getScenarioTemplates()
      .then(setTemplates)
      .catch(() => { /* templates are optional */ })
      .finally(() => setLoadingTemplates(false));
  }, []);

  // Keep subscriber input in sync with slider
  useEffect(() => {
    setSubscriberInput(String(subscriberCount));
  }, [subscriberCount]);

  const applyTemplate = useCallback(
    (template: ScenarioTemplate) => {
      const cfg = template.config as Partial<FormConfig>;
      if (cfg.target) {
        if (cfg.target.auth_address) setValue("target.auth_address", cfg.target.auth_address);
        if (cfg.target.acct_address) setValue("target.acct_address", cfg.target.acct_address);
        if (cfg.target.shared_secret) setValue("target.shared_secret", cfg.target.shared_secret);
      }
      if (cfg.subscribers?.count) setValue("subscribers.count", cfg.subscribers.count);
      if (cfg.scenario?.type) {
        setValue("scenario.type", cfg.scenario.type);
        if (cfg.scenario.hard_timeout_sec) setValue("scenario.hard_timeout_sec", cfg.scenario.hard_timeout_sec);
        if (cfg.scenario.uniform) setValue("scenario.uniform", cfg.scenario.uniform);
        if (cfg.scenario.cold_start) setValue("scenario.cold_start", cfg.scenario.cold_start);
        if (cfg.scenario.pessimal) setValue("scenario.pessimal", cfg.scenario.pessimal);
      }
      toast.success(`Applied template: ${template.name}`);
    },
    [setValue]
  );

  const onSubmit = async (data: FormConfig) => {
    setSubmitting(true);
    try {
      // configSchema.parse ensures all defaults are applied; cast is safe
      const parsed = configSchema.parse(data);
      const run = await createRun({ name: runName.trim() || undefined, config: parsed });
      toast.success("Run started");
      router.push(`/runs/${run.id}`);
    } catch (err) {
      if (err instanceof ApiError) {
        toast.error(`Failed to start run: ${err.message}`);
      } else {
        toast.error("Unexpected error starting run");
      }
      setSubmitting(false);
    }
  };

  return (
    <form onSubmit={handleSubmit(onSubmit)} className="space-y-6" noValidate>
      {/* Run name */}
      <div className="flex items-center gap-3">
        <div className="flex-1">
          <Input
            placeholder="Run name (optional)"
            value={runName}
            onChange={(e) => setRunName(e.target.value)}
            className="h-8 text-sm"
          />
        </div>
        <Button type="submit" size="sm" disabled={submitting} className="gap-1.5">
          {submitting ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <Play className="h-3.5 w-3.5" />
          )}
          Start Run
        </Button>
      </div>

      {/* Template picker */}
      <div>
        <p className="mb-2 text-xs font-medium text-muted-foreground uppercase tracking-wide">
          Start from template
        </p>
        {loadingTemplates ? (
          <div className="flex gap-2">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-7 w-24 rounded-lg" />
            ))}
          </div>
        ) : templates.length > 0 ? (
          <div className="flex flex-wrap gap-2">
            {templates.map((t) => (
              <button
                key={t.id}
                type="button"
                onClick={() => applyTemplate(t)}
                className="inline-flex items-center gap-1 rounded-lg border border-border bg-muted/50 px-3 py-1 text-xs text-muted-foreground transition-colors hover:border-signal/40 hover:bg-signal-muted hover:text-signal"
              >
                {t.name}
              </button>
            ))}
          </div>
        ) : null}
      </div>

      <Separator />

      {/* ── Target section ─────────────────────────────────────────────────── */}
      <FormSection
        title="Target"
        description="RADIUS server endpoints and shared secret"
      >
        <FieldRow columns={2}>
          <FieldGroup
            label="Auth address"
            htmlFor="auth_address"
            hint="host:port for Access-Request packets"
            error={errors.target?.auth_address?.message}
            required
          >
            <Input
              id="auth_address"
              {...register("target.auth_address")}
              placeholder="127.0.0.1:1812"
              className="font-mono text-xs"
            />
          </FieldGroup>
          <FieldGroup
            label="Acct address"
            htmlFor="acct_address"
            hint="host:port for Accounting-Request packets"
            error={errors.target?.acct_address?.message}
            required
          >
            <Input
              id="acct_address"
              {...register("target.acct_address")}
              placeholder="127.0.0.1:1813"
              className="font-mono text-xs"
            />
          </FieldGroup>
        </FieldRow>
        <FieldGroup
          label="Shared secret"
          htmlFor="shared_secret"
          hint="RADIUS shared secret for packet authentication"
          error={errors.target?.shared_secret?.message}
          required
        >
          <Input
            id="shared_secret"
            type="password"
            {...register("target.shared_secret")}
            placeholder="testing123"
            className="font-mono text-xs max-w-xs"
          />
        </FieldGroup>
      </FormSection>

      {/* ── CoA Listener section ───────────────────────────────────────────── */}
      <FormSection
        title="CoA Listener"
        description="Bind address for incoming CoA and Disconnect packets"
      >
        <FieldRow columns={2}>
          <FieldGroup
            label="Bind address"
            htmlFor="coa_bind"
            hint="UDP address to listen for CoA/DM"
            error={errors.coa_listener?.bind_address?.message}
          >
            <Input
              id="coa_bind"
              {...register("coa_listener.bind_address")}
              placeholder="0.0.0.0:3799"
              className="font-mono text-xs"
            />
          </FieldGroup>
          <FieldGroup
            label="Shared secret"
            htmlFor="coa_secret"
            hint="Matches the NAS CoA shared secret"
            error={errors.coa_listener?.shared_secret?.message}
          >
            <Input
              id="coa_secret"
              type="password"
              {...register("coa_listener.shared_secret")}
              placeholder="testing123"
              className="font-mono text-xs"
            />
          </FieldGroup>
        </FieldRow>
      </FormSection>

      {/* ── Subscribers section ────────────────────────────────────────────── */}
      <FormSection
        title="Subscribers"
        description="Virtual subscriber population and authentication parameters"
      >
        {/* Subscriber count slider */}
        <div className="space-y-3">
          <div className="flex items-center justify-between">
            <label className="text-xs font-medium text-foreground">
              Subscriber count
              <span className="ml-1 text-destructive">*</span>
            </label>
            <Input
              type="number"
              value={subscriberInput}
              onChange={(e) => {
                setSubscriberInput(e.target.value);
                const n = parseInt(e.target.value, 10);
                if (!isNaN(n) && n >= 1 && n <= 1_000_000) {
                  setValue("subscribers.count", n, { shouldValidate: true });
                }
              }}
              className="h-7 w-28 text-right font-mono text-xs tabular-nums"
              min={1}
              max={1_000_000}
            />
          </div>

          <Slider
            min={1}
            max={1_000_000}
            step={100}
            value={subscriberCount ?? 1000}
            onValueChange={(v) => {
              const n = Array.isArray(v) ? v[0] : v;
              if (typeof n === "number") {
                setValue("subscribers.count", n, { shouldValidate: true });
              }
            }}
            className="w-full"
            aria-label="Subscriber count"
          />

          {/* Quick presets */}
          <div className="flex gap-1.5">
            {SUBSCRIBER_PRESETS.map((preset) => (
              <button
                key={preset.value}
                type="button"
                onClick={() => {
                  setValue("subscribers.count", preset.value, { shouldValidate: true });
                }}
                className={cn(
                  "rounded-md border px-2.5 py-0.5 font-mono text-xs transition-colors",
                  subscriberCount === preset.value
                    ? "border-signal bg-signal-muted text-signal"
                    : "border-border bg-muted/50 text-muted-foreground hover:border-signal/40 hover:text-foreground"
                )}
              >
                {preset.label}
              </button>
            ))}
          </div>
          {errors.subscribers?.count && (
            <p className="text-[11px] font-medium text-destructive">
              {errors.subscribers.count.message}
            </p>
          )}
        </div>

        <FieldGroup
          label="Credentials file"
          htmlFor="cred_file"
          hint="Path to CSV with username, password columns"
          error={errors.subscribers?.credentials_file?.message}
          required
        >
          <Input
            id="cred_file"
            {...register("subscribers.credentials_file")}
            placeholder="credentials.csv"
            className="font-mono text-xs max-w-sm"
          />
        </FieldGroup>

        <FieldRow columns={2}>
          <FieldGroup
            label="PAP percentage"
            htmlFor="pap_pct"
            hint="% of subscribers using PAP auth (rest use CHAP)"
            error={errors.subscribers?.auth_method_pap_pct?.message}
          >
            <div className="flex items-center gap-2">
              <Input
                id="pap_pct"
                type="number"
                {...register("subscribers.auth_method_pap_pct", { valueAsNumber: true })}
                min={0}
                max={100}
                className="w-20 font-mono text-xs"
              />
              <span className="text-xs text-muted-foreground">%</span>
            </div>
          </FieldGroup>
          <FieldGroup
            label="PPPoE percentage"
            htmlFor="pppoe_pct"
            hint="% of subscribers using PPPoE (rest use MAC)"
            error={errors.subscribers?.type_pppoe_pct?.message}
          >
            <div className="flex items-center gap-2">
              <Input
                id="pppoe_pct"
                type="number"
                {...register("subscribers.type_pppoe_pct", { valueAsNumber: true })}
                min={0}
                max={100}
                className="w-20 font-mono text-xs"
              />
              <span className="text-xs text-muted-foreground">%</span>
            </div>
          </FieldGroup>
        </FieldRow>

        <div className="flex items-center gap-2">
          <Checkbox
            id="acct_start"
            checked={watch("subscribers.include_acct_start")}
            onCheckedChange={(checked) =>
              setValue("subscribers.include_acct_start", Boolean(checked))
            }
          />
          <label htmlFor="acct_start" className="cursor-pointer text-xs text-foreground">
            Include Accounting-Start after successful auth
          </label>
        </div>
      </FormSection>

      {/* ── Source section ─────────────────────────────────────────────────── */}
      <FormSection
        title="Source"
        description="Source IP addresses and UDP port range for outbound packets"
      >
        <FieldGroup
          label="Source IPs"
          htmlFor="source_ips"
          hint="Comma-separated list of source IP addresses"
          error={errors.source?.ips?.message}
          required
        >
          <Input
            id="source_ips"
            value={sourceIPs?.join(", ") ?? ""}
            onChange={(e) => {
              const raw = e.target.value;
              const ips = raw.split(",").map((s) => s.trim()).filter(Boolean);
              setValue("source.ips", ips, { shouldValidate: true });
            }}
            placeholder="127.0.0.1, 192.168.0.1"
            className="font-mono text-xs"
          />
        </FieldGroup>
        <FieldRow columns={2}>
          <FieldGroup
            label="Port range low"
            htmlFor="port_low"
            hint="Lowest UDP source port"
            error={errors.source?.port_range?.message}
          >
            <Input
              id="port_low"
              type="number"
              {...register("source.port_range.0", { valueAsNumber: true })}
              min={1}
              max={65535}
              className="font-mono text-xs w-28"
            />
          </FieldGroup>
          <FieldGroup
            label="Port range high"
            htmlFor="port_high"
            hint="Highest UDP source port"
          >
            <Input
              id="port_high"
              type="number"
              {...register("source.port_range.1", { valueAsNumber: true })}
              min={1}
              max={65535}
              className="font-mono text-xs w-28"
            />
          </FieldGroup>
        </FieldRow>
      </FormSection>

      {/* ── NAS section ────────────────────────────────────────────────────── */}
      <FormSection
        title="NAS"
        description="Network Access Server identity presented in RADIUS packets"
      >
        <FieldRow columns={2}>
          <FieldGroup
            label="NAS IP address"
            htmlFor="nas_ip"
            hint="NAS-IP-Address attribute value"
            error={errors.nas?.ip_address?.message}
            required
          >
            <Input
              id="nas_ip"
              {...register("nas.ip_address")}
              placeholder="192.168.0.1"
              className="font-mono text-xs"
            />
          </FieldGroup>
          <FieldGroup
            label="NAS identifier"
            htmlFor="nas_id"
            hint="NAS-Identifier attribute value"
            error={errors.nas?.identifier?.message}
            required
          >
            <Input
              id="nas_id"
              {...register("nas.identifier")}
              placeholder="nas-01"
              className="font-mono text-xs"
            />
          </FieldGroup>
        </FieldRow>
      </FormSection>

      {/* ── Retransmit section ─────────────────────────────────────────────── */}
      <FormSection
        title="Retransmit"
        description="Timeout and retry policy for unanswered Access-Requests"
      >
        <FieldRow columns={2}>
          <FieldGroup
            label="Initial timeout"
            htmlFor="init_timeout"
            hint="Milliseconds before first retry"
            error={errors.retransmit?.initial_timeout_ms?.message}
          >
            <div className="flex items-center gap-2">
              <Input
                id="init_timeout"
                type="number"
                {...register("retransmit.initial_timeout_ms", { valueAsNumber: true })}
                min={0}
                className="w-24 font-mono text-xs"
              />
              <span className="text-xs text-muted-foreground">ms</span>
            </div>
          </FieldGroup>
          <FieldGroup
            label="Max retries"
            htmlFor="max_retries"
            hint="0 = no retransmit"
            error={errors.retransmit?.max_retries?.message}
          >
            <Input
              id="max_retries"
              type="number"
              {...register("retransmit.max_retries", { valueAsNumber: true })}
              min={0}
              className="w-20 font-mono text-xs"
            />
          </FieldGroup>
        </FieldRow>
        <FieldRow columns={2}>
          <FieldGroup
            label="Backoff strategy"
            htmlFor="backoff"
            error={errors.retransmit?.backoff?.message}
          >
            <select
              id="backoff"
              {...register("retransmit.backoff")}
              className="flex h-8 w-full rounded-lg border border-input bg-background px-3 py-1 text-xs text-foreground transition-colors focus:outline-none focus:ring-2 focus:ring-ring"
            >
              <option value="exponential">Exponential</option>
              <option value="linear">Linear</option>
              <option value="constant">Constant</option>
            </select>
          </FieldGroup>
          <FieldGroup
            label="Backoff base"
            htmlFor="backoff_base"
            hint="Base interval in milliseconds"
            error={errors.retransmit?.backoff_base_ms?.message}
          >
            <div className="flex items-center gap-2">
              <Input
                id="backoff_base"
                type="number"
                {...register("retransmit.backoff_base_ms", { valueAsNumber: true })}
                min={0}
                className="w-24 font-mono text-xs"
              />
              <span className="text-xs text-muted-foreground">ms</span>
            </div>
          </FieldGroup>
        </FieldRow>
      </FormSection>

      {/* ── Scenario section ───────────────────────────────────────────────── */}
      <FormSection
        title="Scenario"
        description="Activation pattern and hard timeout for this run"
      >
        {/* Scenario type radio group */}
        <div>
          <p className="mb-2 text-xs font-medium text-foreground">
            Scenario type <span className="text-destructive">*</span>
          </p>
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
            {(
              [
                { value: "uniform", label: "Uniform", desc: "Even ramp over a fixed window" },
                { value: "cold_start", label: "Cold Start", desc: "Gaussian burst (power outage)" },
                { value: "pessimal", label: "Pessimal", desc: "Simultaneous burst worst case" },
                { value: "coa_storm", label: "CoA Storm", desc: "Bulk change-of-authorization" },
              ] as const
            ).map((opt) => (
              <label
                key={opt.value}
                className={cn(
                  "flex cursor-pointer flex-col gap-0.5 rounded-lg border p-3 text-xs transition-colors",
                  scenarioType === opt.value
                    ? "border-signal bg-signal-muted text-signal"
                    : "border-border bg-muted/30 text-muted-foreground hover:border-border hover:bg-muted/60 hover:text-foreground"
                )}
              >
                <input
                  type="radio"
                  {...register("scenario.type")}
                  value={opt.value}
                  className="sr-only"
                />
                <span className="font-semibold">{opt.label}</span>
                <span className="leading-snug">{opt.desc}</span>
              </label>
            ))}
          </div>
          {errors.scenario?.type && (
            <p className="mt-1 text-[11px] font-medium text-destructive">
              {errors.scenario.type.message}
            </p>
          )}
        </div>

        {/* Hard timeout */}
        <FieldGroup
          label="Hard timeout"
          htmlFor="hard_timeout"
          hint="Maximum run duration in seconds"
          error={errors.scenario?.hard_timeout_sec?.message}
        >
          <div className="flex items-center gap-2">
            <Input
              id="hard_timeout"
              type="number"
              {...register("scenario.hard_timeout_sec", { valueAsNumber: true })}
              min={0}
              className="w-24 font-mono text-xs"
            />
            <span className="text-xs text-muted-foreground">sec</span>
          </div>
        </FieldGroup>

        {/* Conditional sub-forms */}
        {scenarioType === "uniform" && (
          <div className="rounded-lg border border-border/50 bg-muted/20 p-4">
            <p className="mb-3 text-xs font-semibold text-foreground">Uniform parameters</p>
            <FieldGroup
              label="Duration"
              htmlFor="uniform_dur"
              hint="Time window over which all subscribers activate evenly"
              error={errors.scenario?.uniform?.duration_sec?.message}
            >
              <div className="flex items-center gap-2">
                <Input
                  id="uniform_dur"
                  type="number"
                  {...register("scenario.uniform.duration_sec", { valueAsNumber: true })}
                  min={0.1}
                  step={0.1}
                  className="w-24 font-mono text-xs"
                />
                <span className="text-xs text-muted-foreground">sec</span>
              </div>
            </FieldGroup>
          </div>
        )}

        {scenarioType === "cold_start" && (
          <div className="rounded-lg border border-border/50 bg-muted/20 p-4">
            <p className="mb-3 text-xs font-semibold text-foreground">Cold Start parameters</p>
            <FieldRow columns={3}>
              <FieldGroup
                label="μ (mean)"
                htmlFor="cs_mu"
                hint="Mean activation time (sec)"
                error={errors.scenario?.cold_start?.mu_sec?.message}
              >
                <Input
                  id="cs_mu"
                  type="number"
                  {...register("scenario.cold_start.mu_sec", { valueAsNumber: true })}
                  min={0.001}
                  step={0.1}
                  className="font-mono text-xs"
                />
              </FieldGroup>
              <FieldGroup
                label="σ (std dev)"
                htmlFor="cs_sigma"
                hint="Standard deviation (sec)"
                error={errors.scenario?.cold_start?.sigma_sec?.message}
              >
                <Input
                  id="cs_sigma"
                  type="number"
                  {...register("scenario.cold_start.sigma_sec", { valueAsNumber: true })}
                  min={0.001}
                  step={0.1}
                  className="font-mono text-xs"
                />
              </FieldGroup>
              <FieldGroup
                label="Truncation σ"
                htmlFor="cs_trunc"
                hint="Clip Gaussian at N×σ from mean"
                error={errors.scenario?.cold_start?.truncate_sigma?.message}
              >
                <Input
                  id="cs_trunc"
                  type="number"
                  {...register("scenario.cold_start.truncate_sigma", { valueAsNumber: true })}
                  min={1}
                  step={0.5}
                  className="font-mono text-xs"
                />
              </FieldGroup>
            </FieldRow>
          </div>
        )}

        {scenarioType === "pessimal" && (
          <div className="rounded-lg border border-border/50 bg-muted/20 p-4">
            <p className="mb-3 text-xs font-semibold text-foreground">Pessimal parameters</p>
            <FieldGroup
              label="Burst window"
              htmlFor="burst_win"
              hint="All subscribers attempt within this window. 0 = truly simultaneous"
              error={errors.scenario?.pessimal?.burst_window_ms?.message}
            >
              <div className="flex items-center gap-2">
                <Input
                  id="burst_win"
                  type="number"
                  {...register("scenario.pessimal.burst_window_ms", { valueAsNumber: true })}
                  min={0}
                  className="w-24 font-mono text-xs"
                />
                <span className="text-xs text-muted-foreground">ms</span>
              </div>
            </FieldGroup>
          </div>
        )}

        {scenarioType === "coa_storm" && (
          <div className="rounded-lg border border-border/50 bg-muted/20 p-4">
            <p className="text-xs text-muted-foreground">
              CoA storm scenario captures inbound CoA and Disconnect messages from the NAS.
              The test is triggered externally (e.g. via <code className="font-mono">radclient</code>).
              No additional parameters needed.
            </p>
          </div>
        )}
      </FormSection>

      {/* ── Output section ─────────────────────────────────────────────────── */}
      <FormSection
        title="Output"
        description="Where to write results and how often to flush"
      >
        <FieldRow columns={2}>
          <FieldGroup
            label="Output directory"
            htmlFor="out_dir"
            hint="Relative path for Parquet + summary output"
            error={errors.output?.directory?.message}
          >
            <Input
              id="out_dir"
              {...register("output.directory")}
              placeholder="results"
              className="font-mono text-xs"
            />
          </FieldGroup>
          <FieldGroup
            label="Flush interval"
            htmlFor="flush_interval"
            hint="Parquet write interval in seconds"
            error={errors.output?.flush_interval_sec?.message}
          >
            <div className="flex items-center gap-2">
              <Input
                id="flush_interval"
                type="number"
                {...register("output.flush_interval_sec", { valueAsNumber: true })}
                min={1}
                className="w-20 font-mono text-xs"
              />
              <span className="text-xs text-muted-foreground">sec</span>
            </div>
          </FieldGroup>
        </FieldRow>
      </FormSection>

      {/* Submit row */}
      <div className="flex items-center justify-end gap-3 pt-2">
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() => router.push("/runs")}
        >
          Cancel
        </Button>
        <Button type="submit" disabled={submitting} className="gap-1.5">
          {submitting ? (
            <>
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
              Starting run…
            </>
          ) : (
            <>
              <Play className="h-3.5 w-3.5" />
              Start Run
            </>
          )}
        </Button>
      </div>
    </form>
  );
}
