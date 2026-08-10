---
title: Queue demand observation
description: Read a consistent, selector-aware queue snapshot for external capacity management.
---

# Queue demand observation

Core exposes one instance-admin operation for capacity observers that must not
read Core storage directly:

```http
POST /api/queue-demand-snapshots
Authorization: Bearer <INSTANCE_ADMIN_TOKEN>
Content-Type: application/json

{
  "selectors": [
    {
      "key": "arm64-orders",
      "workspace_id": "production",
      "tags": ["orders"],
      "labels": ["arm64", "container"]
    }
  ]
}
```

`key` is opaque caller context. Core normalizes tag and label sets and echoes
the selector in the response. A selector with no tags keeps the existing Core
worker meaning: it can serve every worker tag. A selector with no labels can
serve only jobs without label requirements.

```json
{
  "store_epoch": "store_7ed28c9dfcf21ed74faeb62f",
  "snapshot_revision": 1842,
  "observed_at": "2026-08-01T01:02:03Z",
  "items": [
    {
      "selector": {
        "key": "arm64-orders",
        "workspace_id": "production",
        "tags": ["orders"],
        "labels": ["arm64", "container"]
      },
      "eligible": 2,
      "queued": 1,
      "expired_reacquirable": 1,
      "claimed": 2,
      "busy_workers": 2,
      "oldest_eligible_at": "2026-08-01T01:00:00Z"
    }
  ]
}
```

Every item in one response uses the same fence and observed time. Consumers
must retain `(store_epoch, snapshot_revision)` together:

- a higher revision in the same epoch is a newer store state;
- the same revision may produce a different lease-expiry classification at a
  later `observed_at`, because eligibility also depends on authoritative time;
- a lower revision in the same epoch is a regression and destructive capacity
  changes must stop; and
- a new epoch starts a new observation window and must not be ordered against
  the old epoch.

The snapshot is observational. It does not reserve or assign jobs. Queue
mutation remains in the existing claim transaction.

## Restore procedure

A restart or failover against the same authoritative store preserves the epoch
and revision. Creating a fresh Local state file or PostgreSQL schema creates a
new epoch. If an operator performs a point-in-time restore that moves the
authoritative store backwards, rotating `store_epoch` is a required restore
step before capacity observation resumes. Keep capacity scale-in and
destructive rollout frozen until consumers have filled a fresh observation
window for the new epoch.

The rationale and precise eligibility definition are recorded in
[ADR 0019](../adr/0019-fenced-queue-demand-snapshots.md).
