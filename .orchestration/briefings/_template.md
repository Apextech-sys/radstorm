# Briefing template

Copy this file when creating a new task briefing. Use the filename pattern `<wave>-<slice>-<short-name>.md`.

---

# Briefing: <Wave>.<Slice> — <Short title>

## Mission

One paragraph: what you (the sub-agent) are building and why it matters to the overall system.

## Context — read these first

- `README.md`
- `.orchestration/STATE.md`
- `.orchestration/WAVES.md`
- `docs/ARCHITECTURE.md`
- `docs/CONVENTIONS.md`
- Any contracts in `.orchestration/contracts/` your work touches
- Any prior reports in `.orchestration/reports/` whose work you depend on

## Scope

**In scope:**
- Bullet list of what to build

**Out of scope:**
- Explicitly what NOT to do (so you don't accidentally take on adjacent work)

## Working directory

- **Worktree:** `<path>` (the orchestrator created this for you; CD here)
- **Files you own (create or modify):** list of paths
- **Files you read but do NOT modify:** list of paths and why

## Contracts you must conform to

- Reference the relevant `.orchestration/contracts/*.md` and key invariants

## Success criteria

- Specific, verifiable conditions (e.g., "tests pass", "binary builds", "endpoint returns expected JSON shape")
- Coverage targets if applicable

## Test requirements

- Unit tests required for each public function with non-trivial logic
- Integration tests required for any I/O or cross-package interaction
- Use the test framework conventions in `docs/CONVENTIONS.md`

## File-header requirement

Every code file you create or significantly modify MUST start with the header block defined in `docs/CONVENTIONS.md`. No exceptions.

## Reporting

When you finish:
1. Run all tests; ensure they pass
2. Write your implementation report at `.orchestration/reports/<wave>-<slice>-<short-name>.md` covering:
   - What you built (bullets)
   - Files created/modified
   - Test results (counts, coverage if measured)
   - Any deviations from this briefing and why
   - Any follow-ups required
3. Return a concise summary in your tool response — no need to repeat the report verbatim
