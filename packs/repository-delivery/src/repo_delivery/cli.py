"""JSON-only local command interface."""
import argparse
import json
import sys
from .core import DeliveryError, inspect, compare, create_checkpoint, verify_checkpoint


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    inspection = commands.add_parser("inspect")
    inspection.add_argument("--repo", required=True)
    comparison = commands.add_parser("compare")
    comparison.add_argument("--repo", required=True)
    comparison.add_argument("--baseline", required=True)
    checkpoint = commands.add_parser("checkpoint").add_subparsers(dest="operation", required=True)
    create = checkpoint.add_parser("create")
    create.add_argument("--repo", required=True)
    create.add_argument("--output", required=True)
    create.add_argument("--include-ignored", action="append", default=[])
    checkpoint.add_parser("verify").add_argument("path")
    from .delivery_cli import add_commands, dispatch
    add_commands(commands)
    from . import extensions_cli
    extensions_cli.add_commands(commands)
    args = parser.parse_args(argv)
    try:
        if sys.platform not in ("linux", "darwin"):
            raise DeliveryError("repository-delivery is certified only on Linux/macOS; private POSIX preservation is required. Existing AOK commands remain available.")
        if args.command == "inspect":
            result = inspect(args.repo)
        elif args.command == "compare":
            result = compare(args.repo, args.baseline)
        elif args.command in extensions_cli.COMMANDS:
            result = extensions_cli.dispatch(args)
        elif args.command != "checkpoint":
            result = dispatch(args)
        elif args.operation == "create":
            result = create_checkpoint(args.repo, args.output, args.include_ignored)
        else:
            result = verify_checkpoint(args.path)
        print(json.dumps(result, ensure_ascii=True, sort_keys=True))
        return 1 if result.get("passed") is False or result.get("ready") is False else 0
    except (DeliveryError, OSError, ValueError, KeyError, TypeError) as exc:
        print(json.dumps({"error": str(exc)}), file=sys.stderr)
        return 1

if __name__ == "__main__":
    raise SystemExit(main())
