/**
 * FormSection — labeled card section for the scenario config form.
 *
 * Purpose:
 *   Groups related form fields into a visually distinct card with a section
 *   heading and optional helper text. Used across the New Run config form
 *   to separate Target, Subscribers, Scenario, etc.
 *
 * Related files:
 *   - src/app/runs/new/scenario-form.tsx (primary consumer)
 *   - src/components/ui/card.tsx (underlying card primitives)
 *
 * Briefing: .orchestration/briefings/2d-frontend-pages.md
 *
 * Contract: Exports FormSection component.
 */

import React from "react";
import { cn } from "@/lib/utils";

interface FormSectionProps {
  title: string;
  description?: string;
  children: React.ReactNode;
  className?: string;
}

export function FormSection({
  title,
  description,
  children,
  className,
}: FormSectionProps) {
  return (
    <div className={cn("rounded-xl border border-border bg-card", className)}>
      <div className="border-b border-border px-5 py-4">
        <h3 className="text-sm font-semibold text-foreground">{title}</h3>
        {description && (
          <p className="mt-0.5 text-xs text-muted-foreground">{description}</p>
        )}
      </div>
      <div className="space-y-4 p-5">{children}</div>
    </div>
  );
}

interface FieldRowProps {
  children: React.ReactNode;
  columns?: 1 | 2 | 3;
  className?: string;
}

export function FieldRow({
  children,
  columns = 2,
  className,
}: FieldRowProps) {
  return (
    <div
      className={cn(
        "grid gap-4",
        columns === 1 && "grid-cols-1",
        columns === 2 && "grid-cols-1 sm:grid-cols-2",
        columns === 3 && "grid-cols-1 sm:grid-cols-3",
        className
      )}
    >
      {children}
    </div>
  );
}

interface FieldGroupProps {
  label: string;
  htmlFor?: string;
  hint?: string;
  error?: string;
  required?: boolean;
  children: React.ReactNode;
}

export function FieldGroup({
  label,
  htmlFor,
  hint,
  error,
  required,
  children,
}: FieldGroupProps) {
  return (
    <div className="flex flex-col gap-1.5">
      <label
        htmlFor={htmlFor}
        className="text-xs font-medium text-foreground"
      >
        {label}
        {required && (
          <span className="ml-1 text-destructive" aria-hidden="true">
            *
          </span>
        )}
      </label>
      {children}
      {hint && !error && (
        <p className="text-[11px] text-muted-foreground">{hint}</p>
      )}
      {error && (
        <p className="text-[11px] font-medium text-destructive" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}
