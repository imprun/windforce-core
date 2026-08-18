import re
import unittest
from pathlib import Path


REPOSITORY_ROOT = Path(__file__).resolve().parents[1]
ADR_DIRECTORY = REPOSITORY_ROOT / "docs" / "adr"
ADR_FILENAME = re.compile(r"^(?P<number>\d{4})-[a-z0-9][a-z0-9-]*\.md$")
CANONICAL_HEADING = re.compile(r"^# ADR (?P<number>\d{4}): .+$")
REDIRECT_LINE = re.compile(
    r"^Redirect: \[ADR (?P<number>\d{4})(?:: [^\]]+)?\]\((?P<target>[^)]+)\)$",
    re.MULTILINE,
)


class ADRNumbersTest(unittest.TestCase):
    def adr_files(self) -> list[Path]:
        return sorted(ADR_DIRECTORY.glob("*.md"))

    def canonical_heading(self, path: Path) -> re.Match[str] | None:
        first_line = path.read_text(encoding="utf-8").splitlines()[0]
        return CANONICAL_HEADING.fullmatch(first_line)

    def test_canonical_numbers_are_unique_and_match_filenames(self) -> None:
        canonical_paths: dict[str, Path] = {}

        for path in self.adr_files():
            filename = ADR_FILENAME.fullmatch(path.name)
            self.assertIsNotNone(filename, f"ADR filename is not canonical: {path.name}")

            heading = self.canonical_heading(path)
            if heading is None:
                continue

            number = heading.group("number")
            self.assertEqual(
                filename.group("number"),
                number,
                f"ADR filename and heading disagree: {path.name}",
            )
            self.assertNotIn(
                number,
                canonical_paths,
                f"ADR {number} is canonical in both {canonical_paths.get(number)} and {path}",
            )
            canonical_paths[number] = path

    def test_noncanonical_files_are_valid_redirects(self) -> None:
        adr_root = ADR_DIRECTORY.resolve()

        for path in self.adr_files():
            if self.canonical_heading(path) is not None:
                continue

            content = path.read_text(encoding="utf-8")
            redirect = REDIRECT_LINE.search(content)
            self.assertIsNotNone(redirect, f"ADR file is neither canonical nor a redirect: {path}")

            target = (path.parent / redirect.group("target")).resolve()
            self.assertEqual(target.parent, adr_root, f"ADR redirect leaves docs/adr: {path}")
            self.assertTrue(target.is_file(), f"ADR redirect target is missing: {path} -> {target}")

            target_heading = self.canonical_heading(target)
            self.assertIsNotNone(target_heading, f"ADR redirect target is not canonical: {target}")
            self.assertEqual(
                redirect.group("number"),
                target_heading.group("number"),
                f"ADR redirect number disagrees with target heading: {path}",
            )


if __name__ == "__main__":
    unittest.main()
