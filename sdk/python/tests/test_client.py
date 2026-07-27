from __future__ import annotations

from pathlib import Path
import sys
from unittest import TestCase
from unittest.mock import call, patch


sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "src"))

from windforce_invocation import WindforceInvocationClient, WindforceTimeoutError  # noqa: E402


class WindforceInvocationClientTest(TestCase):
    def test_legacy_sdk_package_is_absent(self) -> None:
        legacy_package = Path(__file__).resolve().parents[1] / "src" / "windforce_execution"
        self.assertFalse(legacy_package.exists())

    def test_ready_uses_service_readiness_endpoint(self) -> None:
        client = WindforceInvocationClient("http://windforce")
        with patch.object(client, "_request", return_value={"ready": True}) as request:
            self.assertTrue(client.ready())
        request.assert_called_once_with("GET", "/readyz")

    def test_create_run_uses_canonical_workspace_endpoint_and_header(self) -> None:
        client = WindforceInvocationClient("http://windforce", workspace="team a")
        with patch.object(
            client,
            "_request",
            return_value={"run_id": "run_a", "state": "queued", "app": "echo", "action": "run"},
        ) as request:
            run = client.create_run(
                app="echo",
                action="run",
                input={"message": "hello"},
                idempotency_key="message-1",
            )

        self.assertEqual(run.run_id, "run_a")
        self.assertFalse(hasattr(run, "job_id"))
        request.assert_called_once_with(
            "POST",
            "/api/v1/workspaces/team%20a/runs",
            {"app": "echo", "action": "run", "input": {"message": "hello"}},
            headers={"Idempotency-Key": "message-1"},
        )

    def test_create_run_and_wait_reads_run_id_case_insensitively(self) -> None:
        client = WindforceInvocationClient("http://windforce", workspace="default")
        with patch.object(
            client,
            "_request_with_metadata",
            return_value=({"ok": True}, 200, {"X-Wf-Run-Id": "run_a"}),
        ) as request:
            result = client.create_run_and_wait(
                app="echo",
                action="run",
                input={"message": "hello"},
                timeout_seconds=5,
                correlation_id="request-a",
                idempotency_key="message-1",
            )

        self.assertTrue(result.completed)
        self.assertEqual(result.run_id, "run_a")
        self.assertEqual(result.value, {"ok": True})
        request.assert_called_once_with(
            "POST",
            "/api/v1/workspaces/default/runs/wait?timeout=5s",
            {
                "app": "echo",
                "action": "run",
                "input": {"message": "hello"},
                "correlation_id": "request-a",
            },
            headers={"Idempotency-Key": "message-1"},
            accepted_statuses=(200, 202),
        )

    def test_run_lifecycle_and_app_routes_are_canonical(self) -> None:
        client = WindforceInvocationClient("http://windforce", workspace="team a")
        with patch.object(
            client,
            "_request",
            side_effect=[
                {"run_id": "run/a", "state": "running", "app": "echo", "action": "run"},
                {"answer": 42},
                {"run_id": "run/a", "state": "canceled", "app": "echo", "action": "run"},
                {"app": "echo", "actions": []},
            ],
        ) as request:
            self.assertEqual(client.get_run("run/a").state, "running")
            self.assertEqual(client.get_result("run/a"), {"answer": 42})
            self.assertEqual(client.cancel("run/a", reason="operator request").state, "canceled")
            self.assertEqual(client.describe_app("echo")["app"], "echo")

        self.assertEqual(
            request.call_args_list,
            [
                call("GET", "/api/v1/workspaces/team%20a/runs/run%2Fa"),
                call(
                    "GET",
                    "/api/v1/workspaces/team%20a/runs/run%2Fa/result",
                    accepted_statuses=(200, 202),
                ),
                call(
                    "POST",
                    "/api/v1/workspaces/team%20a/runs/run%2Fa/cancel",
                    {"reason": "operator request"},
                ),
                call(
                    "GET",
                    "/api/v1/workspaces/team%20a/apps/echo",
                ),
            ],
        )

    def test_wait_returns_terminal_run(self) -> None:
        client = WindforceInvocationClient("http://windforce", poll_interval_seconds=0.01)
        with patch.object(
            client,
            "_request",
            side_effect=[
                {"run_id": "run_a", "state": "queued", "app": "echo", "action": "run"},
                {"run_id": "run_a", "state": "succeeded", "app": "echo", "action": "run"},
            ],
        ):
            run = client.wait("run_a", timeout_seconds=1)
        self.assertEqual(run.state, "succeeded")

    def test_wait_reports_last_state_on_timeout(self) -> None:
        client = WindforceInvocationClient("http://windforce")
        with patch.object(
            client,
            "_request",
            return_value={"run_id": "run_a", "state": "running", "app": "echo", "action": "run"},
        ):
            with self.assertRaises(WindforceTimeoutError) as raised:
                client.wait("run_a", timeout_seconds=0)
        self.assertEqual(raised.exception.state, "running")
