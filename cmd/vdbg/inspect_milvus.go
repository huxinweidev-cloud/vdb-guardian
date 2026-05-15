package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/h3xwave/vdb-guardian/internal/inspection"
)

type inspectMilvusOptions struct {
	Address    string
	Collection string
	OutputPath string
}

type inspectMilvusRunner func(context.Context, inspectMilvusOptions) (inspection.MilvusInspectionPlan, error)

// runInspectMilvusCommand inspects Milvus metadata and emits a read-only
// migration planning JSON document without moving records or mutating targets.
func runInspectMilvusCommand(ctx context.Context, args []string) error {
	return runInspectMilvusWithRunner(ctx, args, runRealInspectMilvus, os.Stdout)
}

func runInspectMilvusWithRunner(ctx context.Context, args []string, runner inspectMilvusRunner, stdout io.Writer) error {
	options, err := parseInspectMilvusOptions(args)
	if err != nil {
		return err
	}
	plan, err := runner(ctx, options)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal milvus inspection plan: %w", err)
	}
	data = append(data, '\n')
	if options.OutputPath == "" {
		_, err = stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(options.OutputPath), 0o755); err != nil {
		return fmt.Errorf("create inspect-milvus output dir: %w", err)
	}
	if err := os.WriteFile(options.OutputPath, data, 0o600); err != nil {
		return fmt.Errorf("write inspect-milvus output %q: %w", options.OutputPath, err)
	}
	fmt.Fprintf(stdout, "inspection completed\n")
	fmt.Fprintf(stdout, "output: %s\n", options.OutputPath)
	fmt.Fprintf(stdout, "collections: %d\n", plan.Summary.CollectionCount)
	fmt.Fprintf(stdout, "warnings: %d\n", plan.Summary.WarningCount)
	fmt.Fprintf(stdout, "unsupported_features: %d\n", plan.Summary.UnsupportedFeatureCount)
	return nil
}

func parseInspectMilvusOptions(args []string) (inspectMilvusOptions, error) {
	flagSet := flag.NewFlagSet("inspect-milvus", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	var options inspectMilvusOptions
	flagSet.StringVar(&options.Address, "milvus-address", "", "Milvus gRPC endpoint to inspect")
	flagSet.StringVar(&options.Collection, "collection", "", "optional single Milvus collection to inspect")
	flagSet.StringVar(&options.OutputPath, "output", "", "optional JSON output path; stdout is used when omitted")
	if err := flagSet.Parse(args); err != nil {
		return inspectMilvusOptions{}, err
	}
	if options.Address == "" {
		return inspectMilvusOptions{}, fmt.Errorf("milvus-address is required")
	}
	return options, nil
}

func runRealInspectMilvus(ctx context.Context, options inspectMilvusOptions) (inspection.MilvusInspectionPlan, error) {
	client, err := inspection.NewRealMilvusMetadataClient(ctx, options.Address)
	if err != nil {
		return inspection.MilvusInspectionPlan{}, err
	}
	defer client.Close()
	inspector := inspection.NewMilvusInspector(client, inspection.MilvusInspectorOptions{
		Address:    options.Address,
		Collection: options.Collection,
	})
	return inspector.Inspect(ctx)
}
