# Contributing

OpenRouter ImageJem is intentionally small. Changes should preserve its narrow role as a stateless protocol bridge between OpenWebUI and OpenRouter's dedicated Images API.

## Development checks

Go 1.23 or later is sufficient for the source module. The container and GitHub workflows use the pinned Go version in the Dockerfile and workflow files.

```bash
gofmt -w ./cmd ./internal
go vet ./...
go test -race ./...
CGO_ENABLED=0 go build -mod=readonly -trimpath ./cmd/bridge
```

A local container build requires Docker only:

```bash
docker build -t local/openrouter-imagejem:dev .
```

## Change expectations

- Add tests for every request or response translation change.
- Do not introduce API-key configuration, persistence or logging.
- Do not log prompts, image bytes, filenames or authorisation headers.
- Do not follow redirects or retry image-generation POST requests automatically.
- Keep the default listener on loopback.
- Preserve useful OpenRouter error bodies and status codes where safe.
- Document any change to the OpenWebUI settings contract.

## Pull requests

A pull request should state:

- the OpenWebUI request shape being handled
- the OpenRouter request or response shape expected
- the tests added or updated
- any security or compatibility impact
- whether a live, chargeable OpenRouter test was performed

Do not include real API keys, prompts containing private information or generated customer images in issues, tests or logs.
