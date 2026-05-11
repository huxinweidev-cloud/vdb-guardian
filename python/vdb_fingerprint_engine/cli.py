"""Command-line entrypoint for the Python fingerprint engine.

Python 指纹引擎的命令行入口点。
"""

import argparse
import json
import sys
from pathlib import Path

from pydantic import ValidationError

from vdb_fingerprint_engine import __version__
from vdb_fingerprint_engine.artifact_compare import compare_fingerprint_artifacts
from vdb_fingerprint_engine.schemas import CompareInput, CompareOutput


def build_parser() -> argparse.ArgumentParser:
    """Build the command-line parser for the fingerprint engine.

    构建指纹引擎的命令行解析器。

    Returns:
        An argparse parser that supports version output and the JSON-based
        compare command invoked by the Go control plane.
    """
    parser = argparse.ArgumentParser(prog="vdb-fingerprint-engine")
    parser.add_argument("--version", action="store_true", help="print engine version and exit")
    subparsers = parser.add_subparsers(dest="command")

    compare_parser = subparsers.add_parser("compare", help="compare fingerprint artifacts")
    compare_parser.add_argument("--input", required=True, help="path to compare input JSON")
    compare_parser.add_argument("--output", required=True, help="path to compare output JSON")
    return parser


def run_compare(input_path: Path, output_path: Path) -> CompareOutput:
    """Run artifact-backed fingerprint comparison from a JSON protocol request.

    The command reads a Go-generated CompareInput payload, loads the source and
    target fingerprint artifact files referenced by that payload, computes
    retrieval behavior consistency metrics, and writes a CompareOutput JSON file.

    基于 JSON 协议请求，执行以产物为后盾的指纹比对。

    该命令会读取由 Go 控制平面生成的 CompareInput 负载，加载该负载中所引用的源端
    与目标端指纹产物文件，测算出检索行为的一致性指标，并最终写入一份 CompareOutput
    的 JSON 文件。

    Args:
        input_path: Path to the JSON CompareInput payload created by Go.
        output_path: Path where the JSON CompareOutput payload should be written.

    Returns:
        The CompareOutput object written to disk.

    Raises:
        FileNotFoundError: If the input JSON file or either fingerprint artifact is missing.
        ValueError: If an artifact is empty or semantically invalid.
        ValidationError: If the input payload or artifact payload does not match the schema.
        OSError: If reading or writing files fails.
    """
    payload = json.loads(input_path.read_text(encoding="utf-8"))
    compare_input = CompareInput.model_validate(payload)
    output = compare_fingerprint_artifacts(
        compare_input.job_id,
        Path(compare_input.source_fingerprint_path),
        Path(compare_input.target_fingerprint_path),
    )
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(output.model_dump_json(indent=2), encoding="utf-8")
    return output


def main() -> int:
    """Run the fingerprint engine command line interface.

    运行指纹引擎的命令行接口。

    Returns:
        Process exit code. A zero value indicates successful command handling.
    """
    parser = build_parser()
    args = parser.parse_args()
    if args.version:
        print(f"vdb-fingerprint-engine {__version__}")
        return 0

    if args.command == "compare":
        try:
            run_compare(Path(args.input), Path(args.output))
        except (
            FileNotFoundError,
            ValueError,
            ValidationError,
            json.JSONDecodeError,
            OSError,
        ) as exc:
            print(f"compare input error: {exc}", file=sys.stderr)
            return 1
        return 0

    parser.print_help()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
