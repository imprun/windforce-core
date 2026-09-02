# ADR 0058: Rotate local-state encryption keys explicitly

## Status

Accepted

## Context

Core encrypts Run and Job input and output, InputConfig, HumanTask private values, Trigger secret configuration, and Webhook endpoint material with a workspace data-encryption key. New workspaces have a random DEK wrapped by a KEK derived from `SECRET_KEY`, while older local snapshots can have no `workspaceKeys` entry and derive their record key directly from the instance secret. `SECRET_KEY_PREVIOUS` lets a wrapped DEK try an additional KEK, but it does not rewrap the DEK, remove dependence on an exposed legacy derivation, or prove that every encrypted record is readable by the target key.

Automatic startup migration is unsafe because it combines ordinary process boot with irreversible cryptographic maintenance and offers no dry-run, drain gate, or explicit operator approval. A direct flag carrying the raw key would also expose it through process listings and shell history. PostgreSQL and local JSON need different locking and transaction implementations; partial cross-backend support must not imply parity.

## Decision

Core provides `windforce-core rotate-secret-key` as an explicit local-state operator command. It accepts only the names of environment variables containing the target current secret and source previous secret. It defaults to a dry-run and requires `--apply` before writing. Its output contains aggregate counts and blocker categories only; it never includes a raw key, wrapped key, ciphertext, workspace identifier, record identifier, or digest.

The command is an offline maintenance operation. Every process that can access the local state file must be stopped before dry-run and apply; the ordinary writer lock is not a process-lifetime drain mechanism. Once offline, the command takes the same cross-process local-state lock used by writers, loads one snapshot, completes the full transformation and current-key-only verification in memory, and replaces the state file atomically only after every check succeeds. A failure before replacement leaves the original file unchanged. Each changed workspace receives a count-free `secret_key_rotated` audit entry.

For a version-1 wrapped workspace DEK, the command unwraps with the target or source KEK and rewraps the same DEK with the target KEK. Record ciphertext already using that DEK is unchanged. Mixed legacy ciphertext still using an instance-secret-derived key is migrated to the workspace DEK. For a workspace with no key or a version-0 key, the command creates a fresh random DEK, decrypts supported legacy envelopes with the available legacy candidates, re-encrypts them with that DEK, and stores the DEK wrapped by the target KEK. Rewrapping a legacy derived key is forbidden because knowledge of the old instance secret would continue to recover it.

The supported local legacy record families are Run input/output/result output, Job input, InputConfig, HumanTask private context and decision, Trigger secret configuration, and Webhook endpoint and signing secret. A legacy workspace containing a secret Variable is a hard blocker until its bound-ciphertext and runtime-candidate formats have a dedicated migration. The command also refuses queued or running Jobs, pending HumanTasks, unexpired keyed rate buckets, active Webhook deliveries, and active Trigger completion deliveries. These gates prevent state that depends on the old DEK from being created or consumed during replacement.

When a State Store key provider has no workspace key row, encrypted JSON reads try the derived current key and then the derived previous key. An existing workspace key row remains authoritative and cannot be bypassed by that fallback.

The safe live sequence is explicit: configure the runtime with both source and target secrets, fence admission and asynchronous delivery, stop every process that can access the local file normally, and run the dry-run and apply command offline. Start the runtime with the target secret current and the source secret previous, verify existing reads and a new encrypted write, then stop it briefly once more, remove the source secret, and restart. The additional configured key is a rotation candidate; its environment-variable name does not change the cryptographic ordering passed to the command.

PostgreSQL is not supported by this command. It is rejected before state access and requires a separate transactional implementation with equivalent record coverage and gates.

## Consequences

- Local self-hosters can remove dependence on an old instance secret without decrypting or copying values outside Core.
- Modern records already using the workspace DEK avoid ciphertext churn; mixed legacy records are rewritten once.
- Legacy local snapshots gain a random wrapped DEK and current-key-only verification in one atomic operation.
- Dry-run, blocker counts, idempotent retry, and explicit apply separate diagnosis from mutation.
- Secret Variables and PostgreSQL remain deliberate hard boundaries instead of partially migrated data.
- Operators must coordinate admission drain, complete runtime shutdown, and the two-key staging window; rotation is not an automatic startup side effect.
