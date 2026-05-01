# Project conventions

## File-header documentation (MANDATORY)

Every code file in this repo (Go, TypeScript, TSX, shell, SQL, Dockerfile, YAML config) MUST begin with a header comment block. The block exists so a future contributor reading the file in isolation has full context.

### Go

```go
// Package <name> — <one-line purpose>.
//
// Purpose:
//   <2-3 lines on what this file does>
//
// Related files:
//   - pkg/foo/bar.go (consumer of this type)
//   - .orchestration/contracts/event-schema.md (defines the event shape this file produces)
//
// Briefing: .orchestration/briefings/<wave>-<slice>-<task>.md
//
// Contract: <if this file owns a public contract — describe it; else "internal">
package mypackage
```

### TypeScript / TSX

```ts
/**
 * <one-line purpose>
 *
 * Purpose:
 *   <2-3 lines>
 *
 * Related files:
 *   - apps/web/lib/api.ts (calls this hook)
 *   - .orchestration/contracts/rest-api.md
 *
 * Briefing: .orchestration/briefings/<wave>-<slice>-<task>.md
 *
 * Contract: <public contract owned, or "internal">
 */
```

### Shell / Dockerfile / YAML

```bash
# <one-line purpose>
#
# Purpose: <2-3 lines>
# Related: <files>
# Briefing: .orchestration/briefings/<...>
# Contract: <...>
```

A linter script in `scripts/check-headers.sh` (added in Wave 1) verifies presence in CI.

## Code style

### Go
- `gofmt` + `goimports`
- `golangci-lint` with `errcheck`, `staticcheck`, `gosimple`, `govet`, `ineffassign` enabled
- Errors wrapped with `fmt.Errorf("doing X: %w", err)`; never use `panic` outside `init()` or genuine programmer errors
- Test files end in `_test.go`, use `testing` + `github.com/stretchr/testify/assert` and `require`
- Use `slog` (stdlib) for logging
- Use `context.Context` everywhere I/O happens

### TypeScript
- Strict mode on
- Prettier formatting
- ESLint with the Next.js + TypeScript recommended rules
- All API responses validated with `zod` schemas mirroring the OpenAPI spec
- React: function components only, hooks for state, no class components
- shadcn/ui components consumed via the CLI-installed copies in `apps/web/components/ui/`

## Testing

### Go
- Unit tests live next to the code in `*_test.go`
- Integration tests live in `*_integration_test.go` with build tag `//go:build integration`
- E2E tests live in `test/e2e/` and use a real Dockerized FreeRADIUS

### Frontend
- Component tests with Vitest + React Testing Library
- Live in `__tests__/` next to the component or under `apps/web/__tests__/`

## Commit hygiene

- Conventional commits (`feat:`, `fix:`, `chore:`, `docs:`, `test:`, `refactor:`)
- Each PR maps to one wave-slice-task
- Sub-agents commit on their worktree branch; the orchestrator merges to `main`

## Documentation discipline

- Architecture changes → ADR in `docs/decisions/`
- API changes → update `.orchestration/contracts/rest-api.md` AND the OpenAPI spec AND the frontend zod schemas
- Config schema changes → update `.orchestration/contracts/config-schema.md` AND the Go struct AND the TypeScript types AND CONFIG.md
- Whenever you touch a contract, you touch every file referenced from that contract — no exceptions
