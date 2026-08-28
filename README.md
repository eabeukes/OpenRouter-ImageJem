# OpenRouter ImageJem

[![CI](https://github.com/eabeukes/OpenRouter-ImageJem/actions/workflows/ci.yml/badge.svg)](https://github.com/eabeukes/OpenRouter-ImageJem/actions/workflows/ci.yml)
[![Publish container](https://github.com/eabeukes/OpenRouter-ImageJem/actions/workflows/publish.yml/badge.svg)](https://github.com/eabeukes/OpenRouter-ImageJem/actions/workflows/publish.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A small, stateless Docker sidecar that lets OpenWebUI use OpenRouter's dedicated Images API for image generation and image editing without patching OpenWebUI.

ImageJem accepts the OpenAI-compatible image calls emitted by OpenWebUI, translates them to OpenRouter's unified Images API and forwards the API key supplied by OpenWebUI without storing a second copy.

> **Status:** test candidate. Unit tests and static builds pass. A live OpenWebUI to OpenRouter generation and edit test is still required before treating the `latest` image as production-proven.

## What it translates

OpenWebUI sends:

- `POST /v1/images/generations` as JSON
- `POST /v1/images/edits` as multipart form data

OpenRouter expects both operations at:

- `POST https://openrouter.ai/api/v1/images` as JSON
- reference images in `input_references`

ImageJem converts the route and body, forwards the incoming `Authorization: Bearer ...` header unchanged and normalises OpenRouter's base64 response for OpenWebUI.

The bridge has no API-key environment variable, secret file, database or persistent volume.

## Fastest installation: Docker only

This route needs only Docker Engine and an existing, running OpenWebUI container. It does not require Go, Git or Docker Compose.

### 1. Find the OpenWebUI container name

```bash
docker ps --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
```

The examples below assume the container is named `open-webui`.

### 2. Pull the pre-built image

```bash
docker pull ghcr.io/eabeukes/openrouter-imagejem:latest
```

The GitHub workflow builds both `linux/amd64` and `linux/arm64`, so the same command works on ordinary x86 servers and ARM64 hosts.

### 3. Run ImageJem inside OpenWebUI's network namespace

```bash
docker rm -f openrouter-imagejem 2>/dev/null || true

docker run -d \
  --name openrouter-imagejem \
  --restart unless-stopped \
  --network container:open-webui \
  --read-only \
  --user 65532:65532 \
  --cap-drop ALL \
  --security-opt no-new-privileges:true \
  --pids-limit 64 \
  --memory 768m \
  --cpus 0.50 \
  -e LISTEN_ADDR=127.0.0.1:8787 \
  -e OPENROUTER_IMAGES_URL=https://openrouter.ai/api/v1/images \
  -e UPSTREAM_TIMEOUT=600s \
  -e MAX_REQUEST_BYTES=67108864 \
  -e MAX_RESPONSE_BYTES=134217728 \
  -e MAX_SCALAR_FIELD_BYTES=1048576 \
  -e MAX_REFERENCE_IMAGES=10 \
  -e MAX_CONCURRENT_REQUESTS=2 \
  ghcr.io/eabeukes/openrouter-imagejem:latest
```

Replace `open-webui` after `--network container:` when your OpenWebUI container has another name.

There is deliberately no `-p` option. ImageJem binds to loopback within the shared network namespace and is not exposed on the Docker host.

### 4. Verify the container without curl or other host tools

```bash
docker inspect --format 'status={{.State.Status}} health={{.State.Health.Status}} network={{.HostConfig.NetworkMode}}' openrouter-imagejem
docker exec openrouter-imagejem /bridge version
docker exec openrouter-imagejem /bridge healthcheck
```

The health status should become `healthy`.

The repository also includes an idempotent installer which performs these checks:

```bash
./deploy/docker-only.sh open-webui
```

The script itself requires only a POSIX shell and Docker.

## Configure OpenWebUI

Open **Admin Panel → Settings → Images**.

### Create image

Set:

- **Image Generation:** On
- **Image Generation Engine:** `Default (Open AI)`
- **API Base URL:** `http://127.0.0.1:8787/v1`
- **API Key:** the raw OpenRouter key, without the word `Bearer`
- **API Version:** blank
- **Model:** a full OpenRouter image-model slug
- **Image Size:** a supported `WIDTHxHEIGHT` value or blank

Example Additional Parameters:

```json
{
  "quality": "high",
  "output_format": "png",
  "provider": {
    "sort": "price"
  }
}
```

For ratio and resolution controls, leave OpenWebUI's Image Size blank and use parameters such as:

```json
{
  "resolution": "2K",
  "aspect_ratio": "16:9",
  "output_format": "png"
}
```

Supported fields and values vary by model. OpenRouter exposes current model capabilities at `GET /api/v1/images/models` and per-provider endpoint details beneath each model record.

### Edit image

Set:

- **Image Edit:** On
- **Image Edit Engine:** `Default (Open AI)`
- **API Base URL:** `http://127.0.0.1:8787/v1`
- **API Key:** enter the same raw OpenRouter key again
- **API Version:** blank
- **Model:** an OpenRouter image model whose input modalities include image

OpenWebUI maintains separate settings for generation and editing, so the key must be entered in both sections. ImageJem does not cache the generation key for use by the edit route.

## Why `127.0.0.1` works

A separate Docker container normally has its own loopback interface. The `--network container:open-webui` option attaches ImageJem directly to OpenWebUI's networking stack. Both processes therefore see the same `127.0.0.1`.

This also means:

- ImageJem cannot publish its own ports in this mode
- recreating the OpenWebUI container creates a new network namespace
- after OpenWebUI is recreated rather than merely restarted, recreate ImageJem with the same `docker run` command

Docker Compose handles that relationship more neatly. See the next section.

## Docker Compose integration

Merge [`compose.fragment.yml`](compose.fragment.yml) into the same Compose project as the existing `open-webui` service, then run:

```bash
docker compose pull openrouter-imagejem
docker compose up -d openrouter-imagejem
```

The service uses:

```yaml
network_mode: "service:open-webui"
```

Do not add `ports:` or `networks:` to that service.

For a source build instead of GHCR, use [`compose.local-build.fragment.yml`](compose.local-build.fragment.yml).

## OpenWebUI configuration persistence

OpenWebUI deployments using:

```yaml
ENABLE_PERSISTENT_CONFIG: "False"
```

may replace settings entered through the Admin UI when OpenWebUI restarts. ImageJem remains keyless, but the OpenWebUI image base URLs and API keys may not persist.

To keep these values solely in OpenWebUI's database, allow OpenWebUI to persist UI configuration. Otherwise configure the corresponding image settings through OpenWebUI's own environment or secret mechanism.

## Updating

For the Docker-only installation:

```bash
docker pull ghcr.io/eabeukes/openrouter-imagejem:latest
./deploy/docker-only.sh open-webui
```

The script replaces only the ImageJem container. It does not modify OpenWebUI.

For repeatable production deployment, use a release tag or immutable digest rather than `latest`:

```bash
docker pull ghcr.io/eabeukes/openrouter-imagejem:1.0.0
```

or:

```bash
docker pull ghcr.io/eabeukes/openrouter-imagejem@sha256:<digest>
```

## GitHub-native build and publication

The repository contains two workflows:

- **CI** runs formatting checks, `go vet`, race-enabled unit tests, a static binary build and a two-architecture container build
- **Publish container** builds and pushes the image to GHCR after tests pass

Published tags are:

- `latest` for the default branch
- `sha-<full commit SHA>` for each published commit
- semantic version tags when a Git tag such as `v1.0.0` is pushed

The publish job also emits BuildKit provenance, an SBOM and a GitHub/Sigstore attestation. It uses the repository's temporary `GITHUB_TOKEN`; no registry password or long-lived package token is required.

After the first run, verify anonymous access with:

```bash
docker logout ghcr.io 2>/dev/null || true
docker pull ghcr.io/eabeukes/openrouter-imagejem:latest
```

If GitHub created the package as private despite the public repository, change the package visibility to **Public** once in the package settings. Anonymous Docker-only installation depends on public package visibility.

## Request flow

```text
OpenWebUI backend
  -> http://127.0.0.1:8787/v1/images/generations
  -> ImageJem translates the request
  -> https://openrouter.ai/api/v1/images
  -> ImageJem normalises data[].b64_json and media_type
  -> OpenWebUI stores and displays the image
```

For edits, multipart `image` and `image[]` fields are converted to ordered base64 data URLs inside OpenRouter `input_references`.

## Endpoints

- `GET /healthz`
- `GET /readyz`
- `GET /version`
- `POST /v1/images/generations`
- `POST /v1/images/edits`
- unversioned aliases for both image routes

## Response normalisation

OpenRouter returns `b64_json` plus `media_type`. OpenWebUI can treat plain base64 as PNG, so ImageJem returns an explicit data URL:

```json
{
  "data": [
    {
      "b64_json": "data:image/webp;base64,...",
      "media_type": "image/webp"
    }
  ]
}
```

This preserves PNG, JPEG, WebP and other image MIME types.

URL-only upstream results are rejected. ImageJem returns base64 content only and never asks OpenWebUI to retrieve a generated image URL using the configured API credential.

## Security properties

- no API-key configuration in ImageJem
- exact incoming Bearer header forwarded only to the configured upstream
- no logging of keys, prompts, image contents or filenames
- loopback listener by default
- no redirect following
- no automatic retries for chargeable image POST requests
- TLS certificate verification with TLS 1.2 minimum
- non-root UID/GID 65532
- read-only distroless runtime with no shell or package manager
- no Linux capabilities and `no-new-privileges`
- bounded request, response, field, image-count and concurrency limits

The OpenRouter key is still stored wherever OpenWebUI stores the value entered in its settings. ImageJem merely avoids introducing another secret store.

## Configuration

| Variable | Default | Purpose |
|---|---:|---|
| `LISTEN_ADDR` | `127.0.0.1:8787` | Loopback listener |
| `OPENROUTER_IMAGES_URL` | `https://openrouter.ai/api/v1/images` | Upstream Images API |
| `UPSTREAM_TIMEOUT` | `600s` | Total upstream request timeout |
| `MAX_REQUEST_BYTES` | `67108864` | Maximum inbound request body |
| `MAX_RESPONSE_BYTES` | `134217728` | Maximum upstream response body |
| `MAX_SCALAR_FIELD_BYTES` | `1048576` | Maximum multipart text field |
| `MAX_REFERENCE_IMAGES` | `10` | Maximum edit reference images |
| `MAX_CONCURRENT_REQUESTS` | `2` | Memory and spend protection |
| `ALLOW_HTTP_UPSTREAM` | `false` | Test-only HTTP upstream escape hatch |
| `ALLOW_NON_LOOPBACK_LISTEN` | `false` | Explicit non-loopback bind escape hatch |

## Known limitations

- OpenWebUI's present OpenAI edit request does not emit a mask, so masked inpainting is not available through this bridge.
- OpenWebUI exposes Additional Parameters for generation but not for its current edit path. Advanced provider-routing values cannot be supplied for edits through the present UI.
- Streaming is removed because OpenWebUI expects a buffered JSON image response.
- ImageJem does not silently replace an incompatible model. Editing requires a model that accepts image input.
- OpenWebUI may reduce a detailed upstream error to a generic UI message. ImageJem logs only request ID, model, status and timing.
- The default HTTP transport deliberately ignores proxy environment variables. Deployments requiring an outbound proxy need a reviewed code change rather than silently forwarding the bearer token through an arbitrary proxy.

## Build and test locally with Docker only

This path is intended for a clean test server that has Docker Engine but does not have Go, Git or Docker Compose. Extract the repository bundle, change into its directory and run:

```bash
./deploy/docker-only-local-build.sh open-webui
```

The script uses the repository Dockerfile to compile the Go binary inside a temporary builder container, creates `local/openrouter-imagejem:dev` and starts it inside the OpenWebUI container's network namespace. It does not pull the unpublished GHCR image.

The equivalent manual commands are:

```bash
docker build \
  --pull \
  --build-arg VERSION=dev \
  --build-arg COMMIT=local \
  --build-arg BUILD_DATE=local \
  -t local/openrouter-imagejem:dev .

PULL_IMAGE=false \
IMAGE=local/openrouter-imagejem:dev \
./deploy/docker-only.sh open-webui
```

Set `open-webui` to the actual OpenWebUI container name. Re-running either route replaces only the ImageJem container.

## Testing

Docker-only smoke test:

```bash
./deploy/smoke-test.sh openrouter-imagejem
```

Source checks when Go is available:

```bash
gofmt -w ./cmd ./internal
go vet ./...
go test -race ./...
CGO_ENABLED=0 go build -trimpath ./cmd/bridge
```

The live acceptance plan is documented in [`docs/TESTING.md`](docs/TESTING.md). The detailed protocol and security contract is in [`SPEC.md`](SPEC.md).

## Licence

MIT. See [`LICENSE`](LICENSE).
