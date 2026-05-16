#!/usr/bin/env bash
set -euo pipefail

TOTAL_MIN="${VDBG_COVER_TOTAL_MIN:-70.0}"
CMD_VDBG_MIN="${VDBG_COVER_CMD_VDBG_MIN:-65.0}"
MIGRATION_MIN="${VDBG_COVER_MIGRATION_MIN:-70.0}"
REPORTING_MIN="${VDBG_COVER_REPORTING_MIN:-85.0}"

CMD_VDBG_PKG="github.com/h3xwave/vdb-guardian/cmd/vdbg"
MIGRATION_PKG="github.com/h3xwave/vdb-guardian/internal/migration"
REPORTING_PKG="github.com/h3xwave/vdb-guardian/internal/reporting"

usage() {
  cat <<'USAGE'
Usage: scripts/check-go-coverage.sh [coverage-summary-file]

Without a fixture file, runs Go coverage commands and enforces migration-critical
coverage thresholds. A fixture file may contain whitespace-separated rows:

  github.com/h3xwave/vdb-guardian/cmd/vdbg 66.0
  github.com/h3xwave/vdb-guardian/internal/migration 71.0
  github.com/h3xwave/vdb-guardian/internal/reporting 89.0
  total 70.5

Thresholds can be overridden with:
  VDBG_COVER_TOTAL_MIN
  VDBG_COVER_CMD_VDBG_MIN
  VDBG_COVER_MIGRATION_MIN
  VDBG_COVER_REPORTING_MIN
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

coverage_lt() {
  awk -v got="$1" -v min="$2" 'BEGIN { exit !(got + 0 < min + 0) }'
}

extract_percent() {
  sed -nE 's/.*coverage: ([0-9]+(\.[0-9]+)?)% of statements.*/\1/p' | tail -n 1
}

read_summary_file() {
  local file="$1"
  awk -v cmd_pkg="${CMD_VDBG_PKG}" -v migration_pkg="${MIGRATION_PKG}" -v reporting_pkg="${REPORTING_PKG}" '
    $1 == cmd_pkg { print "cmd_vdbg " $2 }
    $1 == migration_pkg { print "migration " $2 }
    $1 == reporting_pkg { print "reporting " $2 }
    $1 == "total" { print "total " $NF }
  ' "${file}" | sed 's/%$//'
}

run_live_summary() {
  local tmp_dir cover_profile cmd_out migration_out reporting_out total_out
  tmp_dir="$(mktemp -d /tmp/vdb-guardian-coverage.XXXXXX)"
  trap 'rm -rf "${tmp_dir}"' RETURN

  cmd_out="$(go test ./cmd/vdbg -cover)"
  migration_out="$(go test ./internal/migration -cover)"
  reporting_out="$(go test ./internal/reporting -cover)"
  go test ./... -coverprofile="${tmp_dir}/coverage.out" -covermode=atomic >/dev/null
  total_out="$(go tool cover -func="${tmp_dir}/coverage.out" | tail -n 1)"

  printf 'cmd_vdbg %s\n' "$(printf '%s\n' "${cmd_out}" | extract_percent)"
  printf 'migration %s\n' "$(printf '%s\n' "${migration_out}" | extract_percent)"
  printf 'reporting %s\n' "$(printf '%s\n' "${reporting_out}" | extract_percent)"
  printf 'total %s\n' "$(printf '%s\n' "${total_out}" | awk '{print $NF}' | sed 's/%$//')"
}

summary=""
if [[ $# -gt 0 ]]; then
  summary="$(read_summary_file "$1")"
else
  summary="$(run_live_summary)"
fi

cmd_vdbg="$(printf '%s\n' "${summary}" | awk '$1 == "cmd_vdbg" {print $2}' | tail -n 1)"
migration="$(printf '%s\n' "${summary}" | awk '$1 == "migration" {print $2}' | tail -n 1)"
reporting="$(printf '%s\n' "${summary}" | awk '$1 == "reporting" {print $2}' | tail -n 1)"
total="$(printf '%s\n' "${summary}" | awk '$1 == "total" {print $2}' | tail -n 1)"

missing=0
for value_name in cmd_vdbg migration reporting total; do
  if [[ -z "${!value_name:-}" ]]; then
    echo "coverage-check: missing ${value_name} coverage value" >&2
    missing=1
  fi
done
if [[ "${missing}" -ne 0 ]]; then
  echo "coverage-check: summary was:" >&2
  printf '%s\n' "${summary}" >&2
  exit 1
fi

failures=0
check_threshold() {
  local label="$1" got="$2" min="$3"
  if coverage_lt "${got}" "${min}"; then
    printf 'coverage-check=fail package=%s coverage=%s threshold=%s\n' "${label}" "${got}" "${min}"
    failures=$((failures + 1))
  else
    printf 'coverage-check=pass package=%s coverage=%s threshold=%s\n' "${label}" "${got}" "${min}"
  fi
}

check_threshold "${CMD_VDBG_PKG}" "${cmd_vdbg}" "${CMD_VDBG_MIN}"
check_threshold "${MIGRATION_PKG}" "${migration}" "${MIGRATION_MIN}"
check_threshold "${REPORTING_PKG}" "${reporting}" "${REPORTING_MIN}"
check_threshold "total" "${total}" "${TOTAL_MIN}"

if [[ "${failures}" -gt 0 ]]; then
  exit 1
fi
