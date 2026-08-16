# ADR 0042: Enforce operator execution-limit policies at claim

- Status: Accepted
- Date: 2026-08-16
- Discussion: [#234](https://github.com/imprun/windforce-core/discussions/234)
- Issue: [#235](https://github.com/imprun/windforce-core/issues/235)
- Depends on: [#233](https://github.com/imprun/windforce-core/pull/233)

## Context

Release-owned App and Action execution limits express two facts that only the App author can safely define: which resolved input values identify a shared external resource, and the maximum capacity that the App considers safe. A Core operator also needs to tighten those capacities for one installation or managed Cell without changing App source or publishing another Release. Keeping that control only in a hosted product would leave standalone and self-hosted Core installations without the same execution protection.

An operator control that is applied only when a new Run is admitted does not protect an already admitted backlog. During an external outage, queued Jobs would continue to claim with their older numeric pins. Rewriting those Run pins through bulk re-admission would weaken exact idempotency replay and immutable Run history.

Release changes create another boundary. A numeric ceiling change retains the same key meaning, while a change to scope, kind, input pointers, or fixed rate window changes the policy shape. Forward publication can be rejected while an operator is present to resolve a conflict, but rollback must remain available during an incident. Old-shape queued Jobs can coexist with Jobs admitted after rollback, so one active-Release effective number cannot describe every enforced claim decision.

## Decision

### Ownership and vocabulary

Core stores a workspace-scoped `ExecutionLimitPolicy` as a mutable control object separate from immutable Release history. The Release value is the **release ceiling**, the operator value is the **operator allowance**, and the value used by claim is the **effective limit**.

Core is authoritative for Cell-local applied/enforced state and the final claim decision. A hosted control plane may own desired intent, commercial quota, and cross-Cell policy, but it uses the versioned Core Control API and never reads Core tables or participates in the claim transaction. Commercial plans, billing, tenant entitlement, cross-Cell budgets, domain health decisions, and managed fleet capacity remain outside this contract.

An operator may change only numeric capacity. Policy ID, App or Action scope, concurrency or fixed-window rate kind, ordered input pointers, and rate window remain Release-owned. Allowances are positive; zero is not a pause alias. Disabling a Release safety limit requires a new Release. A future pause contract must define permissions and queued/running behavior explicitly.

### Versioned shape fingerprint

Every keyed Release limit gets one compatibility token in the form `elfp:v1:sha256:<hex>`. The SHA-256 input is a length-framed canonical sequence containing workspace, App, an explicit Action-or-none value, scope, policy ID, kind, the normalized input pointers in declaration order, and `windowSeconds` for rate limits. `maxConcurrent` and `maxAttempts` are excluded. Pointer order is preserved because it is part of key-material construction.

The same fingerprint is stored in the operator policy row and immutable Run/Job pin and is returned in Control API read-back, structured preflight errors, and conflict metadata. Claim compatibility is exact fingerprint equality. Policy revision continues to include numeric Release values for diagnostics but is not the compatibility token or bucket identity.

App-wide concurrency has a Core-known key, workspace plus App, and therefore does not require App-authored input pointers. An absent Release `maxConcurrent` is an infinite author ceiling for this one primitive. A finite operator allowance is permitted through the versioned implicit shape identity `app-concurrency/v1`; a declared Release cap and operator allowance combine by minimum. Keyed policies cannot be created without a matching Release shape. Keyless App-wide rate and Action-wide keyless limits are not part of v1.

### Claim-time operator gate

Release-owned key shape, HMAC digest, and numeric ceiling remain immutable Run/Job pins. `ExecutionLimitPolicy` is a separate mutable gate. For every unclaimed candidate and the next claim of retry or resume, Core uses `min(pinned release ceiling, current compatible operator allowance)`. Exact idempotency replay returns the same Run and Release pin while its future claim remains subject to the current Core-local gate.

Tightening never cancels a running Job. Concurrency claims stop while the matching running count is at or above the effective limit and resume as capacity drains. Fixed-window rate consumption is neither reset nor refunded; when current consumption is already at or above a tightened allowance, claims wait for the next window. Queue-demand projection uses the same policy snapshot and effective limits as claim so blocked work does not request capacity.

Local state serializes policy mutation and claim with its snapshot mutation lock. PostgreSQL claim and policy mutation share a policy-identity transaction advisory lock. Claims acquire every policy and bucket lock through one stable sort order, then batch-read only the candidate's applicable policy rows and perform the authoritative limit checks. A mutation committed before a later claim begins and acquires the shared lock is visible to that claim; overlapping operations linearize at the shared lock. The Release contract permits eight keyed concurrency and eight keyed rate declarations independently at App and Action scope, so one candidate can carry 32 keyed pins plus the implicit App-concurrency shape. PostgreSQL still performs one exact-identity batch policy query, not one query per policy.

### Release transitions

Forward publication or activation performs compatibility preflight before changing the active Release. An incompatible stored policy returns a structured conflict containing safe stable identity, expected and observed fingerprints, revision, compatibility, and resolution guidance. The previous active Release and policy remain unchanged.

Rollback is never blocked by an incompatible operator policy. Policies are not deleted or rewritten. `dormant` is a derived view: a policy is dormant relative to the current active Release when their fingerprints do not match. The policy continues to apply to queued Jobs whose immutable pins carry its fingerprint. Jobs admitted from the rollback Release with another fingerprint use their Release ceiling. When a compatible shape becomes active again, the policy automatically reappears as active. A Release ceiling-only numeric change retains the fingerprint and uses the minimum without becoming dormant; lowering a ceiling below a stored allowance does not reject activation or silently rewrite the allowance.

### Control API, observation, and audit

Policy mutation accepts `operationId`, `expectedRevision`, actor, reason, target identity, fingerprint, and positive allowance. Repeating the same operation ID and payload returns the original result and revision without another audit entry. Reusing an operation ID with a different request fingerprint is a conflict. Explicit deletion is an audited mutation that returns enforcement to the Release ceiling; omission from a desired list is not deletion.

The API distinguishes desired, observed, and enforced state. Desired belongs to an external reconciler when present. Observed reports Core policy and Release revisions, compatibility, fingerprint, and active-Release effective value. Enforced cannot be one scalar during transitions, so read-back includes fingerprint-specific queued/running residual counts, their actual effective limit, and an over-allowance drain indicator. A hosted console marks a change applied only after read-back confirms convergence and must not claim that unreachable Core backlog is fail-closed.

Provisioning export includes stable policy identity, allowance, fingerprint, revision, and safe audit provenance. Import performs capability and complete compatibility preflight before applying the policy-resource batch atomically. This does not claim a new transaction spanning unrelated provisioning backends such as Git sources and database connections. Unsupported API/schema capability is distinct from an empty policy collection or an incompatible Release shape.

Raw input key components never enter policy rows, pins, audit, logs, metrics, diagnostics, or errors. Only stable policy identity, versioned fingerprint, HMAC key digest, revisions, residual counts, and numeric limits are observable to authorized callers.

## Consequences

- Standalone, self-hosted, and hosted Cells use one neutral Core execution-policy contract.
- Operators can tighten a live backlog without mutating Run history or involving an external claim service.
- Claim is intentionally a function of an immutable Release pin and a mutable Core-local gate; the pin alone no longer describes all future claim decisions.
- Rollback remains available, but active-Release and residual old-shape enforcement must be shown separately.
- Local snapshots gain policy and audit collections; PostgreSQL gains dedicated policy, operation, and audit storage plus claim-path lookup and locking.
- Claim hot-path SQL cost is one exact-identity batch lookup. The agreed 16-keyed-pin benchmark remains the merge gate and must stay within 15 percent `benchstat` time/op regression of the #233 foundation on the same fixture; the 33-shape contractual worst case is also covered so the benchmark is not mistaken for a semantic cap.
- Embedded UI, headless Control API, and downstream reconcilers share the same read-back and mutation semantics.
