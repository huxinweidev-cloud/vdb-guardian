# pgvector COPY Complex Fields Smoke Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Harden pgvector COPY migration confidence by proving typed scalar fields, dynamic metadata, partition metadata, renamed source-to-target mappings, full-record compare, and target reconciliation all work against real PostgreSQL/pgvector in an opt-in Docker smoke.

**Architecture:** Keep the existing `make smoke-migration-copy` fast id/vector path unchanged and add a separate `make smoke-migration-copy-complex` gate. Extend the synthetic fixture/seeder path only as much as needed to produce deterministic scalar/dynamic/partition records, then run `vdbg migrate --pgvector-write-mode copy` through the existing schema/mapping/full-record/reconcile CLIs. The new smoke must use disposable local Docker services, redact connection URLs, write artifacts with `0600`, and remain outside ordinary `make test`.

**Tech Stack:** Go, Milvus SDK, pgx v5, PostgreSQL/pgvector, Bash smoke scripts, existing vdbg CLI, existing Docker migration stack.

---

## Current State Reconnaissance

### Already implemented

- `b496482 feat(migration): add pgvector copy write modes` added `batch-upsert`, `copy`, and `auto` pgvector write modes.
- `scripts/smoke-migration-copy.sh` validates the COPY path with the committed `testdata/migration/synthetic-small.json` fixture.
- The current COPY smoke verifies `requested=copy`, `used=copy`, `copy_batches=4`, `batch_upsert_batches=0`, `copy_fallbacks=0`, row count 100, full-record compare pass, target reconciliation `stale_target_count=0`, artifact `0600`, and generated-artifact secret scan.
- Existing docs describe mapped scalar fields, dynamic metadata, partition metadata, and COPY mode.
- `internal/fixtures.SyntheticDataset` / `SyntheticVector` currently only model `id` and `vector` payloads.
- The current smoke intentionally writes a minimal record mapping with `mapped_scalar_count=0` to keep the first COPY gate stable.

### Missing

- No committed live smoke proves COPY staging/merge handles typed scalar columns.
- No live smoke proves dynamic metadata round-trips through pgvector full-record artifacts.
- No live smoke proves partition metadata is preserved as planned metadata.
- No fixture path currently supports non-vector record payloads.
- No smoke covers renamed source field to target column mappings, which is important because identity mappings can hide source/target lookup bugs.

### Design constraints

- Do not weaken or slow down the existing `make smoke-migration-copy` id/vector smoke.
- Add an explicit opt-in smoke target, likely `make smoke-migration-copy-complex`.
- Do not put Docker smoke in ordinary `make test`.
- Do not commit real connection URLs, passwords, tokens, or credentials. Use `[REDACTED]` in docs and logs.
- Any generated report/checkpoint/artifact touched by the smoke must be checked for `0600` permissions.
- Keep default production behavior unchanged: `batch-upsert` remains default unless `--pgvector-write-mode copy|auto` is supplied.
- Preserve schema preflight, full-record compare, and reconciliation gates.

---

## Proposed Task Breakdown

### Task 1: Add Complex Fixture Model Support Without Changing Existing Fixtures

**Objective:** Extend the synthetic fixture model so JSON records can carry optional scalar fields, dynamic metadata, and partition metadata while keeping existing id/vector fixtures compatible.

**Files:**
- Modify: `internal/fixtures/synthetic.go`
- Modify: `internal/fixtures/synthetic_test.go`
- Possibly modify: `cmd/vdbg/milvus_seed_test.go`

**TDD steps:**
1. Add failing tests that unmarshal a synthetic record containing:
   - `title`
   - `price`
   - `quantity`
   - `active`
   - `category: null`
   - `dynamic_metadata`
   - `partition`
2. Verify existing `synthetic-small.json` still unmarshals.
3. Implement optional fields with `omitempty` where appropriate.
4. Run:
   ```bash
   go test ./internal/fixtures ./cmd/vdbg -run 'Test.*Synthetic|Test.*SeedMilvus' -v
   ```

**Acceptance criteria:**
- Existing fixtures are backward-compatible.
- New complex records preserve numeric, boolean, null, object, array, and partition values when decoded.

---

### Task 2: Teach Milvus Seeder to Insert Complex Fields and Partitions

**Objective:** Seed real Milvus with typed scalar fields, dynamic metadata, and partition labels from the complex fixture.

**Files:**
- Modify: Milvus seeder implementation under `internal/fixtures` or `internal/migration` as discovered.
- Modify: relevant seeder tests under `cmd/vdbg` / `internal`.

**TDD steps:**
1. Add fake seeder tests proving complex fixture fields are passed into the seeding layer.
2. Add validation tests for unsupported scalar shapes if needed.
3. Implement minimal seeder support for:
   - varchar/text scalar
   - int64 quantity
   - double/float price
   - bool active
   - nullable category where supported by existing schema path
   - dynamic metadata object
   - partition name
4. Run targeted tests:
   ```bash
   go test ./cmd/vdbg ./internal/fixtures ./internal/migration -run 'Test.*SeedMilvus|Test.*Synthetic|Test.*Milvus' -v
   ```

**Acceptance criteria:**
- Existing seed path remains compatible with id/vector fixtures.
- Complex fixture records can be seeded into real Milvus in Docker smoke.
- No real connection URLs or secrets are logged.

---

### Task 3: Add Committed Complex Synthetic Fixture

**Objective:** Add a small deterministic fixture that exercises typed scalars, metadata, partitions, and renamed field mapping without making the repository heavy.

**Files:**
- Create: `testdata/migration/synthetic-complex.json`

**Fixture shape:**
- 12–24 records, dimension 8, query_count >= 2.
- Fields per record:
  - `id`
  - `vector`
  - `title` string
  - `price` float
  - `quantity` integer
  - `active` boolean
  - `category` string or null
  - `dynamic_metadata` object with nested object and array
  - `partition` alternating `tenant_a` / `tenant_b`

**Acceptance criteria:**
- Small enough to review and commit.
- Contains at least one null scalar and one nested JSON metadata value.
- Uses deterministic local-only test values; no secrets.

---

### Task 4: Add Complex Record Mapping and Schema Gate Coverage

**Objective:** Generate or write a mapping artifact that covers renamed source fields, typed scalar target columns, dynamic metadata, and partition metadata.

**Files:**
- Modify or create smoke mapping generation inside `scripts/smoke-migration-copy-complex.sh`.
- Add unit tests if mapping helpers need code changes.

**Mapping requirements:**
- `source_field: title` → `target_column: item_title`
- `source_field: price` → `target_column: price_amount`
- `source_field: quantity` → `target_column: stock_count`
- `source_field: active` → `target_column: is_active`
- `source_field: category` → `target_column: category_name`
- dynamic metadata → JSONB target metadata column
- partition metadata → metadata column or dedicated field consistent with current mapping model

**Acceptance criteria:**
- Mapping status is `pass`.
- Renamed mappings are used in migration and full-record artifact construction.
- Schema preflight rejects incompatible target schema before migration.

---

### Task 5: Add Complex COPY Docker Smoke Script

**Objective:** Add an opt-in smoke script that runs a real COPY migration with complex fields and verifies end-to-end correctness.

**Files:**
- Create: `scripts/smoke-migration-copy-complex.sh`
- Modify: `Makefile`

**Smoke flow:**
1. Start/check disposable Docker migration stack.
2. Seed Milvus with `synthetic-complex.json`.
3. Run inspect/schema plan/apply/schema compare.
4. Build or write complex record mapping.
5. Run:
   ```bash
   vdbg migrate --pgvector-write-mode copy --record-mapping <complex-mapping>
   ```
6. Assert migration report metrics:
   - requested `copy`
   - used `copy`
   - `copy_batches > 0`
   - `batch_upsert_batches = 0`
   - `copy_fallbacks = 0`
7. Assert target row count equals fixture record_count.
8. Build source/target full-record artifacts.
9. Run full-record compare and require pass.
10. Run target reconciliation and require `stale_target_count=0`.
11. Check artifact permissions `0600`.
12. Run generated-artifact secret scan.

**Acceptance criteria:**
- `make smoke-migration-copy-complex` passes locally.
- Script redacts connection URL in logs.
- Script is not part of `make test`.

---

### Task 6: Add Auto Mode Complex Fallback Test Coverage

**Objective:** Verify complex-field migrations remain correct when `auto` falls back from recoverable COPY execution errors to batch-upsert.

**Files:**
- Modify: `internal/migration/vector_migration_test.go`
- Modify: `internal/migration/vector_migration_real_adapters_test.go` if needed

**TDD steps:**
1. Add fake target/writer test with complex scalar/metadata records where COPY returns a recoverable execution error.
2. Assert fallback writes the exact same records through batch-upsert.
3. Assert metrics/report:
   - requested `auto`
   - used `batch-upsert` or `mixed` depending on batch mix
   - `copy_fallbacks` increments
4. Assert validation/schema/context errors still do not fallback.

**Acceptance criteria:**
- Auto fallback remains conservative.
- Complex records are not mutated across fallback.

---

### Task 7: Update English and Chinese Docs

**Objective:** Document the complex COPY smoke, verified field coverage, and remaining limitations.

**Files:**
- Modify: `docs/migrate-cli.md`
- Modify: `docs/migrate-and-verify-cli.md`
- Modify: `docs/zh-CN/migrate-cli.md`
- Modify: `docs/zh-CN/migrate-and-verify-cli.md`
- Modify: `CONTRIBUTING.md`
- Modify: `CHANGELOG.md`

**Content:**
- Add `make smoke-migration-copy-complex`.
- Explain it verifies typed scalars, dynamic metadata, partition metadata, renamed mappings, full-record compare, and reconciliation.
- Note that it requires Docker/local ports and must not point at production DBs.
- Keep examples redacted.

**Acceptance criteria:**
- English and Chinese docs are synchronized.
- No docs imply complex smoke is part of ordinary `make test`.

---

### Task 8: Final Gates, Review, Commit, Push

**Objective:** Verify and land the complex COPY smoke hardening safely.

**Commands:**
```bash
make fmt
make lint
make test
make coverage-check
git diff --check
git diff --cached --check
make pre-commit
make smoke-migration-copy
make smoke-migration-copy-complex
```

**Security checks:**
```bash
git diff -U0 | grep -nEi '(^\+.*(api_key|secret|password|token|passwd|credential|client_secret)\s*=\s*['"'"'\"][^'"'"'\"]{6,}['"'"'\"]|^\+.*(Bearer |Authorization:|postgres://|postgresql://|redis://|mysql://|sk-|ghp_|github_pat_|AKIA|AIza|ya29\.|BEGIN .*PRIVATE))' || true
git ls-files --others --exclude-standard -z | xargs -0 -r grep -nEi '(api_key|secret|password|token|credential|client_secret|private key|BEGIN .*PRIVATE|Authorization:|Bearer |postgres://|postgresql://|redis://|mysql://|sk-|ghp_|github_pat_|AKIA|AIza|ya29\.)' || true
git ls-files --others --exclude-standard | grep -Ei '(^|/)(\.env|config\.ya?ml|.*secret.*|.*key.*|.*pem|.*crt|.*password.*|.*token.*)$' || true
```

**Review:**
- Run independent spec review.
- Run independent quality/security review.
- Fix blockers before commit.

**Commit:**
```bash
git add <changed-files>
git commit -m "test(migration): add complex pgvector copy smoke"
git push
```

---

## Risks and Mitigations

- **Milvus dynamic field support mismatch:** If the current seeder cannot create dynamic fields cleanly, keep Task 2 scoped to the minimal supported Milvus SDK path and document any unsupported shape.
- **Typed scalar COPY casting issues:** The smoke must use real PostgreSQL/pgvector, not only unit tests, because staging/merge casting behavior is the point of this phase.
- **Smoke runtime growth:** Keep the fixture small and use a separate opt-in target.
- **Secret leakage:** Redact URLs in logs and scan generated artifacts.
- **Mapping drift:** Include renamed source-to-target fields so tests prove the code reads source values and writes target columns correctly.

## Definition of Done

- `make smoke-migration-copy-complex` passes locally.
- Existing `make smoke-migration-copy` still passes.
- Full-record compare passes for complex fields.
- Target reconciliation reports `stale_target_count=0`.
- Artifacts are `0600`.
- No real secrets are committed or printed.
- Final gate and independent reviews pass.
