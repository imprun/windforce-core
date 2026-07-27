# Windforce Invocation SDK for Python

This package lets operators, apps, and external adapters create and observe Windforce Runs through the canonical Invocation API. It does not access the Windforce database or catalog files.

```python
from windforce_invocation import WindforceInvocationClient

client = WindforceInvocationClient(
    "http://windforce-core:8080",
    workspace="default",
    token="...",
)
run = client.create_run(
    app="example",
    action="lookup",
    input={"query": "value"},
    idempotency_key="request-123",
)
run = client.wait(run.run_id, timeout_seconds=60)
result = client.get_result(run.run_id)
```

Authentication derives the caller principal from an operator, workspace, `wfk_` client, or `wfs_` service-principal bearer. Callers cannot assert another principal, inject per-run environment variables, or observe internal Job identifiers. `Idempotency-Key` is scoped to the authenticated principal.
