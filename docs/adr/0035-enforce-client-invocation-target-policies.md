# ADR 0035: Enforce Client invocation target policies

- Status: Accepted
- Date: 2026-08-13
- Issue: [#222](https://github.com/imprun/windforce-core/issues/222)

## Context

A workspace-scoped Client authenticated by a `wfk_` token currently receives the standard Client scopes but no persisted App or Action target policy. The execution principal treats an empty target list as allow-all, so every Client can invoke and discover every App and Action in its workspace. Client-specific InputConfig depends on preserving the Client identity and therefore cannot be replaced by a Service Principal without changing execution input semantics.

Self-hosters need least-privilege machine clients without depending on a hosted tenant, billing, entitlement, or gateway product. Core must remain the final execution admission boundary while allowing a downstream control plane to reconcile its product grants into the neutral Client policy.

## Decision

### Client-owned policy

Each Client owns one versioned invocation policy:

```text
ClientInvocationPolicy
  mode: all | restricted
  allowed_targets: [app, app/action, ...]
  revision: non-negative integer
```

`all` permits every App and Action in the Client's workspace and requires an empty target list. `restricted` permits only normalized exact `app` and `app/action` entries; an App entry permits all Actions in that App, and an empty restricted list denies all targets. Target syntax is validated but target existence is not, so an operator may stage policy before publishing a Release.

The policy revision is independent from Client name updates and token rotation or revocation. The policy remains owned by the Client, so future support for multiple credentials per Client can share one execution target policy without copying it onto each credential. Existing Clients and newly created Core-only Clients default to explicit `all` at revision zero. Existing Client IDs, tokens, InputConfig, Runs, and audit history remain unchanged.

### Mutation contract

Invocation policy changes use a dedicated subresource:

```http
PUT /api/w/{workspace}/clients/{client_id}/invocation-policy
Content-Type: application/json

{
  "operation_id": "op-...",
  "expected_revision": 0,
  "mode": "restricted",
  "allowed_targets": ["orders", "reports/export"]
}
```

The request requires a valid operation ID, a non-negative expected revision, an explicit mode, and a normalized policy. The resulting policy, monotonic revision, latest operation replay metadata, Client audit record, actor, and update timestamp commit atomically. An exact retry of the latest operation returns the same revision without another audit record. Reusing the operation ID with a different fingerprint or using a stale revision with a different operation returns conflict.

Client creation accepts an optional initial `invocation_policy` without revision or operation metadata. The Client, issued token hash, initial policy at revision zero, and creation audit record commit in one store transaction. Omission preserves the existing `all` default for backward-compatible Core-only callers. Hosted control planes and app-caller provisioning automation must send the intended policy during creation rather than create `all` and narrow it afterward. Name updates preserve the current policy. Policy-aware Client responses always return an explicit normalized policy and revision.

### Admission and idempotency ordering

Core authenticates the current Client token and validates workspace and `runs:create` scope before processing a request. If the request has a principal-scoped idempotency key, Core resolves the deterministic Run and validates the exact request fingerprint before applying the current target policy. A matching replay returns the already admitted Run and creates no new Job even when policy was reduced after the original admission. A mismatched replay remains a conflict.

Every new admission, including a new idempotency key, applies the current Client policy before App lookup, InputConfig resolution, schema validation, or queue mutation. Policy changes do not retroactively cancel or alter admitted Runs.

### Discovery projection

App contract discovery applies the same target policy. App-level permission returns the App and all Actions. Action-level permissions return the App shell with only the permitted Actions. A policy that leaves no published Action visible returns forbidden. Hidden Actions do not expose input or output schemas, runtime access, timeout, placement, or other Action metadata. Future Client-visible App lists and generated contracts must use the same projection.

Operators retain unrestricted access. Existing Service Principal target semantics remain unchanged by this decision.

### Ownership boundary

Core owns Client identity, `wfk_` authentication, Client-specific InputConfig resolution, policy persistence, discovery projection, and final execution enforcement. A hosted control plane may own Tenant or product entitlement and reconcile a restricted policy, but tenant state, commercial entitlement, sync status, gateway credential exposure, NetworkPolicy, and short-lived delegated credentials do not become Core concepts.

`Client` remains the product-neutral API, database, and provisioning name. It is not a synonym for a hosted Customer or Tenant. The Web UI presents this area as **App access** and presents each Client as an **app caller**: a machine, person, partner, downstream customer, or internal integration that calls workspace Apps. A hosted Customer or Tenant does not automatically map one-to-one to a Core Client. Client-specific InputConfig remains part of the neutral Client contract, but the Web UI edits it only from each App's Input Settings tab, where the active release and Action schemas provide the required context; app-caller detail remains focused on identity, credentials, target access, and audit.

Core-only installations may create, rotate, revoke, and delete app-caller credentials and manage app access directly in the Core UI. Embedded UI mode changes host presentation and return navigation only; it does not transfer Client ownership, add a Tenant concept, or change Core enforcement. Until a separate product-neutral host-capability contract exists, the embedded Core UI continues to expose Core-local Client management. A managed host that requires exclusive credential custody must enforce that product policy in its own control plane and embedding boundary while using the neutral Control API to provision the intended Core policy atomically.

## Consequences

- Core-only installations keep the existing allow-all creation experience and can opt into least privilege per Client.
- Core UI terminology distinguishes app callers and app access from hosted Customers, Tenants, products, and commercial entitlements.
- Hosted products can maintain their own grant model while Core independently prevents a broader downstream invocation.
- A hosted control plane must fail closed at its own gateway when desired policy synchronization is unknown or failed; Core continues to enforce the last policy committed successfully.
- Local JSON and PostgreSQL stores must preserve identical policy, OCC, replay, audit, and migration behavior.
- Control OpenAPI, provisioning import and export, the embedded administration UI, and authorization tests must expose the explicit contract.
- An exact idempotency replay is a read of an existing admission, not authorization for new work.
