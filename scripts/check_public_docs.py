#!/usr/bin/env python3
import pathlib
import re
import sys


ROOTS = [
    pathlib.Path("README.md"),
    pathlib.Path("CHANGELOG.md"),
    pathlib.Path("docs"),
]

BLOCKED_FILENAMES = [
    re.compile(r"(^|[-_/])p\d+[-_]", re.IGNORECASE),
    re.compile(r"smart[-_]examples", re.IGNORECASE),
    re.compile(r"design[-_]partner", re.IGNORECASE),
    re.compile(r"community[-_]preview", re.IGNORECASE),
    re.compile(r"superpowers", re.IGNORECASE),
    re.compile(r"scorecard|runbook", re.IGNORECASE),
]

BLOCKED_TEXT = [
    re.compile(r"\bAI agent\b", re.IGNORECASE),
    re.compile(r"\bagent artifact\b", re.IGNORECASE),
    re.compile(r"\bsuperpowers\b", re.IGNORECASE),
    re.compile(r"\bdesign[- ]partner\b", re.IGNORECASE),
    re.compile(r"\bcommunity preview\b", re.IGNORECASE),
    re.compile(r"\bwedge validation\b", re.IGNORECASE),
    re.compile(r"\bscorecard\b", re.IGNORECASE),
    re.compile(r"\brunbook\b", re.IGNORECASE),
    re.compile(r"\bevidence pack\b", re.IGNORECASE),
    re.compile(r"\bpainful job\b", re.IGNORECASE),
    re.compile(r"\btarget buyer\b", re.IGNORECASE),
    re.compile(r"^## (Goal|Scope|First Targets|Residual Follow-Ups|Guardrails|Ownership|New Internal IR)\b", re.IGNORECASE | re.MULTILINE),
]


def iter_public_files() -> list[pathlib.Path]:
    files: list[pathlib.Path] = []
    for root in ROOTS:
        if root.is_file():
            files.append(root)
        elif root.is_dir():
            files.extend(path for path in root.rglob("*") if path.is_file())
    return sorted(files)


def main() -> int:
    failures: list[str] = []

    for path in iter_public_files():
        normalized = path.as_posix()
        for pattern in BLOCKED_FILENAMES:
            if pattern.search(normalized):
                failures.append(f"{normalized}: blocked public-docs filename")

        if path.suffix.lower() not in {".md", ".php", ".json"}:
            continue

        text = path.read_text(encoding="utf-8")
        for pattern in BLOCKED_TEXT:
            if pattern.search(text):
                failures.append(f"{normalized}: blocked internal-docs language matched {pattern.pattern!r}")

    if failures:
        print("Public docs contain internal planning or agent-facing artifacts:", file=sys.stderr)
        for failure in failures:
            print(f"- {failure}", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
