#!/usr/bin/env bash
# Verify every code file has the mandatory header documentation block.
#
# Purpose:
#   Scans all tracked Go, TypeScript/TSX, and shell files for the "Purpose:"
#   keyword that anchors the file-header convention defined in docs/CONVENTIONS.md.
#   Exits non-zero when any violations are found, making it safe for CI gating.
#
# Related: docs/CONVENTIONS.md, .github/workflows/ci.yml
# Briefing: orchestrator-managed
# Contract: CI gate for the file-header convention; exit 0 = clean, exit 1 = violations

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# ---------------------------------------------------------------------------
# Resolve file list — prefer git ls-files for accuracy; fall back to find so
# the script still works in environments where git is unavailable (e.g. a
# plain Docker build context without the .git directory).
# ---------------------------------------------------------------------------
if command -v git >/dev/null 2>&1 && git rev-parse --git-dir >/dev/null 2>&1; then
  mapfile -t candidates < <(git ls-files \
    '*.go' \
    'apps/web/src/**/*.ts' 'apps/web/src/**/*.tsx' \
    '*.sh' \
    2>/dev/null || true)
else
  # Fallback: find all relevant files under root
  mapfile -t candidates < <(find "$ROOT" \
    \( -name '*.go' -o -name '*.ts' -o -name '*.tsx' -o -name '*.sh' \) \
    -not -path '*/node_modules/*' \
    -not -path '*/.git/*' \
    -not -path '*/vendor/*' \
    2>/dev/null || true)
fi

# ---------------------------------------------------------------------------
# Exclusion patterns — files that are exempt from the header requirement.
# ---------------------------------------------------------------------------
# shellcheck disable=SC2016
EXCLUDE_PATTERN='(
  _templ\.go$
  |^apps/web/src/components/ui/
  |/node_modules/
  |/vendor/
  |_generated\.go$
  |\.pb\.go$
)'
# Remove whitespace we added for readability
EXCLUDE_PATTERN="$(echo "$EXCLUDE_PATTERN" | tr -d '[:space:]')"

fail=0
checked=0

for f in "${candidates[@]}"; do
  [ -f "$f" ] || continue

  # Apply exclusions
  if echo "$f" | grep -qE "$EXCLUDE_PATTERN"; then
    continue
  fi

  checked=$((checked + 1))

  # Look within the first 40 lines for "Purpose:" in any comment style:
  #   Go:         // Purpose:
  #   TS/TSX:      * Purpose:
  #   Shell/YAML:  # Purpose:
  #   Plain:       Purpose:
  if ! head -n 40 "$f" | grep -qE '^\s*(//|#|\*)?\s*Purpose:'; then
    echo "MISSING HEADER: $f"
    fail=$((fail + 1))
  fi
done

echo
echo "Header check: $checked files checked, $fail violation(s)."

if [ "$fail" -gt 0 ]; then
  echo "Run 'bash scripts/check-headers.sh' locally and add missing headers per docs/CONVENTIONS.md."
  exit 1
fi

exit 0
