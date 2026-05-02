# Briefing 2D — Frontend pages (config form, runs list, run detail, results)

## Mission

Build out the actual page content in the Next.js app at `apps/web/`. The scaffold from 1D has the shell, design system, and routing in place. You're filling in the pages with real, polished UI: scenario configuration form, runs list, run detail with live progress (SSE), results dashboard with charts. Wire to the API stub from 1F (which is real and runs). **Visual quality remains the bar — polish matters.**

## Context

1. `README.md`, `.orchestration/STATE.md`, `.orchestration/WAVES.md`
2. `docs/ARCHITECTURE.md`
3. `docs/CONVENTIONS.md` — JSDoc file headers on every TS/TSX
4. **`.orchestration/contracts/config-schema.md`** — for the config form fields
5. **`.orchestration/contracts/rest-api.md`** — for the API client + SSE
6. **`.orchestration/contracts/results-schema.md`** — for the results dashboard
7. `apps/web/` — read what 1D scaffolded, especially:
   - `apps/web/src/lib/types/*` — TypeScript types + zod schemas
   - `apps/web/src/lib/api.ts` — typed API client
   - `apps/web/src/components/layout/app-shell.tsx` — layout shell
   - `apps/web/src/app/globals.css` — design tokens
8. `apps/api/openapi.yaml` — OpenAPI spec from 1F (matches rest-api.md)
9. `.orchestration/reports/1d-frontend-scaffold.md` — design rationale and tokens chosen

## Working directory

- **Worktree:** `C:\dev\radstorm-2d` on branch `wave-2/2d-frontend-pages`
- **Files you own:** everything new under `apps/web/src/app/` and `apps/web/src/components/` for these features. May modify the layout shell if needed (justify in report).

## Scope — pages to build

### `/runs/new` — Scenario configuration form
- Two-column layout: left = template picker (calls `GET /api/v1/scenarios/templates`), right = the editable config form
- Form sections matching the TOML config schema groups: Target, CoA listener, Subscribers, Source, NAS, Retransmit, Scenario, Output
- Use `react-hook-form` + the existing zod schema for validation
- Live-validate fields and show errors inline
- Scenario type = radio group (cold_start | uniform | pessimal | coa_storm) with conditional sub-form for the chosen type's parameters
- Subscriber count: prominent slider (with input box) — 100, 1k, 10k, 100k, 1M presets as quick buttons
- "Validate" button that calls a (future) endpoint or just runs zod validation locally
- "Start run" button submits POST `/api/v1/runs` and routes to `/runs/{id}` on success
- Polished, breathable layout. Section headers, helper text under tricky fields, smart defaults pre-filled

### `/runs` — Runs list
- Table of recent runs with columns: name, status (badge), created at (relative time), duration, subscribers established / total, p99 latency
- Status badges: queued (neutral), running (accent + animated dot), succeeded (green), failed (red), cancelling (amber), cancelled (muted)
- Click row → navigate to detail
- Empty state: subtle, "No runs yet" with a CTA button to `/runs/new`

### `/runs/[id]` — Run detail
- Header: name, status badge, created/started/finished timestamps, "Cancel" button if cancellable
- Live progress section (only while running):
  - Progress bar (subscribers activated / total)
  - Stat cards: established, in flight, failed, retransmits — large monospace numerals
  - Live chart: establishment curve being drawn in real time as SSE events arrive
- "Configuration" tab/section showing the frozen config (read-only, syntax-highlighted JSON)
- Wire to SSE: open EventSource against `GET /api/v1/runs/{id}/events`. Update local state from each `progress` event. On `complete` event, fetch full results and switch view to results dashboard.
- Logs panel (collapsible) showing the `log` events as they arrive

### Results dashboard (rendered when run finished — could be at `/runs/[id]` with a "results" tab)
- Top: pass/fail summary (overall threshold rollup)
- Threshold table — each threshold from `summary.thresholds.evaluated` as a row with pass/fail badge
- Subscriber outcome donut chart: established / auth-failed / acct-failed / terminated
- Establishment curve chart (line: established over time; line: activated over time; area: in-flight)
- Latency distribution chart: histogram of buckets from `/results/latency-histogram`, plus markers for p50/p95/p99/p99.9
- Retransmit distribution
- CoA + Disconnect summary cards (if applicable)
- Server health section (unresponsive periods, error responses)
- Artifacts download list

## Implementation notes

- **Charts:** use Recharts (already a shadcn chart dependency) or shadcn's Chart component. Keep the palette to the 5 distinct hues from the scaffold.
- **SSE:** native `EventSource`, with reconnect-on-error logic. Wrap in a custom hook `useRunEvents(runId)`.
- **Polling fallback:** for `/runs` list, poll every 5 seconds while the page is open OR connect to SSE.
- **Toast notifications:** use sonner for action confirmations and errors.
- **Loading states:** use Skeleton components for everything async.
- **Empty states:** thoughtful, not boilerplate.
- **Numerics:** Geist Mono with tabular numerals.
- **Time formatting:** use `date-fns` or native Intl. Show absolute on hover, relative in the cell.
- **Accessibility:** keyboard navigation works, focus indicators visible, ARIA labels for charts.

## Visual quality bar

Same as 1D: Linear/Vercel/Modal-grade polish. No purple. Restrained accent. Real product personality.

## Success criteria

- `cd apps/web && npm run build` succeeds
- `npm test -- --run` passes (add tests for non-trivial logic; smoke tests per page)
- `npm run lint` clean
- `npx tsc --noEmit` clean
- Manual flow works: navigate `/runs/new`, fill form, submit, watch live progress on `/runs/{id}`, see results render
- Visual review — the pages should look polished and intentional

## How to test against the API

API server runs on `:8080`. Set `NEXT_PUBLIC_API_BASE_URL=http://localhost:8080` if not already in `.env.local`. The 1F skeleton produces fake SSE events and a hardcoded summary, so the full flow demos end-to-end.

You can start it from the worktree:
```bash
cd /c/dev/radstorm-2d
export PATH="$PATH:/c/Program Files/Go/bin"
go run ./apps/api/cmd/radstorm-api &
cd apps/web && npm run dev
```

## File-header requirement

JSDoc on every TS/TSX file you author or significantly modify. shadcn-installed components exempt.

## When you finish

1. `cd apps/web && npm run build`
2. `npm test -- --run`
3. `npm run lint`
4. `npx tsc --noEmit`
5. Optionally take screenshots and embed in the report
6. Write `.orchestration/reports/2d-frontend-pages.md`
7. Commit on `wave-2/2d-frontend-pages`. Do NOT push.
8. <200-word summary.
