package main

import (
	"context"
	"fmt"
	"os"

	"github.com/h3xwave/vdb-guardian/internal/version"
)

// main is the CLI entrypoint for local and automation-driven vdb-guardian usage.
// The initial scaffold supports version output so operators can verify that the
// binary and repository are wired correctly before connector commands are added.
//
// main 是 vdb-guardian 的命令行 (CLI) 入口点，供本地调试或自动化脚本调用。
// 最初的脚手架仅支持输出版本信息，以便操作人员在添加连接器相关命令之前，
// 就能提前验证二进制文件和代码仓库是否已正确关联。
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
	if len(os.Args) > 1 && os.Args[1] == "seed-milvus" {
		if err := runSeedMilvusCommand(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "seed-milvus failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "search-milvus" {
		if err := runSearchMilvusCommand(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "search-milvus failed: %v\n", err)
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
	if len(os.Args) > 1 && os.Args[1] == "build-milvus-artifact" {
		if err := runMilvusArtifactCommand(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "build-milvus-artifact failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "compare-artifacts" {
		if err := runCompareArtifactsCommand(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "compare-artifacts failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "compare-full-records" {
		if err := runCompareFullRecordsCommand(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "compare-full-records failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "inspect-milvus" {
		if err := runInspectMilvusCommand(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "inspect-milvus failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "plan-pgvector-schema" {
		if err := runPlanPGVectorSchemaCommand(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "plan-pgvector-schema failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "compare-schema-plans" {
		if err := runCompareSchemaPlansCommand(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "compare-schema-plans failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "map-migration-records" {
		if err := runMapMigrationRecordsCommand(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "map-migration-records failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) > 1 && os.Args[1] == "compare-applied-schema" {
		if err := runCompareAppliedSchemaCommand(os.Args[2:], os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "compare-applied-schema failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "inspect-pgvector-schema" {
		if err := runInspectPGVectorSchemaCommand(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "inspect-pgvector-schema: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "apply-pgvector-schema" {
		if err := runApplyPGVectorSchemaCommand(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "apply-pgvector-schema failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := runMigrateCommand(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "migrate failed: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "migrate-and-verify" {
		if err := runMigrateAndVerifyCommand(context.Background(), os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "migrate-and-verify failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	fmt.Printf("%s: enterprise vector database migration verifier\n", info.Name)
}
