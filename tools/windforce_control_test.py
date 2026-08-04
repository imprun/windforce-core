import argparse
import os
import unittest
from unittest.mock import patch

import windforce_control


class WindforceControlEnvironmentTest(unittest.TestCase):
    def test_core_value_wins_over_legacy_alias(self) -> None:
        with patch.dict(
            os.environ,
            {
                "WINDFORCE_CORE_API_URL": "https://core.example.test",
                "WINDFORCE_LITE_API_URL": "https://legacy.example.test",
            },
            clear=True,
        ):
            self.assertEqual(
                windforce_control.env_value("WINDFORCE_CORE_API_URL"),
                "https://core.example.test",
            )

    def test_legacy_alias_is_used_when_core_value_is_absent(self) -> None:
        with patch.dict(
            os.environ,
            {"WINDFORCE_LITE_API_TOKEN": "legacy-token"},
            clear=True,
        ):
            headers = windforce_control.request_headers(
                argparse.Namespace(
                    auth_token_env=windforce_control.DEFAULT_AUTH_TOKEN_ENV,
                    actor="",
                ),
                False,
            )
            self.assertEqual(headers["Authorization"], "Bearer legacy-token")


if __name__ == "__main__":
    unittest.main()
