#!/usr/bin/env bash
# merge-coverage.sh — merge Vitest and Playwright istanbul coverage into a
# single LCOV report at site/coverage/lcov.info
set -euo pipefail

SITE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
COVERAGE_DIR="$SITE_DIR/coverage"

# Merge e2e JSON coverage files into a single LCOV via nyc
if [ -d "$COVERAGE_DIR/e2e" ] && [ "$(ls -A "$COVERAGE_DIR/e2e" 2>/dev/null)" ]; then
  echo "Merging e2e coverage JSON files..."
  npx nyc report \
    --temp-dir "$COVERAGE_DIR/e2e" \
    --reporter lcov \
    --report-dir "$COVERAGE_DIR/e2e-lcov" \
    --exclude-after-remap false
fi

# Merge all LCOV files into one
LCOV_FILES=()
if [ -f "$COVERAGE_DIR/vitest/lcov.info" ]; then
  LCOV_FILES+=("$COVERAGE_DIR/vitest/lcov.info")
fi
if [ -f "$COVERAGE_DIR/e2e-lcov/lcov.info" ]; then
  LCOV_FILES+=("$COVERAGE_DIR/e2e-lcov/lcov.info")
fi

if [ ${#LCOV_FILES[@]} -eq 0 ]; then
  echo "ERROR: No coverage files found to merge."
  exit 1
fi

if [ ${#LCOV_FILES[@]} -eq 1 ]; then
  cp "${LCOV_FILES[0]}" "$COVERAGE_DIR/lcov.info"
else
  # Simple concatenation works for LCOV format (Codecov merges overlapping entries)
  cat "${LCOV_FILES[@]}" > "$COVERAGE_DIR/lcov.info"
fi

echo "Merged coverage written to $COVERAGE_DIR/lcov.info"

# Print summary
if command -v npx &>/dev/null; then
  echo ""
  echo "=== Coverage Summary ==="
  npx nyc report \
    --temp-dir "$COVERAGE_DIR/e2e" \
    --reporter text-summary \
    --exclude-after-remap false 2>/dev/null || true
fi
