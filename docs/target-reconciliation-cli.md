# Target reconciliation and stale cleanup CLI

`vdbg reconcile-target` and `vdbg cleanup-target-stale` provide a guarded audit-then-delete workflow for pgvector rows that no longer exist in the Milvus source after upsert-style migrations.

The commands are intentionally split:

1. `reconcile-target` is artifact-only and never connects to Milvus or pgvector.
2. `cleanup-target-stale` is destructive and requires explicit confirmation before deleting only IDs classified as stale by a reconciliation report.

## Reconcile target artifacts

Build source/target full-record artifacts first, then reconcile them locally:

```bash
go run ./cmd/vdbg reconcile-target \
  --source /tmp/vdb-guardian-source-full-records.json \
  --target /tmp/vdb-guardian-target-full-records.json \
  --output /tmp/vdb-guardian-target-reconciliation.json
```

`reconcile-target` writes a `0600` JSON report with:

- `schema_version`
- `status`: `pass` or `fail`
- `source` and `target` endpoint metadata from the full-record artifacts
- `summary` counts for source, target, matched, stale, missing, and changed records
- `stale_target_ids`
- `missing_target_ids`
- `changed_record_ids`

The command exits non-zero when any stale, missing, or changed records are found, but it still writes the report for diagnosis and possible cleanup. ID lists are sorted for deterministic diffs.

## Cleanup stale target rows

Only stale target IDs are eligible for deletion. Missing target IDs and changed record IDs are never deleted by this command.

```bash
go run ./cmd/vdbg cleanup-target-stale \
  --reconcile-report /tmp/vdb-guardian-target-reconciliation.json \
  --pgvector-connection-url '[REDACTED]' \
  --target-table items \
  --target-id-column id \
  --output /tmp/vdb-guardian-target-stale-cleanup.json \
  --confirm-delete-stale
```

Required flags:

- `--reconcile-report`: target reconciliation report JSON path.
- `--pgvector-connection-url`: PostgreSQL/pgvector connection URL. Redact in logs, docs, tickets, and PRs.
- `--target-table`: pgvector target table to delete from.
- `--output`: cleanup result JSON path.
- `--confirm-delete-stale`: explicit destructive confirmation.

Optional flags:

- `--target-id-column`: pgvector target ID column. Defaults to `id`.

The cleanup result is written as `0600` JSON and contains the target table, requested delete count, deleted count, and deleted stale IDs. It never stores the connection URL.

## Safety boundaries

- Reconciliation is read-only and artifact-only.
- Cleanup fails closed unless `--confirm-delete-stale` is supplied when stale IDs exist.
- Cleanup consumes only `stale_target_ids` from the report.
- Cleanup validates report schema, status, count consistency, and aggregate source/target counts before DML.
- pgvector deletes use validated/quoted identifiers and bind stale IDs as a query parameter array.
- Connection and delete errors are sanitized to avoid leaking connection URLs, passwords, or stale IDs.
- Reconciliation reports and cleanup results are sensitive local artifacts and are written with `0600` permissions.

## Local Docker smoke

For migration-critical reconciliation/cleanup changes, run the opt-in local smoke:

```bash
make smoke-target-reconciliation-cleanup
```

The smoke starts/checks the disposable migration stack, seeds the committed small fixture into Milvus and pgvector, inserts one target-only pgvector row, builds source/target full-record artifacts, verifies `stale_target_count=1`, runs explicit stale cleanup, verifies the pgvector row count returns to `100`, rebuilds/reconciles target artifacts, verifies `stale_target_count=0`, checks `0600` artifact permissions, and scans generated artifacts for obvious secret markers.

It is intentionally outside `make test` because it requires Docker and local ports. Never point it at production databases.
