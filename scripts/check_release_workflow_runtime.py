#!/usr/bin/env python3
import pathlib
import re
import sys


EXPECTED_ACTIONS = {
    "actions/upload-artifact": "v7",
    "actions/download-artifact": "v8",
    "softprops/action-gh-release": "v3",
}


def main() -> int:
    workflow = pathlib.Path(".github/workflows/release.yml").read_text(encoding="utf-8")
    failures: list[str] = []

    for action, version in EXPECTED_ACTIONS.items():
        pattern = re.compile(rf"uses:\s*{re.escape(action)}@([^\s]+)")
        match = pattern.search(workflow)
        if match is None:
            failures.append(f"missing {action}@{version}")
            continue

        actual = match.group(1)
        if actual != version:
            failures.append(f"expected {action}@{version}, found @{actual}")

    if failures:
        for failure in failures:
            print(failure, file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
