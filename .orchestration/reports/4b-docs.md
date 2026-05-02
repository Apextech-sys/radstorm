# Wave 4B — Operator documentation

**Branch:** `wave-4/4b-docs`
**Worktree:** `C:\dev\radstorm-4b`
**Status:** Complete
**Date:** 2026-05-02

---

## Files created / modified

### Created

| File | Description |
|---|---|
| `docs/RUNBOOK.md` | Full operator runbook: prerequisites, build, first run, summary.json walkthrough, config section-by-section, source IP setup with capacity math, API+frontend navigation, production deployment, cancellation, output artifact reference |
| `docs/QUICKSTART.md` | 5-step numbered guide from clone to first successful 100-subscriber run; copy-pastable code blocks throughout; links to RUNBOOK and TROUBLESHOOTING |
| `docs/TROUBLESHOOTING.md` | 9 common operator issues with symptoms, causes, and fixes: credentials_file path resolution, RADIUS timeouts, Docker rig health, ID exhaustion, 409 conflicts, API disconnected, bad authenticator warnings, race detector CGO constraint, missing Parquet files, port conflicts |
| `docs/decisions/0001-go-monorepo-with-nextjs.md` | ADR for single-repo, single-go.mod, Next.js as sibling project |
| `docs/decisions/0002-hand-rolled-radius-protocol.md` | ADR for hand-rolled pkg/radius vs layeh.com/radius: Identifier ownership and deterministic test fixture rationale |
| `docs/decisions/0003-sqlite-via-modernc.md` | ADR for pure-Go SQLite (modernc.org/sqlite) vs mattn/go-sqlite3 (CGO): Windows dev host has no C compiler |
| `.orchestration/reports/4b-docs.md` | This file |

### Modified

| File | Changes |
|---|---|
| `README.md` | Full rewrite: two-sentence lead, CI badge, QUICKSTART link, ASCII architecture diagram, component map table, quick start commands, E2E suite instructions, repo layout tree, documentation index table, removed "under active development" status |

---

## Source material read

- `README.md` (original)
- `docs/ARCHITECTURE.md`, `docs/PROTOCOL.md`, `docs/CONFIG.md`, `docs/CONVENTIONS.md`
- `.orchestration/contracts/config-schema.md`, `event-schema.md`, `rest-api.md`, `results-schema.md`
- `.orchestration/STATE.md`, `.orchestration/WAVES.md`
- `.orchestration/reports/1a-radius-protocol.md`, `1b-config.md`, `2a-io-layer.md`, `2b-subscriber.md`, `2c-server-listener.md`, `2d-frontend-pages.md`, `3a-scenario-cli.md`, `3b-api-full.md`, `3d-e2e.md`
- `.orchestration/briefings/3d-e2e.md`
- `Makefile`, `test/e2e/README.md`, `go.mod`, `.github/workflows/ci.yml`

---

## Key facts captured from reports

- **E2E validated**: 100 subscribers, outcome=succeeded, duration=5668ms, p50≈10ms, p99≈35ms latency
- **Docker rig ports**: 11812 (auth), 11813 (acct), secret `testing123`
- **modernc.org/sqlite v1.50.0**: reason for choice documented in ADR 0003
- **Hand-rolled RADIUS**: reason for choice documented in ADR 0002
- **Source IP capacity math**: 256 IDs × tuples → ≥4000 tuples needed for 1M concurrent
- **Parquet sharding**: 24 shard files (one per CPU core) in the 100-sub smoke run

---

## Internal link verification

All internal document links verified against actual file paths in the worktree:

- `docs/RUNBOOK.md` links: `docs/CONFIG.md` ✓, `docs/TROUBLESHOOTING.md` ✓
- `docs/QUICKSTART.md` links: `docs/RUNBOOK.md` ✓, `docs/CONFIG.md` ✓, `docs/TROUBLESHOOTING.md` ✓
- `README.md` links: all docs/ targets ✓, `test/e2e/README.md` ✓, `docs/decisions/` dir ✓
- ADR cross-references: none (each is self-contained)

---

## Acceptance criteria

- [x] `docs/RUNBOOK.md` — created, covers all required sections
- [x] `docs/QUICKSTART.md` — created, 5 numbered steps, copy-pastable
- [x] `docs/TROUBLESHOOTING.md` — created, 9 operator issues covered
- [x] `docs/decisions/0001-go-monorepo-with-nextjs.md` — created, ADR format
- [x] `docs/decisions/0002-hand-rolled-radius-protocol.md` — created, ADR format
- [x] `docs/decisions/0003-sqlite-via-modernc.md` — created, ADR format
- [x] `README.md` — polished, no "under active development", confident tone, CI badge
- [x] All files have Purpose comment at top
- [x] Internal links verified
- [x] Actual E2E data used (100 subs, 5668ms, p50=10ms, p99=35ms)
