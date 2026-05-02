# Report: 2D — Frontend pages (config form, runs list, run detail, results)

**Wave:** 2
**Slice:** 2D
**Branch:** wave-2/2d-frontend-pages
**Date:** 2026-05-02
**Status:** Complete

---

## Summary

Built the full frontend page content for radstorm at `apps/web/`. All acceptance
criteria pass: `npm run build` succeeds, `npm test -- --run` passes (44 tests
across 7 files), `npm run lint` clean (0 errors, 1 expected warning), and
`npx tsc --noEmit` emits zero errors.

---

## Acceptance criteria

| Criterion | Status |
|---|---|
| `npm run build` succeeds | PASS |
| `npm test -- --run` passes | PASS — 7 files, 44 tests |
| `npm run lint` clean (0 errors) | PASS — 1 warning (react-hook-form incompatible lib) |
| `npx tsc --noEmit` zero errors | PASS |
| `/runs/new` form with all TOML schema sections | Done |
| Subscriber count slider with presets | Done |
| Template picker (loads GET /scenarios/templates) | Done |
| `/runs` sortable table with status badges + relative timestamps | Done |
| Empty state with CTA | Done |
| `/runs/[id]` live SSE progress section | Done |
| Live establishment curve chart | Done |
| Results dashboard (donut, curve, histogram, thresholds) | Done |
| Cancel button | Done |
| Log panel (collapsible) | Done |
| Config viewer tab | Done |
| Artifacts download list | Done |
| JSDoc headers on every authored file | Done |

---

## New files authored

```
src/
  lib/
    format.ts                          # Number/time/duration formatting utilities
    hooks/
      use-run-events.ts               # SSE subscription hook with reconnect
  components/
    runs/
      status-badge.tsx                 # RunStatus badge with animated dot for "running"
      form-section.tsx                 # FormSection, FieldRow, FieldGroup primitives
      scenario-form.tsx               # Full config form (react-hook-form + zod)
      runs-table.tsx                  # Sortable runs list with polling + empty state
      live-progress.tsx               # Live SSE progress bar + stat cards + chart
      results-dashboard.tsx           # Full results: donut, curve, histogram, CoA, artifacts
  app/
    runs/
      page.tsx                        # Replaced placeholder with RunsTable
      new/
        page.tsx                      # Replaced placeholder with ScenarioForm
      [id]/
        page.tsx                      # Replaced placeholder, calls RunDetailClient
        run-detail-client.tsx         # Client component: SSE + progress → results switch
  __tests__/
    components/
      status-badge.test.tsx           # 5 StatusBadge unit tests
    lib/
      format.test.ts                  # 14 format utility unit tests
```

---

## Pages visual design

### /runs/new
- Two-column form with breathable section cards (FormSection component)
- Section headers: Target, CoA Listener, Subscribers, Source, NAS, Retransmit, Scenario, Output
- Subscriber count: prominent slider (1–1M) + editable number input + 5 quick-preset buttons (100 / 1K / 10K / 100K / 1M) styled in signal green when active
- Scenario type: 4-card radio group showing type name + one-line description, selected card highlighted in signal-green tint
- Conditional sub-forms appear/disappear for each scenario type
- Template picker loads API templates as pill buttons above the form
- Inline zod error messages below each field
- Submit button in page header row + footer — keeps action always visible

### /runs
- Full-width table with sortable columns: Name, Status, Created, Duration, Established, p99 Latency
- Status badges: color-coded, animated pulse dot for "running"
- Relative timestamps in cell, absolute datetime on `title` hover
- Loading skeleton (4 rows of shimmer) before first API response
- Empty state: centered icon + "No runs yet" + "New Run" CTA button
- 5-second polling while page is open

### /runs/[id] — active run
- Header: run name, status badge, created/started/finished timestamps (relative + hover absolute), Cancel button
- Tab bar: Progress | Configuration
- Live progress section: animated progress bar + 4 large-numeral stat cards (Established / In flight / Failed / Retransmits) + live ComposedChart (area for in-flight, line for established)
- Collapsible log panel showing log events from SSE stream
- Config tab: syntax-highlighted JSON

### /runs/[id] — completed run
- Pass/fail banner with icon + percentage established
- 4 summary stat cards: Established, p99 latency, Duration, Retransmits
- Threshold table with per-row pass/fail badges
- Subscriber outcome donut chart (5 segments: established / auth-failed / acct-failed / terminated / in-flight)
- Establishment curve chart (area: in-flight, line: established)
- Latency histogram bar chart with p50/p95/p99/p99.9 markers above
- CoA + Disconnect cards
- Server health card
- Artifacts download list

---

## Notable decisions

**react-hook-form + Zod:** The form uses `z.input<typeof configSchema>` as the
form type rather than `z.infer` (the output type). This handles Zod's `.default()`
values which make fields optional in the input type. The form calls
`configSchema.parse(data)` before submitting to ensure all defaults are applied.

**SSE hook design:** `useRunEvents` uses the "callback ref" pattern — all
callbacks are stored in a `useRef` and synced via a layout effect, so the
`EventSource` listeners always call the latest callback without needing to
re-open the connection when parent re-renders.

**Recharts formatter types:** Recharts 3.x makes the `formatter` prop value
type `ValueType | undefined`. All formatters use `typeof v === "number" ?
fmtInt(v) : String(v)` to satisfy the type.

**setState-in-effect lint rule:** The project's ESLint config flags calling
setState as the first statement in a useEffect. Worked around using `setTimeout`
with delay 0 to schedule the initial data fetch — matching the pattern already
used in ApiStatus (`setInterval` before the immediate `check()` call).

**Lint warning:** One non-blocking warning from `react-hooks/incompatible-library`
for react-hook-form's `watch()` function — this is expected behavior documented
by the React Compiler team and does not affect functionality.

---

## Handoff notes for Wave 3 / 3B (API full impl)

- The SSE hook (`useRunEvents`) is wired and will connect to the real stream once
  3B implements `GET /api/v1/runs/{id}/events` with live events
- The `getEstablishmentCurve` and `getLatencyHistogram` endpoints are called
  from ResultsDashboard — they need real data from 3B/3C
- `createRun` posts the full Config JSON; 3B must accept and persist it
- All component boundaries are clean client/server — server components wrap
  client components, no mixing
