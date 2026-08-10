# ADR 0030: Pin each Release to one compatible worker execution profile

## Status

Accepted (2026-08-10), implementing [issue #204](https://github.com/imprun/windforce-core/issues/204).

## Context

Prepared execution bundles may contain platform-specific runtime dependencies or compiled output. The previous `prepare-v3` fingerprint included the publisher Core Go version and `/etc/os-release`, then required the Worker to reproduce that exact string. That rejected valid placements such as the same runtime container on different Linux hosts, while still expressing compatibility as an incidental cache fingerprint rather than an explicit Release and scheduling contract.

Windforce Core does not need one Bundle to run across Windows and Linux or across architectures. Deployments can build and place the App correctly for their target platform. Bun is the tier-1/default runtime; Python and Go remain supported. One App Release must not mix Action runtimes because one published Bundle has one preparation and execution target.

## Decision

1. One Release targets one versioned execution profile. The profile contains OS, architecture, launcher runtime, runtime ABI, normalized libc identity, an optional immutable operator profile ID, and a canonical key. TypeScript maps to the Bun launcher. An Action may override its entrypoint but not the App runtime.
2. Release publication detects the target profile, writes it into the immutable Bundle, pins it on the Deployment and compact Job payload, and adds an engine-owned `sys/execution-profile-*` required label. App manifests cannot author the reserved label namespace.
3. Workers detect and register every runtime profile they can execute. Bun and Python profiles are advertised only when their executable identity can be read. Go execution advertises the `static-cgo0` ABI because publication builds `CGO_ENABLED=0` binaries and a Worker does not need a Go compiler.
4. The existing atomic label scheduler performs profile placement. A Worker offers Core-derived profile labels in addition to operator labels. A profile-pinned Job remains queued unless one label matches. Managed worker credentials continue to constrain operator labels; registered profiles determine engine-owned labels.
5. After claim, the Processor compares the structured pinned profile against its detected profiles before launching. A mismatch is an invariant failure and never falls back to Git, source preparation, another runtime, or another Release.
6. Bundle validation checks that the Bundle profile, Deployment profile, and current Worker profile agree. Profile-less historical Bundles remain on exact `prepare-v3` validation. They are not reinterpreted or weakened.
7. The optional `WINDFORCE_EXECUTION_PROFILE_ID` / `--execution-profile-id` identifies an immutable prepared environment. Container operators should use the exact image digest. Without it, native compatibility derives from OS, architecture, runtime, runtime ABI, and libc identity.
8. The Core Go version and complete Linux distribution metadata are not compatibility fields. Native Bun/Python profiles distinguish normalized glibc or musl versions without pinning a distribution name. The Go compiler version can be recorded as build provenance separately but is not required on a Worker for a static Bundle.
9. Remote Workers verify the canonical tree digest directly from tar headers and bytes before promotion. This provides the same integrity decision on Windows, where a post-extraction filesystem hash cannot faithfully reproduce POSIX modes, and on POSIX hosts.

## Rejected alternatives

- **Make every Bundle cross-platform.** This duplicates Bundle variants and adds no value when Windows and Linux workers can be placed independently.
- **Keep exact `prepare-v3` as the new scheduler contract.** It encodes publisher implementation details, does not participate in claim matching, and rejects compatible workers.
- **Let a Worker claim first and discover compatibility while launching.** It consumes a lease and turns a placement problem into a Job failure.
- **Let each Action select a runtime.** A single App Bundle would no longer have one preparation target or one profile.
- **Use operator-authored labels only.** Runtime compatibility would become an unenforced convention and could drift from the actual launcher image.

## Consequences

- Native and container deployments build one Bundle per Release target and arrange workers with matching profiles. Cross-platform Bundle reuse is not promised.
- Existing State Store label matching remains the atomic claim primitive; structured profiles add explicit metadata, registry observability, and defense in depth.
- Worker registry persistence and Worker Plane registration carry execution profiles. Existing static worker-plane tokens retain their trusted compatibility behavior; managed credentials bind claims to registered profiles.
- Legacy Releases remain executable only where their old exact fingerprint matches. Republish them to receive explicit profile scheduling.

## Verification

- Equivalent TypeScript and Bun profile inputs produce the same canonical key; OS, architecture, runtime ABI, or immutable profile ID changes produce an incompatible key.
- Publication pins the same valid profile in the Bundle, Deployment, required label, and Job payload.
- A worker with a different profile cannot claim the Job; a compatible worker can.
- The Processor refuses a mismatched claimed Job before invoking the launcher.
- New profile-pinned Bundles do not compare incidental publisher distribution metadata, while profile-less Bundles retain strict `prepare-v3` comparison.
- Local, standalone, and remote Worker paths preserve the same profile and bundle semantics.
- A tampered remote archive is rejected before promotion on every supported worker OS.
