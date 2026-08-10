---
title: 컨테이너 이미지
description: Windforce Core 표준·OCR 런타임 이미지를 가져오고 검증하거나 소스에서 빌드합니다.
---

# 컨테이너 이미지

Windforce Core 안정 릴리스는 두 개의 공개 컨테이너 이미지를 제공합니다.

- `ghcr.io/imprun/windforce-core`는 Python, Bun, Go가 포함된 표준 런타임입니다.
- `ghcr.io/imprun/windforce-core-ocr`는 표준 런타임에 한국어·영어 Tesseract OCR 데이터를 추가합니다.

두 이미지 모두 `linux/amd64`와 `linux/arm64`를 지원합니다. Go 전용 이미지와 Windows 컨테이너 이미지는 별도로 제공하지 않습니다. Windows 사용자는 Docker Desktop 등 Linux 컨테이너 환경에서 `linux/amd64` 이미지를 실행합니다.

## 태그 선택

운영 환경에서는 변경되지 않는 정확한 릴리스 태그나 digest를 사용합니다.

```bash
docker pull ghcr.io/imprun/windforce-core:v0.8.1
docker pull ghcr.io/imprun/windforce-core-ocr:v0.8.1
```

안정 릴리스는 정확한 `vMAJOR.MINOR.PATCH`, 이동하는 `vMAJOR.MINOR`·`vMAJOR`, 소스 `sha-*`, `latest` 태그를 게시합니다. `latest`는 안정 SemVer 릴리스에서만 갱신되며 prerelease는 공식 이미지를 게시하지 않습니다. 재현성이 중요하면 OCI digest까지 고정합니다.

## 릴리스 검증

릴리스 이미지에는 BuildKit SBOM·provenance attestation과 저장소 이미지 워크플로의 keyless Cosign 서명이 포함됩니다. 다음과 같이 immutable digest를 검증합니다.

```bash
cosign verify \
  --certificate-identity-regexp='^https://github.com/imprun/windforce-core/.github/workflows/image.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com' \
  ghcr.io/imprun/windforce-core@sha256:<digest>
```

배포 전 OCI index를 확인하고 두 실행 플랫폼이 모두 있는지 검증합니다.

```bash
docker buildx imagetools inspect ghcr.io/imprun/windforce-core:v0.8.1
```

## 소스에서 빌드

공개 이미지는 편의를 위한 배포물이며 필수 경로가 아닙니다. 내부 정책상 직접 빌드해야 한다면 신뢰하는 소스 checkout에서 같은 Dockerfile target을 빌드합니다.

```bash
docker build --target runtime -t windforce-core:local .
docker build --target runtime-ocr -t windforce-core-ocr:local .
```

이미지 게시 워크플로는 GitHub-hosted runner만 사용합니다. Pull Request에서는 네이티브 amd64·arm64 runner가 두 target을 빌드하고 실행 검증하지만 게시하지 않습니다. 안정 태그에서는 플랫폼별 이미지를 게시하고 multi-architecture index를 만든 뒤 플랫폼 구성과 immutable digest 서명을 검증합니다.
