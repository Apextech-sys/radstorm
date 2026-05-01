#!/usr/bin/env bash
# Verify every code file has the mandatory header documentation block.
# See docs/CONVENTIONS.md.
#
# Briefing: orchestrator-managed
# Contract: this script is the CI gate for the file-header convention

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

fail=0
checked=0

# Patterns of files to check
mapfile -t files < <(git ls-files \
  '*.go' \
  'apps/web/src/**/*.ts' 'apps/web/src/**/*.tsx' \
  '*.sh' \
  | grep -vE '(_templ\.go$|^apps/web/src/components/ui/)' || true)

for f in "${files[@]}"; do
  [ -f "$f" ] || continue
  checked=$((checked + 1))
  # Look at the first 30 lines for the header keywords
  head=$(head -n 30 "$f")
  if ! echo "$head" | grep -qE '(Purpose:|@purpose|# Purpose)'; then
    echo "MISSING HEADER: $f"
    fail=$((fail + 1))
  fi
done

echo
echo "Checked $checked files. Missing headers: $fail."
[ "$fail" -eq 0 ]
