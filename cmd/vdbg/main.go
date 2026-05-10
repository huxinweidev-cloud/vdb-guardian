package main

import (
	"context"
	"fmt"
	"os"

	"github.com/huxinweidev-cloud/vdb-guardian/internal/version"
)

// main is the CLI entrypoint for local and automation-driven vdb-guardian usage.
// The initial scaffold supports version output so operators can verify that the
// binary and repository are wired correctly before connector commands are added.
func main() {
	info := version.Info()
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("%s %s\n", info.Name, info.Version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "offline-verify" {
		if err := runOfflineVerifyCommand(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "offline-verify failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "generate-synthetic-fixture" {
		if err := runGenerateSyntheticFixture(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "generate-synthetic-fixture failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "seed-pgvector" {
		if err := runSeedPGVectorCommand(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "seed-pgvector failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "search-pgvector" {
		if err := runSearchPGVectorCommand(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "search-pgvector failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "build-pgvector-artifact" {
		if err := runPGVectorArtifactCommand(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "build-pgvector-artifact failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	fmt.Printf("%s: enterprise vector database migration verifier\n", info.Name)
}
