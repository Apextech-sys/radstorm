# radstorm — web frontend

Next.js 16 + shadcn/ui frontend for the radstorm RADIUS stress tester.

## Stack

- **Next.js 16** App Router, React 19, TypeScript strict
- **shadcn/ui** (base-nova style, Neutral base) — component library
- **Tailwind CSS v4** — utility-first styling
- **Geist Sans** for UI text, **Geist Mono** for numerics and IDs
- **Vitest** + **React Testing Library** — component tests
- **zod** — runtime API response validation
- **next-themes** — dark/light mode switching

## Design system

Single accent color: **muted electric green** `oklch(0.78 0.18 145)`.

- Dark-first theme with a clean light alternative
- Strong typographic hierarchy, generous whitespace
- Monospace numerics for latencies, IDs, and counts
- Chart palette: green / azure / amber / rose / teal (readable in both themes)
- Sidebar: narrow, signal-green accent on active items

## Project structure

```
src/
  app/                     # Next.js App Router pages
    layout.tsx             # Root layout (fonts, theme, shell)
    page.tsx               # Dashboard (empty state hero)
    not-found.tsx          # 404 page
    error.tsx              # Error boundary
    runs/
      page.tsx             # Runs list (Wave 2)
      new/page.tsx         # New Run form (Wave 2)
      [id]/page.tsx        # Run detail + live progress (Wave 3)
    settings/page.tsx      # Settings (Wave 2)
  components/
    layout/
      app-shell.tsx        # Sidebar + top bar frame
      page-container.tsx   # Page padding, header, Wave placeholder
    shell/
      sidebar-nav.tsx      # Left nav with active link state
      top-bar.tsx          # Top bar with API status + theme toggle
      api-status.tsx       # Health poll indicator
      theme-provider.tsx   # next-themes wrapper
      theme-toggle.tsx     # Sun/Moon toggle button
    ui/
      link-button.tsx      # Button-styled Next.js Link
      [shadcn components]  # Installed via shadcn CLI (do not edit)
  lib/
    api.ts                 # Typed fetch wrappers for all REST endpoints
    types/
      config.ts            # Config zod schema + TypeScript types
      results.ts           # RunSummary zod schema + types
      api.ts               # Run, RunProgress, SSE event types
    utils.ts               # cn() helper (shadcn)
  __tests__/pages/         # Smoke tests (one per page)
  test-setup.ts            # @testing-library/jest-dom import
```

## Commands

```bash
npm install          # Install dependencies
npm run dev          # Development server (http://localhost:3000)
npm run build        # Production build
npm test -- --run    # Run tests once
npm run lint         # ESLint
npm run typecheck    # tsc --noEmit
```

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `NEXT_PUBLIC_API_BASE_URL` | `http://localhost:8080` | radstorm API server base URL |

## Wave status

| Feature | Wave |
|---|---|
| Layout shell, design system, types, API client | 1 — done |
| Scenario config form, runs list, settings | 2 |
| Live progress SSE, results dashboard, charts | 3 |
| Final visual polish | 4 |
