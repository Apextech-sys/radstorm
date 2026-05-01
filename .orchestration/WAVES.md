# Wave Plan

This file lists every wave, every slice within each wave, and every task within each slice, with status and acceptance gate for each.

Status legend: `pending` | `in-progress` | `blocked` | `done` | `failed`

---

## Wave 0 — Bootstrap (orchestrator-only, no sub-agents)

**Goal:** Repo, orchestration tooling, contracts skeleton, dev rig committed.

**Acceptance gate:** Repo pushed to GitHub. Initial commit on `main`. Wave 1 briefings written.

| Task | Status | Notes |
|---|---|---|
| 0.1 Verify gh auth | done | `Apextech-sys` |
| 0.2 Create private repo | done | https://github.com/Apextech-sys/reflex-radstorm |
| 0.3 Init local git + structure | done | |
| 0.4 Write orchestration files | in-progress | STATE.md + WAVES.md + contracts skeleton |
| 0.5 Write canonical docs | pending | ARCHITECTURE, PROTOCOL, CONFIG, CONVENTIONS |
| 0.6 Write Wave 1 briefings | pending | One per slice |
| 0.7 Initial commit + push | pending | |

---

## Wave 1 — Contracts & Foundations

**Goal:** All cross-slice contracts written and committed. Project skeleton (Go module, Next.js scaffold, Docker rig) buildable.

**Parallel:** All slices can run in parallel because they consume only the contracts written in 0.4 / 0.5.

**Acceptance gate:** `make build` succeeds (compiles empty stubs). `docker compose up` starts FreeRADIUS. `npm run build` succeeds in `apps/web/`. Contracts committed and not changed.

| Slice | Task | Subagent | Worktree | Status |
|---|---|---|---|---|
| 1A | Go module + RADIUS protocol layer (dictionaries, packet encode/decode for Access-Request PAP+CHAP, Accounting-Request, CoA-ACK, Disconnect-ACK), unit tests with vector fixtures | backend-developer | wave-1/1a-radius | pending |
| 1B | Config package (TOML loader + validation + Go struct definitions matching `.orchestration/contracts/config-schema.md`), credentials file loader | backend-developer | wave-1/1b-config | pending |
| 1C | Events package (Go struct + Parquet schema for events, summary.json schema), collector skeleton | backend-developer | wave-1/1c-events | pending |
| 1D | Next.js scaffold with shadcn/ui, Tailwind, TypeScript types mirroring config + events contracts, design system (theme, tokens, layout shell) | frontend-developer | wave-1/1d-frontend-scaffold | pending |
| 1E | Docker FreeRADIUS test rig (docker-compose.yml, FreeRADIUS config, seeded test users via SQL or users file, smoke test script that auths one user via radclient) | devops-engineer | wave-1/1e-docker-rig | pending |
| 1F | Go HTTP API server skeleton conforming to `.orchestration/contracts/rest-api.md`, OpenAPI spec, stub handlers | backend-developer | wave-1/1f-api-skeleton | pending |

---

## Wave 2 — Core Components

**Goal:** Each subsystem fully built and unit-tested in isolation.

**Parallel:** Yes, all slices.

**Acceptance gate:** Each slice's package has ≥80% line coverage from unit tests; all tests pass; documentation header on every file.

| Slice | Task | Subagent | Worktree | Status |
|---|---|---|---|---|
| 2A | I/O layer: socket pool, ID allocator (bitmap per src tuple), reply matcher (correlation table with duplicate detection), sender (with retransmit), receiver | backend-developer | wave-2/2a-io | pending |
| 2B | Subscriber state machine: full FSM per spec §2.3, retransmit policy, event emission, CoA/Disconnect inbound handlers, mocked I/O for unit tests | backend-developer | wave-2/2b-subscriber | pending |
| 2C | Collector: sharded channels, in-memory ring buffer, periodic Parquet flush, summary aggregation | backend-developer | wave-2/2c-collector | pending |
| 2D | Server listener: UDP listen for CoA + Disconnect, Message-Authenticator validation, attribute decode (incl. Huawei VSA), subscriber lookup, ACK/NAK responses, latency recording | backend-developer | wave-2/2d-server-listener | pending |
| 2E | Frontend: scenario config form (form.tsx), run trigger UI, basic shell pages (Dashboard, New Run, Run Detail, Results) wired to mock API | frontend-developer | wave-2/2e-frontend-pages | pending |

---

## Wave 3 — Integration

**Goal:** All components wired into a working CLI binary + API server + frontend that can run a real scenario end-to-end.

**Parallel where possible.**

**Acceptance gate:** `radstorm run-scenario` against Docker FreeRADIUS at 1k subscribers completes successfully and produces summary.json + Parquet outputs. API server can trigger and stream a run. Frontend can submit + monitor a run.

| Slice | Task | Subagent | Worktree | Status |
|---|---|---|---|---|
| 3A | Scenario driver: load config, build activation schedule (Gaussian/uniform/pessimal), warmup→ramp→drain→finalize lifecycle, wires subscriber pool + I/O + collector + server listener | backend-developer | wave-3/3a-scenario | pending |
| 3B | Go HTTP API server full impl: `POST /runs`, `GET /runs`, `GET /runs/{id}`, `GET /runs/{id}/events` (SSE stream), `GET /runs/{id}/results`, `POST /runs/{id}/cancel`, run state persistence | backend-developer | wave-3/3b-api | pending |
| 3C | Frontend ↔ API integration: real fetch hooks, SSE consumption, run progress UI, results dashboard with charts (establishment curve, latency histogram, retransmit distribution), polished visual design | frontend-developer | wave-3/3c-frontend-integration | pending |
| 3D | E2E test harness: scripted scenario runs against Docker FreeRADIUS, assertions on summary.json metrics, CI workflow | qa-engineer | wave-3/3d-e2e | pending |

---

## Wave 4 — Validation & Polish

**Goal:** End-to-end run at small scale (1k–5k) against Docker FreeRADIUS passes acceptance criteria. Operator docs complete. Repo is presentable.

**Mostly sequential (depends on Wave 3 outputs working together).**

**Acceptance gate:** Full E2E run succeeds. README accurate. RUNBOOK complete. All file headers present. CI green.

| Slice | Task | Subagent | Worktree | Status |
|---|---|---|---|---|
| 4A | Run E2E suite, identify defects, dispatch fixes | orchestrator | main | pending |
| 4B | RUNBOOK + operator docs + screenshots of frontend | documentation-specialist | main | pending |
| 4C | Final visual polish on frontend (Wave 3 + design pass) | frontend-developer | main | pending |
| 4D | Code-architecture review pass: check file headers exist everywhere, lint, fix conventions violations | code-architecture-specialist | main | pending |

---

## Wave 5 — Final commit & morning report

**Goal:** Everything pushed to `main`. A summary report of what was built, what works, what was deferred.

| Task | Status |
|---|---|
| 5.1 Final push to main | pending |
| 5.2 Write `MORNING-REPORT.md` summarizing build outcomes, test results, screenshots, and how to use the system | pending |
| 5.3 Update STATE.md to "complete" | pending |
