# Migration Checkpoint E2E and Coverage Gate Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Add a repeatable coverage gate and local real-service checkpoint/resume E2E smoke for the Milvus → pgvector migration path.

**Architecture:** Keep fast unit/CLI tests in `make test` and add an explicit opt-in smoke target for Docker-backed Milvus/PostgreSQL validation. Coverage enforcement should be scriptable, machine-readable, and focus first on the migration-critical Go packages rather than pretending global coverage alone proves correctness.

**Tech Stack:** Go test coverage, Bash scripts, Docker Compose v2, existing `vdbg` CLI commands, existing Milvus/pgvector migration stack, Python `pytest --cov` already in Makefile.

---

## Repository constraints

- Follow `CLAUDE.md`: plan first, wait for owner confirmation before large code changes.
- Strict TDD for production code and scripts: write failing tests/checks first where practical.
- No real credentials in code, docs, tests, commits, or chat. Use environment variables and redact connection URLs.
- Use `docker compose`, not legacy `docker-compose`.
- Keep generated artifacts under a disposable temp directory and write sensitive reports/checkpoints with `0600`.
- Do not add Docker E2E to default `make test`; it should be opt-in because it requires local services and ports.

## Current baseline

Latest clean branch state before this plan:

```text
feat/enterprise-scaffold...origin/feat/enterprise-scaffold
```

Observed coverage baseline from `go test ./... -coverprofile=/tmp/vdb-guardian-cover.out -covermode=atomic`:

```text
total: 70.4%
cmd/vdbg: 65.8%
internal/migration: 71.7%
internal/reporting: 88.9%
internal/schema: 83.5%
internal/inspection: 44.0%
```

Existing Makefile already has:

```make
coverage-go:
	go test -race -coverprofile=coverage.txt -covermode=atomic ./...
	go tool cover -func=coverage.txt | tail -1

migration-stack-up:
	docker compose -f deployments/docker-compose.migration.yml up -d
```

---

## Acceptance criteria

1. `make coverage-check` fails when total Go coverage or critical package coverage is below configured thresholds.
2. Coverage thresholds start realistic and enforce only migration-critical packages first:
   - total >= `70.0` initially, with docs recommending ratcheting to `75.0`.
   - `github.com/h3xwave/vdb-guardian/internal/migration` >= `70.0` initially, with docs recommending ratcheting to `80.0` after edge tests.
   - `github.com/h3xwave/vdb-guardian/cmd/vdbg` >= `65.0` initially, with docs recommending ratcheting to `72.0` after CLI edge tests.
   - `github.com/h3xwave/vdb-guardian/internal/reporting` >= `85.0`.
3. `make smoke-migration-checkpoint` runs a local Docker-backed checkpoint/resume smoke and prints clear pass/fail markers.
4. Smoke script validates at least:
   - migration stack health;
   - Milvus seed/search;
   - schema/mapping gates;
   - initial checkpointed migration;
   - resume path via `migrate-and-verify --resume-from`;
   - final row count and fingerprint/full-record comparison success;
   - checkpoint/report artifact permissions;
   - artifact secret scan.
5. Scripts do not print real connection URLs; user-facing output uses redacted placeholders.
6. Docs explain the coverage matrix and the opt-in Docker smoke workflow in English and Chinese if corresponding docs are touched.
7. Full quality gate passes before commit:
   ```bash
   make fmt
   make lint
   make test
   make coverage-check
   git diff --check
   git diff --cached --check
   make pre-commit
   ```

---

## Task 1: Add Go coverage gate script tests

**Objective:** Define the expected coverage-gate behavior before implementing the script.

**Files:**
- Create: `scripts/check-go-coverage.sh`
- Create or modify: `scripts/check-go-coverage_test.sh` only if the repository accepts shell tests; otherwise use a lightweight Bash self-test target.
- Modify: `Makefile`

**Step 1: Write failing shell-level checks**

Create temporary coverage function output in the test script:

```text
github.com/h3xwave/vdb-guardian/cmd/vdbg/file.go:10: Run 66.0%
github.com/h3xwave/vdb-guardian/internal/migration/file.go:20: Migrate 71.0%
github.com/h3xwave/vdb-guardian/internal/reporting/file.go:30: Render 89.0%
total: (statements) 70.5%
```

Assert `scripts/check-go-coverage.sh fixture.txt` exits `0`.

Create a failing fixture where `internal/migration` is `69.9%`; assert non-zero and output contains `internal/migration`.

**Step 2: Run and verify RED**

```bash
bash scripts/check-go-coverage_test.sh
```

Expected: FAIL because `scripts/check-go-coverage.sh` does not exist or does not enforce thresholds yet.

**Step 3: Implement minimal script**

`scripts/check-go-coverage.sh` should:

- accept an optional `go tool cover -func` output file path;
- otherwise read from stdin;
- parse per-package coverage by aggregating function lines by package prefix is hard; for MVP, parse exact package lines if `go test -cover` package summary is supplied or parse `go tool cover -func` for total only plus package lines from a generated package summary file;
- simpler, robust approach: run package-level commands inside the script:
  ```bash
  go test ./cmd/vdbg -cover
  go test ./internal/migration -cover
  go test ./internal/reporting -cover
  go test ./... -coverprofile=/tmp/vdb-guardian-coverage.out -covermode=atomic
  go tool cover -func=/tmp/vdb-guardian-coverage.out | tail -1
  ```
- support env overrides:
  ```bash
  VDBG_COVER_TOTAL_MIN=70.0
  VDBG_COVER_CMD_VDBG_MIN=65.0
  VDBG_COVER_MIGRATION_MIN=70.0
  VDBG_COVER_REPORTING_MIN=85.0
  ```
- print summary without secrets.

**Step 4: Verify GREEN**

```bash
bash scripts/check-go-coverage.sh
```

Expected: PASS at current baseline.

---

## Task 2: Wire coverage gate into Makefile

**Objective:** Make coverage enforcement discoverable and easy to run.

**Files:**
- Modify: `Makefile`
- Modify: `CONTRIBUTING.md` if coverage commands are documented there.

**Step 1: Add Makefile target**

Add `.PHONY` target:

```make
coverage-check:
	scripts/check-go-coverage.sh
```

Do **not** put it into `make test` yet. Consider adding it to `ci` only after thresholds stabilize; for this phase, document as explicit pre-merge gate for migration work.

**Step 2: Verify**

```bash
make coverage-check
```

Expected: PASS and prints package threshold summary.

---

## Task 3: Add checkpoint/resume edge tests for coverage quality

**Objective:** Increase meaningful `internal/migration` coverage before relying on the gate.

**Files:**
- Modify: `internal/migration/checkpoint_test.go`
- Maybe modify: `internal/migration/vector_migration_test.go`

**RED tests to add:**

1. Malformed checkpoint JSON:
   ```go
   func TestReadVectorMigrationCheckpointRejectsMalformedJSON(t *testing.T)
   ```
2. Unsupported checkpoint version:
   ```go
   func TestValidateVectorMigrationResumeRejectsUnsupportedVersion(t *testing.T)
   ```
3. Invalid checkpoint invariants:
   - negative offset;
   - offset exceeds records read;
   - completed batch record sum mismatches `records_written`.
4. Secret-like write error redaction:
   ```go
   func TestSanitizeVectorMigrationCheckpointErrorRedactsSecretMarkers(t *testing.T)
   ```
5. Existing broad-permission checkpoint/report overwrite tightens to `0600`.

**Commands:**

```bash
go test ./internal/migration -run 'Test.*Checkpoint|Test.*Resume|TestSanitize' -v
```

Expected RED before implementation if helpers are missing, then GREEN after minimal fixes.

---

## Task 4: Add local E2E checkpoint/resume smoke script

**Objective:** Prove the checkpoint/resume workflow against real Milvus and PostgreSQL/pgvector containers.

**Files:**
- Create: `scripts/smoke-migration-checkpoint-resume.sh`
- Modify: `Makefile`
- Maybe update: `.gitignore` only if script creates local artifact dirs under repo; prefer `/tmp` to avoid this.

**Script design:**

- Use strict Bash:
  ```bash
  set -euo pipefail
  ```
- Use temp run dir:
  ```bash
  RUN_DIR="${VDBG_SMOKE_RUN_DIR:-$(mktemp -d /tmp/vdb-guardian-checkpoint-smoke.XXXXXX)}"
  ```
- Use env-based local connection URL:
  ```bash
  VDB_GUARDIAN_LOCAL_PG_URL="${VDB_GUARDIAN_LOCAL_PG_URL:-postgres://[local placeholder built from compose defaults]}"
  ```
  Do not echo the actual value.
- Start/check stack using existing commands:
  ```bash
  make migration-stack-config
  make migration-stack-up
  make migration-stack-check
  ```
- Run command chain from `test-driven-development` reference `vdb-guardian-local-e2e-smoke.md`.
- For checkpoint/resume specifically use a small batch size, e.g. `--batch-size 25`, so 100 records create four batches.

**Minimum pass markers:**

```text
stack_ready=pass
milvus_seed=pass
schema_gate=pass
checkpoint_written=pass
resume_verify=pass
row_count=100
artifact_permissions=pass
secret_scan=pass
smoke_result=pass
```

**Secret scan:**

Run grep over `$RUN_DIR` for obvious markers, but allow expected words in documentation only. For generated artifacts, fail on:

```text
postgres://
postgresql://
password
token
credential
Bearer
api_key
```

**Permissions check:**

Use `stat -c '%a'` and require `600` for checkpoint and machine-readable reports.

**Step: Verify manually**

```bash
make smoke-migration-checkpoint
```

Expected: PASS markers above.

---

## Task 5: Document coverage and E2E quality matrix

**Objective:** Make “全面覆盖” operational instead of informal.

**Files:**
- Modify: `CONTRIBUTING.md`
- Modify: `docs/migrate-cli.md`
- Modify: `docs/migrate-and-verify-cli.md`
- Modify Chinese equivalents under `docs/zh-CN/` if English docs are updated.

**Content to add:**

- Coverage gates are necessary but insufficient.
- Required matrix for migration changes:
  - unit logic;
  - runner batch/resume;
  - CLI flags/errors;
  - artifact/report schema;
  - Docker E2E smoke;
  - secret/permission checks.
- Commands:
  ```bash
  make coverage-check
  make smoke-migration-checkpoint
  ```
- Explain Docker smoke is opt-in and local-only; never use production credentials.

---

## Task 6: Run final quality and security gates

**Objective:** Verify before commit.

Run:

```bash
make fmt
make lint
make test
make coverage-check
git diff --check
git diff --cached --check
make pre-commit
```

Run optional Docker smoke if local Docker is available:

```bash
make smoke-migration-checkpoint
```

Secret scan staged/tracked/untracked changes:

```bash
git diff --cached | grep -E -i 'api[_-]?key|secret|password|token|credential|postgres://|postgresql://|Bearer' || true
git diff | grep -E -i 'api[_-]?key|secret|password|token|credential|postgres://|postgresql://|Bearer' || true
git ls-files --others --exclude-standard -z | xargs -0 -r grep -nE -i 'api[_-]?key|secret|password|token|credential|postgres://|postgresql://|Bearer' || true
```

Expected: only placeholders, redaction text, or secret-scan marker lists.

---

## Task 7: Commit and push

**Objective:** Land the quality improvements.

```bash
git add Makefile scripts/check-go-coverage.sh scripts/smoke-migration-checkpoint-resume.sh CONTRIBUTING.md docs/migrate-cli.md docs/migrate-and-verify-cli.md docs/zh-CN/migrate-cli.md docs/zh-CN/migrate-and-verify-cli.md internal/migration/*_test.go
git commit -m "test(migration): add checkpoint coverage and e2e gates"
git push
```

---

## Risks and mitigations

- **Docker stack flakiness:** keep smoke opt-in, add health checks and clear errors.
- **Coverage thresholds too strict too soon:** start at current baseline and ratchet in follow-up commits.
- **Secret false positives:** scan generated artifacts only for real URL/credential patterns; document allowed redaction text.
- **Slow developer loop:** keep `make test` fast; run smoke only when migration behavior changes.
- **Current source read is all-at-once:** document that E2E validates target write checkpoint/resume, not source cursor streaming.

---

## Confirmation gate

This is a multi-step quality infrastructure change. Per `CLAUDE.md`, stop after committing the plan and wait for owner confirmation before implementing scripts/tests/docs.
