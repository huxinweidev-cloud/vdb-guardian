#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

pass_fixture="$(mktemp)"
fail_fixture="$(mktemp)"
trap 'rm -f "${pass_fixture}" "${fail_fixture}"' EXIT

cat >"${pass_fixture}" <<'FIXTURE'
github.com/h3xwave/vdb-guardian/cmd/vdbg 66.0
github.com/h3xwave/vdb-guardian/internal/migration 71.0
github.com/h3xwave/vdb-guardian/internal/reporting 89.0
total 70.5
FIXTURE

cat >"${fail_fixture}" <<'FIXTURE'
github.com/h3xwave/vdb-guardian/cmd/vdbg 66.0
github.com/h3xwave/vdb-guardian/internal/migration 69.9
github.com/h3xwave/vdb-guardian/internal/reporting 89.0
total 70.5
FIXTURE

"${REPO_ROOT}/scripts/check-go-coverage.sh" "${pass_fixture}" >/tmp/vdb-guardian-coverage-pass.out

if "${REPO_ROOT}/scripts/check-go-coverage.sh" "${fail_fixture}" >/tmp/vdb-guardian-coverage-fail.out 2>&1; then
  echo "expected migration coverage fixture to fail" >&2
  exit 1
fi
if ! grep -q 'internal/migration' /tmp/vdb-guardian-coverage-fail.out; then
  echo "expected failure output to mention internal/migration" >&2
  cat /tmp/vdb-guardian-coverage-fail.out >&2
  exit 1
fi

echo "check-go-coverage self-test ok"
