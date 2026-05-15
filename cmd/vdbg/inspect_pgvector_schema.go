package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/h3xwave/vdb-guardian/internal/schema"
	"github.com/jackc/pgx/v5"
)

type inspectPGVectorSchemaOptions struct {
	ConnectionURL string
	TargetSchema  string
	OutputPath    string
}

type inspectPGVectorSchemaClientFactory func(connectionURL string) (schema.PGVectorSchemaMetadataClient, func() error, error)

func runInspectPGVectorSchemaCommand(ctx context.Context, args []string) error {
	return runInspectPGVectorSchemaCommandWithFactory(ctx, args, os.Stdout, newPGVectorSchemaMetadataClient)
}

func runInspectPGVectorSchemaCommandWithFactory(ctx context.Context, args []string, stdout io.Writer, factory inspectPGVectorSchemaClientFactory) error {
	options, err := parseInspectPGVectorSchemaOptions(args)
	if err != nil {
		return err
	}
	client, closeClient, err := factory(options.ConnectionURL)
	if err != nil {
		return err
	}
	if closeClient != nil {
		defer func() {
			closeErr := closeClient()
			if closeErr != nil {
				_, _ = fmt.Fprintf(io.Discard, "%v", closeErr)
			}
		}()
	}
	inspection, err := schema.InspectPGVectorSchema(ctx, client, schema.PGVectorLiveSchemaInspectOptions{TargetSchema: options.TargetSchema})
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(inspection, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pgvector schema inspection: %w", err)
	}
	encoded = append(encoded, '\n')
	if options.OutputPath == "" {
		_, err = stdout.Write(encoded)
		return err
	}
	if err = os.WriteFile(options.OutputPath, encoded, 0o600); err != nil {
		return fmt.Errorf("write pgvector schema inspection %q: %w", options.OutputPath, err)
	}
	fmt.Fprintf(stdout, "pgvector_schema_inspection: %s\n", options.OutputPath)
	fmt.Fprintf(stdout, "schema: %s\n", inspection.Target.Schema)
	fmt.Fprintf(stdout, "tables: %d\n", inspection.Summary.TableCount)
	fmt.Fprintf(stdout, "columns: %d\n", inspection.Summary.ColumnCount)
	fmt.Fprintf(stdout, "indexes: %d\n", inspection.Summary.IndexCount)
	fmt.Fprintf(stdout, "warnings: %d\n", inspection.Summary.WarningCount)
	return nil
}

func parseInspectPGVectorSchemaOptions(args []string) (inspectPGVectorSchemaOptions, error) {
	options := inspectPGVectorSchemaOptions{TargetSchema: "public"}
	flags := flag.NewFlagSet("inspect-pgvector-schema", flag.ContinueOnError)
	flags.StringVar(&options.ConnectionURL, "pgvector-connection-url", "", "PostgreSQL/pgvector connection URL")
	flags.StringVar(&options.TargetSchema, "target-schema", "public", "PostgreSQL schema to inspect")
	flags.StringVar(&options.OutputPath, "output", "", "Optional output path for live pgvector schema inspection JSON")
	if err := flags.Parse(args); err != nil {
		return inspectPGVectorSchemaOptions{}, err
	}
	if options.ConnectionURL == "" {
		return inspectPGVectorSchemaOptions{}, errors.New("pgvector-connection-url is required")
	}
	if options.TargetSchema == "" {
		return inspectPGVectorSchemaOptions{}, errors.New("target-schema must not be empty")
	}
	return options, nil
}

type pgxPGVectorSchemaMetadataClient struct {
	conn *pgx.Conn
}

func newPGVectorSchemaMetadataClient(connectionURL string) (schema.PGVectorSchemaMetadataClient, func() error, error) {
	conn, err := pgx.Connect(context.Background(), connectionURL)
	if err != nil {
		return nil, nil, fmt.Errorf("connect pgvector database: %w", err)
	}
	client := &pgxPGVectorSchemaMetadataClient{conn: conn}
	return client, func() error { return conn.Close(context.Background()) }, nil
}

func (client *pgxPGVectorSchemaMetadataClient) InspectVectorExtension(ctx context.Context) (schema.PGVectorExtensionMetadata, error) {
	var version string
	err := client.conn.QueryRow(ctx, `SELECT extversion FROM pg_extension WHERE extname = 'vector'`).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return schema.PGVectorExtensionMetadata{Installed: false}, nil
	}
	if err != nil {
		return schema.PGVectorExtensionMetadata{}, err
	}
	return schema.PGVectorExtensionMetadata{Installed: true, Version: version}, nil
}

func (client *pgxPGVectorSchemaMetadataClient) ListSchemaColumns(ctx context.Context, targetSchema string) ([]schema.PGVectorLiveColumnMetadata, error) {
	rows, err := client.conn.Query(ctx, `
SELECT
  c.table_name,
  c.column_name,
  pg_catalog.format_type(a.atttypid, a.atttypmod) AS formatted_type,
  c.data_type,
  c.udt_name,
  c.is_nullable,
  c.ordinal_position
FROM information_schema.columns c
JOIN pg_catalog.pg_class cls ON cls.relname = c.table_name
JOIN pg_catalog.pg_namespace ns ON ns.oid = cls.relnamespace AND ns.nspname = c.table_schema
JOIN pg_catalog.pg_attribute a ON a.attrelid = cls.oid AND a.attname = c.column_name
WHERE c.table_schema = $1
  AND cls.relkind IN ('r', 'p')
  AND a.attnum > 0
  AND NOT a.attisdropped
ORDER BY c.table_name, c.ordinal_position`, targetSchema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []schema.PGVectorLiveColumnMetadata
	for rows.Next() {
		var column schema.PGVectorLiveColumnMetadata
		var nullable string
		if err = rows.Scan(&column.TableName, &column.ColumnName, &column.FormattedType, &column.DataType, &column.UDTName, &nullable, &column.OrdinalPosition); err != nil {
			return nil, err
		}
		column.IsNullable = nullable == "YES"
		columns = append(columns, column)
	}
	return columns, rows.Err()
}

func (client *pgxPGVectorSchemaMetadataClient) ListPrimaryKeys(ctx context.Context, targetSchema string) ([]schema.PGVectorLivePrimaryKeyMetadata, error) {
	rows, err := client.conn.Query(ctx, `
SELECT
  tc.table_name,
  kcu.column_name
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name
 AND tc.table_schema = kcu.table_schema
WHERE tc.constraint_type = 'PRIMARY KEY'
  AND tc.table_schema = $1
ORDER BY tc.table_name, kcu.ordinal_position`, targetSchema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []schema.PGVectorLivePrimaryKeyMetadata
	for rows.Next() {
		var key schema.PGVectorLivePrimaryKeyMetadata
		if err = rows.Scan(&key.TableName, &key.ColumnName); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (client *pgxPGVectorSchemaMetadataClient) ListIndexes(ctx context.Context, targetSchema string) ([]schema.PGVectorLiveIndexMetadata, error) {
	rows, err := client.conn.Query(ctx, `
SELECT
  t.relname AS table_name,
  i.relname AS index_name,
  am.amname AS method,
  pg_get_indexdef(i.oid) AS definition
FROM pg_index ix
JOIN pg_class t ON t.oid = ix.indrelid
JOIN pg_class i ON i.oid = ix.indexrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
JOIN pg_am am ON am.oid = i.relam
WHERE n.nspname = $1
ORDER BY t.relname, i.relname`, targetSchema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var indexes []schema.PGVectorLiveIndexMetadata
	for rows.Next() {
		var index schema.PGVectorLiveIndexMetadata
		if err = rows.Scan(&index.TableName, &index.IndexName, &index.Method, &index.Definition); err != nil {
			return nil, err
		}
		indexes = append(indexes, index)
	}
	return indexes, rows.Err()
}
