# Orchestration State

**This file is the single source of truth for orchestration state. The orchestrator MUST update this file after every significant action. Anyone (including future-self after compaction) reading this file should know exactly where the build is and what to do next.**

---

## Current state

- **Wave:** COMPLETE
- **Status:** All 5 waves shipped. Full E2E suite passes (smoke-100, cli-flows, api-flow). System operational end-to-end on `origin/main`.
- **Last updated:** 2026-05-02 09:50 GMT+2
- **Orchestrator session:** Started 2026-05-02 ~00:50 GMT+2 — finished ~09:50 GMT+2 (~9 hours wall, ~18 sub-agents + 3 recovery)
- **Deliverable:** see `MORNING-REPORT.md` at repo root

### Wave 1 outcomes (all done)

| Slice | Result | Coverage | Notes |
|---|---|---|---|
| 1A RADIUS protocol | done | 85.7% | Hand-rolled, skipped layeh.com/radius for direct control over ID/Authenticator |
| 1B Config | done | 91.3% | Full TOML loader, validator, credentials CSV |
| 1C Events + Collector | done | 100% events / 86.6% collector | 1M events in <5s perf gate met |
| 1D Frontend scaffold | done | 18 tests | Restrained green accent, Linear/Vercel aesthetic |
| 1E Docker FreeRADIUS rig | done | smoke pass | 5000 seeded users; host ports 11812/11813 |
| 1F API skeleton | done | 82-95% | All endpoints mocked; OpenAPI spec complete |

Post-merge integration:
- API stub swapped for real pkg/config (5 files updated, stub removed)
- mockdata fixture paths corrected to match 1E names
- All Go tests pass; frontend builds + tests pass + tsc clean
- Push to origin/main: commit 1609abe

## Decisions locked in

| Decision | Value | Source |
|---|---|---|
| GitHub account | `Apextech-sys` (private repo) | User |
| Repo URL | https://github.com/Apextech-sys/reflex-radstorm | Created Wave 0 |
| Frontend stack | Next.js + shadcn/ui | User |
| Architecture | Frontend → REST → Go API server → Go CLI/library | User |
| Local E2E target | Docker FreeRADIUS, scale that fits a laptop (≤10k subs) | User |
| Frontend scope | Config form, trigger/cancel, live progress, results dashboard, charts | User |
| Visual quality | Must look superb (frontend) | User |
| Acceptance authority | Auto-advance through waves; deliver complete by morning | User |

## Active wave: Wave 0 — Bootstrap

Sequential setup, not parallelizable. Orchestrator does this directly (no sub-agents).

- [x] Verify GitHub auth → `Apextech-sys`
- [x] Create private repo `reflex-radstorm`
- [x] Initialize local git repo
- [x] Create directory tree
- [x] Write README, .gitignore
- [ ] Write STATE.md, WAVES.md, ARCHITECTURE.md, PROTOCOL.md, CONFIG.md, CONVENTIONS.md (this commit)
- [ ] Write Wave 1 briefing docs
- [ ] Initial commit + push
- [ ] Dispatch Wave 1

## Recovery protocol (read this if you are a new orchestrator session)

1. Read this STATE.md first to understand current wave and phase
2. Read `.orchestration/WAVES.md` for the full wave plan and what comes next
3. Read `.orchestration/agent-log/dispatch.jsonl` (tail) to see recent agent dispatches and outcomes
4. Check `.orchestration/reports/` for any completed agent reports
5. Run `git status` and `git log --oneline -20` to see what's on disk and committed
6. Run `git worktree list` to see active worktrees from in-flight agents
7. Update this file with your understanding of state, then continue

## How to dispatch a sub-agent (orchestrator playbook)

For each task:
1. Write a briefing doc at `.orchestration/briefings/<wave>-<slice>-<task>.md` using the template below
2. Decide: does this task need a worktree? (Yes if it touches code that another agent in the same wave also touches; otherwise it can run on `main` if path-isolated)
3. If worktree needed: `git worktree add ../radstorm-<wave>-<slice> -b wave-<wave>/<slice>`
4. Dispatch via the `Agent` tool with `subagent_type` matching the task (backend-developer, frontend-developer, qa-engineer, devops-engineer, code-architecture-specialist, etc.)
5. The agent's prompt MUST instruct them to: read briefing, read referenced docs, do the work, write tests, write/update the implementation report at `.orchestration/reports/<wave>-<slice>-<task>.md`, return a concise summary
6. Append a JSON line to `.orchestration/agent-log/dispatch.jsonl` (the orchestrator does this, not the agent)
7. When agent returns: validate output, check tests pass, merge worktree if applicable, update this STATE.md

## Briefing template

See [`.orchestration/briefings/_template.md`](briefings/_template.md).

## Failure recovery

If an agent fails, stalls, or returns broken output:
1. Do NOT delete its worktree
2. Read the agent's partial work (worktree contents, partial report)
3. Write a new briefing at `.orchestration/briefings/<wave>-<slice>-<task>-recovery.md` that includes:
   - What the previous agent did
   - What's broken or missing
   - Specific instruction for the recovery agent
4. Dispatch a fresh agent to the same worktree
5. If the worktree is unsalvageable, archive it (rename to `*-failed`) and create a clean one

## Key contracts (DO NOT change without coordinating across slices)

These contracts are referenced by multiple sub-agents and must be agreed upon before parallel work begins. They live at:

- [`.orchestration/contracts/event-schema.md`](contracts/event-schema.md) — event log schema (subscriber/io/server emit, collector consumes)
- [`.orchestration/contracts/config-schema.md`](contracts/config-schema.md) — TOML config + JSON-schema-equivalent (CLI consumes, frontend produces)
- [`.orchestration/contracts/rest-api.md`](contracts/rest-api.md) — REST API between frontend and Go API server
- [`.orchestration/contracts/results-schema.md`](contracts/results-schema.md) — `summary.json` and Parquet schemas (CLI emits, frontend reads)

These are written in Wave 1 BEFORE the slice work that consumes them. Wave 1 has a contract-freeze gate: once contracts are committed and other slices have started, contracts can only be amended via an explicit change request that re-coordinates downstream consumers.
