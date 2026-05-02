# Report: 1D — Next.js + shadcn/ui Frontend Scaffold

**Wave:** 1
**Slice:** 1D
**Branch:** wave-1/1d-frontend-scaffold
**Date:** 2026-05-02
**Status:** Complete

---

## Summary

Built the full Next.js 16 frontend scaffold for radstorm at `apps/web/`. All
acceptance criteria pass: `npm run build` succeeds, `npm test -- --run` passes
(18 tests across 5 files), `npm run lint` is clean, `npx tsc --noEmit` emits
zero errors.

---

## Acceptance criteria

| Criterion | Status |
|---|---|
| `npm run build` succeeds | PASS |
| `npm test -- --run` passes | PASS — 5 files, 18 tests |
| `npm run lint` clean | PASS |
| `npx tsc --noEmit` zero errors | PASS |
| Layout shell with sidebar + top bar | Done |
| API status indicator (polls /health every 10s) | Done |
| Theme toggle (dark/light) | Done |
| Pages render layout shell | Done |
| TypeScript types + zod schemas for Config | Done |
| TypeScript types + zod schemas for RunSummary | Done |
| Typed API client for all REST endpoints | Done |
| File-header JSDoc on every authored file | Done |
| shadcn components installed | Done (21 components) |

---

## Visual direction

**Accent:** Muted electric green — `oklch(0.78 0.18 145)`. Single accent
applied consistently on sidebar active states, API status dot, signal badge,
focus rings, and empty-state elements.

**Typography:** Geist Sans (UI) + Geist Mono (numerics, IDs, monospaced labels).
Feature settings `tnum` + `zero` enabled on monospace.

**Palette:** Near-black dark background `oklch(0.11)` vs near-white light
`oklch(0.99)`. Borders are very subtle (9% opacity in dark). Cards slightly
raised from background. No purple anywhere.

**Chart palette (both themes):** signal green / azure / amber / rose / teal —
5 perceptually distinct hues, all readable on dark and light.

**Dashboard empty state:** Branded hero card with a subtle dot-grid background
texture (CSS repeating-gradient, 3% opacity), pill identity badge, large
"No runs yet." headline, and a 4-column capability grid below a separator —
showing Scale / Protocols / Scenarios / Results specs.

**Anti-patterns avoided:**
- No floating gradients or glassmorphism
- No rotating hero text
- No default shadcn purple (primary is near-black/near-white)
- No lorem ipsum — all text is real radstorm copy

---

## Files authored (src/ tree)

```
src/
  app/
    globals.css                    # Custom design tokens, electric green accent
    layout.tsx                     # Root layout: Geist fonts, ThemeProvider, AppShell
    page.tsx                       # Dashboard with branded empty state
    not-found.tsx                  # 404 page
    error.tsx                      # Error boundary
    runs/page.tsx                  # Runs list skeleton
    runs/new/page.tsx              # New Run skeleton
    runs/[id]/page.tsx             # Run detail skeleton (Wave 3 placeholder)
    settings/page.tsx              # Settings skeleton
  components/
    layout/
      app-shell.tsx                # Full-page shell: sidebar + top bar + main
      page-container.tsx           # Consistent padding, PageHeader, WavePlaceholder
    shell/
      sidebar-nav.tsx              # Left nav, Zap logo, active-link signal dot
      top-bar.tsx                  # Top bar: breadcrumb area + API status + toggle
      api-status.tsx               # Polling health indicator (10s interval)
      theme-provider.tsx           # next-themes ThemeProvider wrapper
      theme-toggle.tsx             # Sun/Moon icon button
    ui/
      link-button.tsx              # Button-styled Link (no asChild needed in shadcn v4)
  lib/
    api.ts                         # Typed fetch wrappers for all 10 REST endpoints
    types/
      config.ts                    # Config zod schema + types (mirrors config-schema.md)
      results.ts                   # RunSummary zod schema + types (mirrors results-schema.md)
      api.ts                       # Run, RunProgress, SSE event types (mirrors rest-api.md)
  __tests__/pages/
    dashboard.test.tsx             # 5 smoke tests
    runs.test.tsx                  # 3 smoke tests
    new-run.test.tsx               # 3 smoke tests
    run-detail.test.tsx            # 4 smoke tests
    settings.test.tsx              # 3 smoke tests
  test-setup.ts                    # jest-dom matchers
vitest.config.ts                   # Vitest + jsdom + path alias
```

---

## Notable decisions

**shadcn version:** The project got shadcn 4.6 which uses `@base-ui/react`
instead of Radix UI. The `Button` component no longer supports `asChild`.
Created `src/components/ui/link-button.tsx` — a thin wrapper that applies
`buttonVariants` CVA classes to a `next/link` `Link`. All page nav buttons
use this instead.

**Zod v4 API changes:** Two adjustments required:
- `z.string().ip()` no longer exists → use `z.ipv4().or(z.ipv6())`
- `z.record(valueSchema)` now requires both key and value args → `z.record(z.string(), z.string())`

**ESLint rule `react-hooks/set-state-in-effect`:** The new Next.js ESLint
config flags calling setState synchronously inside a useEffect body. Fixed
`ApiStatus` by moving check() to `useCallback` and calling it on the interval
callback rather than in the effect body directly.

**Test approach:** Pages use Next.js metadata exports, server component patterns,
and Link. Tests mock `next/link`, `next/navigation`, and `next-themes` at the
module level. Run Detail page (`[id]/page.tsx`) is an async server component —
tests `await` the component function to get the JSX element before rendering.

---

## Handoff notes for Wave 2 (2E — frontend pages)

- All TypeScript types and zod schemas are in `src/lib/types/`
- The API client in `src/lib/api.ts` has stubs for every endpoint; Wire real
  data calls from page components using these functions
- `PageContainer`, `PageHeader`, and `WaveComingPlaceholder` in
  `src/components/layout/page-container.tsx` are ready to replace with real content
- The config form (New Run page) should use `configSchema` from
  `src/lib/types/config.ts` with react-hook-form + zod resolver
- shadcn components installed: button, card, input, label, select, textarea,
  tabs, dialog, sheet, table, badge, separator, skeleton, sonner, tooltip,
  progress, slider, checkbox, switch, form, chart, dropdown-menu, scroll-area
