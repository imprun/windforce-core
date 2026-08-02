#!/usr/bin/env python3
"""Verify that generated Web UI files match the committed Go embed assets."""

from __future__ import annotations

import argparse
from pathlib import Path


TEXT_SUFFIXES = {
    ".css",
    ".html",
    ".js",
    ".json",
    ".map",
    ".svg",
    ".txt",
}


def normalized_content(path: Path) -> bytes:
    content = path.read_bytes()
    if path.suffix.lower() not in TEXT_SUFFIXES:
        return content
    text = content.decode("utf-8").replace("\r\n", "\n").replace("\r", "\n")
    normalized = "\n".join(line.rstrip(" \t") for line in text.split("\n"))
    return normalized.rstrip("\n").encode("utf-8")


def files_by_relative_path(root: Path) -> dict[str, Path]:
    return {
        path.relative_to(root).as_posix(): path
        for path in root.rglob("*")
        if path.is_file()
    }


def compare_trees(generated_root: Path, embedded_root: Path) -> list[str]:
    generated = files_by_relative_path(generated_root)
    embedded = files_by_relative_path(embedded_root)
    differences: list[str] = []

    for relative_path in sorted(generated.keys() - embedded.keys()):
        differences.append(f"missing from embedded assets: {relative_path}")
    for relative_path in sorted(embedded.keys() - generated.keys()):
        differences.append(f"stale embedded asset: {relative_path}")
    for relative_path in sorted(generated.keys() & embedded.keys()):
        if normalized_content(generated[relative_path]) != normalized_content(
            embedded[relative_path]
        ):
            differences.append(f"content differs: {relative_path}")

    return differences


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("generated_root", type=Path)
    parser.add_argument("embedded_root", type=Path)
    args = parser.parse_args()

    differences = compare_trees(args.generated_root, args.embedded_root)
    if differences:
        print("Web UI source and committed Go embed assets are out of sync:")
        for difference in differences:
            print(f"- {difference}")
        print("Run `make web-embed`, review the generated assets, and commit them.")
        return 1

    print("Web UI source and committed Go embed assets match.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
