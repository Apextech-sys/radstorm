/**
 * Tailwind class-name merge utility.
 *
 * Purpose:
 *   Re-exports a combined clsx + tailwind-merge helper used throughout the
 *   component tree to conditionally apply and deduplicate Tailwind classes.
 *
 * Related files:
 *   - apps/web/src/components/ui/* (primary consumers)
 *
 * Briefing: .orchestration/briefings/1d-frontend-scaffold.md
 *
 * Contract: internal — exports `cn()` helper only.
 */
import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
