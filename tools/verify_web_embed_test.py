from pathlib import Path
from tempfile import TemporaryDirectory
import unittest

from verify_web_embed import compare_trees


class VerifyWebEmbedTest(unittest.TestCase):
    def test_ignores_platform_line_endings_and_trailing_whitespace(self) -> None:
        with TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            generated = root / "generated"
            embedded = root / "embedded"
            generated.mkdir()
            embedded.mkdir()
            (generated / "index.js").write_bytes(b"const current = true;  \n\n")
            (embedded / "index.js").write_bytes(b"const current = true;")

            self.assertEqual(compare_trees(generated, embedded), [])

    def test_reports_missing_stale_and_changed_assets(self) -> None:
        with TemporaryDirectory() as temporary_directory:
            root = Path(temporary_directory)
            generated = root / "generated"
            embedded = root / "embedded"
            generated.mkdir()
            embedded.mkdir()
            (generated / "current.js").write_text("current", encoding="utf-8")
            (generated / "changed.css").write_text("new", encoding="utf-8")
            (embedded / "stale.js").write_text("stale", encoding="utf-8")
            (embedded / "changed.css").write_text("old", encoding="utf-8")

            self.assertEqual(
                compare_trees(generated, embedded),
                [
                    "missing from embedded assets: current.js",
                    "stale embedded asset: stale.js",
                    "content differs: changed.css",
                ],
            )


if __name__ == "__main__":
    unittest.main()
