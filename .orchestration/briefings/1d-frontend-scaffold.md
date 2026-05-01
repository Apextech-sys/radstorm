# Briefing 1D — Next.js frontend scaffold

## Mission

Scaffold a polished, opinionated Next.js + shadcn/ui frontend at `apps/web/`. Set up the design system, layout shell, navigation, theme, and TypeScript types mirroring the contracts. Build the visual foundation that the Wave 2/3 page work will plug into. **Visual quality matters — the frontend must look superb.**

## Context

1. `README.md`, `.orchestration/STATE.md`, `.orchestration/WAVES.md`
2. `docs/CONVENTIONS.md`
3. **`.orchestration/contracts/config-schema.md`** — produce TS types + zod schemas mirroring this
4. **`.orchestration/contracts/rest-api.md`** — produce typed API client stub
5. **`.orchestration/contracts/results-schema.md`** — produce TS types
6. `docs/ARCHITECTURE.md`

## Working directory

- **Worktree:** `C:\dev\radstorm` on `main` (no parallel writers to `apps/web/` in Wave 1)
- **Files you own:** everything under `apps/web/`

## Scope

**In scope:**
- `npx create-next-app@latest apps/web --ts --tailwind --eslint --app --src-dir --import-alias '@/*'`
- Use Next.js 16 App Router, React 19, Tailwind v4, TypeScript strict
- Install shadcn/ui: `npx shadcn@latest init` with the New York style and Neutral base color
- Install initial shadcn components: `button`, `card`, `input`, `label`, `select`, `textarea`, `tabs`, `dialog`, `sheet`, `table`, `badge`, `separator`, `skeleton`, `sonner` (toaster), `tooltip`, `progress`, `slider`, `checkbox`, `switch`, `form`, `chart`, `dropdown-menu`, `scroll-area`
- Design system:
  - Theme: dark-first with light alternative; tasteful (NOT default shadcn purple). Use a refined neutral palette with a single accent color — pick something distinctive (suggested: a muted electric green `oklch(0.78 0.18 145)` or deep azure `oklch(0.6 0.2 240)`) and apply consistently
  - Typography: Geist Sans for UI, Geist Mono for numerics (latencies, IDs, JSON)
  - Spacing: generous, not cramped
  - Charts: a custom chart palette that's distinguishable in both themes
- Layout shell:
  - Sidebar navigation (collapsible) with: Dashboard, New Run, Runs, Settings
  - Top bar with theme toggle and a small "API: connected/disconnected" indicator (poll `/api/v1/health` every 10s)
  - Page content area with consistent padding, max-width, breathing room
  - 404 + error boundary pages
- Pages (skeletons that load real layout but show "Coming in Wave 2" content):
  - `/` (Dashboard)
  - `/runs/new` (New Run)
  - `/runs` (Runs list)
  - `/runs/[id]` (Run detail)
  - `/settings`
- TypeScript types and zod schemas for: `Config`, `RunSummary`, `Run`, `EstablishmentCurve`, `LatencyHistogram` (from contracts)
- API client at `src/lib/api.ts`: typed fetch wrappers for every endpoint in `rest-api.md`
- Vitest + React Testing Library set up; one smoke test per page that asserts it renders without throwing
- A landing visual: the Dashboard's empty state should be visually compelling — not a default empty box. Use illustration, hero text, a sense of identity.
- README.md inside `apps/web/` with how to run dev/build/test

**Out of scope:**
- Actually fetching live data (Wave 3)
- Form logic for the New Run page (Wave 2 / 3)
- Charts (Wave 3)

## Visual quality bar

This is not a typical AI-generated frontend. Avoid:
- Generic "AI startup" aesthetics: floating gradients, hero with rotating text, glassmorphism overload
- Default shadcn purple
- Lorem ipsum-style placeholder text (use real branded strings: "radstorm", "Stress-test ISP RADIUS at scale", etc.)
- Default fonts and spacing

Aim for:
- Clean, dense-but-airy data-tool aesthetic (think Linear, Vercel, Modal, Plane)
- Strong typographic hierarchy
- Restrained use of color (mostly mono with a single accent)
- Numerics in monospace
- Real-looking empty states with personality

## Success criteria

- `cd apps/web && npm run build` succeeds
- `cd apps/web && npm test -- --run` passes
- `cd apps/web && npm run dev` starts and you can navigate all pages without errors
- `npm run lint` clean
- Theme toggle works
- All pages render with the layout shell
- TypeScript strict mode with zero errors
- Real visual polish on the dashboard empty state

## Test requirements

- Smoke test per page: `it("renders without crashing", () => { render(<Page />); })`
- Type check is part of acceptance: `tsc --noEmit` clean

## File-header requirement

Per `docs/CONVENTIONS.md` — every TS/TSX file you author or significantly modify gets the JSDoc header. shadcn-installed components are exempt (they're vendored from the registry).

## Reporting

Write `.orchestration/reports/1d-frontend-scaffold.md` per template. Include screenshots if you can capture them — at minimum, paste a tree of `apps/web/src/`.
