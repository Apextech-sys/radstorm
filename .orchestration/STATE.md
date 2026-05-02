# Orchestration State

**This file is the single source of truth for orchestration state. The orchestrator MUST update this file after every significant action. Anyone (including future-self after compaction) reading this file should know exactly where the build is and what to do next.**

---

## Current state

- **Wave:** 1 — Contracts & Foundations (in flight)
- **Phase:** All 6 slices dispatched in parallel via worktrees
- **Last updated:** 2026-05-02 01:10 GMT+2
- **Orchestrator session:** Started 2026-05-02 ~00:50 GMT+2 (autonomous overnight build)
- **Target deliverable by morning:** Working end-to-end system tested at small scale against Docker FreeRADIUS

### Wave 1 in flight

| Slice | Agent | Worktree | Branch | Status |
|---|---|---|---|---|
| 1A RADIUS protocol | backend-developer | C:/dev/radstorm-1a | wave-1/1a-radius | dispatched |
| 1B Config package | backend-developer | C:/dev/radstorm-1b | wave-1/1b-config | dispatched |
| 1C Events + Collector | backend-developer | C:/dev/radstorm-1c | wave-1/1c-events | dispatched |
| 1D Frontend scaffold | frontend-developer | C:/dev/radstorm-1d | wave-1/1d-frontend-scaffold | dispatched |
| 1E Docker FreeRADIUS rig | devops-engineer | C:/dev/radstorm-1e | wave-1/1e-docker-rig | dispatched |
| 1F API skeleton | backend-developer | C:/dev/radstorm-1f | wave-1/1f-api-skeleton | dispatched |

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
