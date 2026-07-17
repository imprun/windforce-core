# Contributing to windforce-lite

Thanks for your interest in contributing.

## License

windforce-lite is licensed under the [Apache License, Version 2.0](LICENSE).
By contributing, you agree that your contributions are licensed under the same
license.

## Developer Certificate of Origin

Every commit must be signed off under the
[Developer Certificate of Origin](DCO) (DCO 1.1):

```
git commit -s
```

This appends a `Signed-off-by:` trailer with your name and email, certifying
that you have the right to submit the work under the project license.

## Pull Requests

- Keep changes small and focused; one logical change per pull request.
- Follow the existing commit style: `feat: ...`, `fix: ...`, `docs: ...`.
- Update documentation (`README.md`, `docs/`) when behavior or contracts
  change; record notable design decisions as an ADR under `docs/adr/`.
- Run `make fmt`, `make build`, and `make test` before submitting. Use
  `make web-test` and `make web-typecheck` for web UI changes.

## Versioning

Releases follow [Semantic Versioning](https://semver.org/) and are published
as `v*` git tags with GitHub Releases. While the project is pre-1.0, minor
releases may contain breaking changes; check release notes when upgrading.

## Security Issues

Do not open public issues for security vulnerabilities — see
[SECURITY.md](SECURITY.md).
