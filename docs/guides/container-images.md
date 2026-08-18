---
title: Container images
description: Pull, verify, or build the standard and OCR Windforce Core runtime images.
---

# Container images

Stable Windforce Core releases publish two public container images:

- `ghcr.io/imprun/windforce-core` is the standard runtime with Bun for Tier 1 TypeScript Apps and Python and Go for compatibility Apps.
- `ghcr.io/imprun/windforce-core-ocr` adds Tesseract with Korean and English OCR data.

Both images support `linux/amd64` and `linux/arm64`. There is no separate Go-only or Windows container image. Windows users run the Linux image through Docker Desktop or another Linux-container environment.

Image contents preserve executable compatibility; they do not make Python and Go equal Tier 1 product paths. See [Product boundary](../concepts/product-boundary.md).

## Choose a tag

Prefer an immutable release tag or digest in production:

```bash
docker pull ghcr.io/imprun/windforce-core:v0.8.1
docker pull ghcr.io/imprun/windforce-core-ocr:v0.8.1
```

Each stable release publishes the exact `vMAJOR.MINOR.PATCH` tag, moving `vMAJOR.MINOR` and `vMAJOR` tags, a source `sha-*` tag, and `latest`. Only stable SemVer tags update `latest`; prereleases do not publish official images. Pin the OCI digest when reproducibility matters.

## Verify a release

Release images include BuildKit SBOM and provenance attestations and a keyless Cosign signature from the repository image workflow. Verify an immutable digest with:

```bash
cosign verify \
  --certificate-identity-regexp='^https://github.com/imprun/windforce-core/.github/workflows/image.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com' \
  ghcr.io/imprun/windforce-core@sha256:<digest>
```

Inspect the OCI index before deployment and require both runtime platforms:

```bash
docker buildx imagetools inspect ghcr.io/imprun/windforce-core:v0.8.1
```

## Build from source

The public images are a convenience, not a requirement. Build the same Dockerfile targets from a trusted source checkout when local policy requires it:

```bash
docker build --target runtime -t windforce-core:local .
docker build --target runtime-ocr -t windforce-core-ocr:local .
```

The image publication workflow runs only on GitHub-hosted runners. Pull requests build and smoke-test both targets on native amd64 and arm64 runners without publishing them. Pull request builds export only a minimal, non-blocking GitHub Actions cache. Stable tags may restore an accessible cache but never export a tag-scoped cache, so repeated releases do not consume cache storage per tag. Stable tags publish per-platform images, create the multi-architecture indexes, verify their platform set, and sign the resulting immutable digests.
