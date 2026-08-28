# Changelog

All notable changes to OpenRouter ImageJem will be recorded here.

The project uses semantic versioning once the first production-proven release is tagged.

## Unreleased

### Added

- OpenAI-compatible image generation endpoint for OpenWebUI
- Multipart image-edit translation to OpenRouter `input_references`
- Transparent Bearer credential pass-through with no bridge-side key storage
- Base64 response MIME normalisation for OpenWebUI
- Loopback-only Docker sidecar deployment
- Docker-only installation path with no local Go, Git or Compose requirement
- Multi-architecture GitHub Actions builds for `linux/amd64` and `linux/arm64`
- GHCR publishing with SBOM, provenance and build attestation
- Unit, race, formatting, vet and container-build checks

### Security

- Read-only distroless non-root runtime
- Redirect and URL-only response blocking
- Request, response, field, reference-count and concurrency limits
- Deliberately disabled proxy environment discovery and chargeable-request retries
