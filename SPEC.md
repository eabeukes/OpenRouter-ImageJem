# Specification: OpenRouter ImageJem

Status: test candidate 1.0.0  
Date: 28 August 2026

## 1. Purpose

OpenRouter ImageJem is a stateless protocol bridge that lets OpenWebUI use OpenRouter's dedicated Images API for image generation and image editing.

The deployment must work without modifying OpenWebUI and must use only the image settings visible in the current OpenWebUI administration interface. The OpenRouter API key is entered in OpenWebUI and passed through ImageJem. The bridge has no key setting or key store of its own.

## 2. Goals

- support OpenWebUI text-to-image generation
- support OpenWebUI one-image and multi-image edit requests
- preserve compatible OpenRouter request parameters
- preserve the incoming Bearer credential without duplicating it
- preserve output MIME type for OpenWebUI
- expose no bridge port on the Docker host
- install on a host that has Docker but no Go, Git or Docker Compose
- publish repeatable `linux/amd64` and `linux/arm64` images through GitHub Actions
- bound memory, concurrency, request size, response size and request duration

## 3. Non-goals

- chat-completion proxying
- API-key creation, storage, substitution or rotation
- local model hosting
- image caching
- automatic retries
- OpenWebUI database modification
- browser extensions, OpenWebUI Pipes or Functions
- masked inpainting when OpenWebUI does not send a mask
- inventing provider controls absent from the OpenWebUI request

## 4. External contracts

### 4.1 OpenWebUI generation input

```http
POST /v1/images/generations
Content-Type: application/json
Authorization: Bearer <OpenRouter key>
```

Expected fields include:

- `model`
- `prompt`
- optional `n`
- optional `size`
- optional generation Additional Parameters

### 4.2 OpenWebUI edit input

```http
POST /v1/images/edits
Content-Type: multipart/form-data
Authorization: Bearer <OpenRouter key>
```

Expected fields include:

- `model`
- `prompt`
- one or more `image` or `image[]` file parts
- optional `n`, `size` and `background`

### 4.3 OpenRouter output

Both operations are translated to:

```http
POST https://openrouter.ai/api/v1/images
Content-Type: application/json
Authorization: Bearer <same key>
```

OpenRouter generation requests are forwarded as JSON after local validation and removal of OpenAI-only transport fields.

OpenWebUI edit files are converted in arrival order to:

```json
{
  "input_references": [
    {
      "type": "image_url",
      "image_url": {
        "url": "data:image/png;base64,..."
      }
    }
  ]
}
```

## 5. Translation rules

### 5.1 Common rules

- require a valid Bearer header
- require non-empty string `model` and `prompt`
- permit `n` only as an integer from 1 through 10
- remove `response_format`
- remove `stream`
- remove `size` when its value is `auto`
- preserve other JSON-compatible fields
- do not add a model, provider, key or prompt

### 5.2 Edit rules

- accept file fields named `image` and `image[]`
- preserve reference order
- reject empty or unrecognised image content
- detect and canonicalise image MIME type
- reject unknown file fields
- reject mask fields with an explicit unsupported error
- limit reference count
- convert selected integer-like multipart fields to JSON integers
- parse multipart values beginning with `{` or `[` as JSON when valid

## 6. Response contract

OpenRouter's buffered response contains `data[].b64_json` and may contain `media_type`.

ImageJem returns an OpenAI-compatible JSON object while changing each plain base64 image value to an explicit image data URL:

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

Rules:

- preserve top-level metadata such as `created`, `usage` and model information
- require a non-empty `data` array
- require non-empty `b64_json` for every image
- preserve a valid image base64 data URL
- infer an image MIME type when `media_type` is absent
- remove any `url` member
- reject URL-only image responses
- reject non-image or non-base64 data URLs
- reject malformed JSON and trailing JSON values

## 7. Error contract

### 7.1 Bridge errors

Bridge-generated errors use:

```json
{
  "error": {
    "message": "Human-readable message",
    "type": "bridge_error",
    "code": "stable_code"
  }
}
```

### 7.2 Upstream errors

For OpenRouter non-2xx responses:

- preserve the upstream HTTP status
- preserve the bounded upstream body
- preserve selected request, retry and rate-limit headers
- do not transform a provider error into a success

### 7.3 Redirects and retries

- never follow an upstream redirect
- return 502 for an upstream redirect
- never retry a chargeable image POST automatically

## 8. Credential and privacy requirements

ImageJem must not:

- define an API-key environment variable
- persist a key in memory beyond normal request lifetime
- write keys, prompts, images or filenames to logs
- copy cookies or OpenWebUI identity headers upstream
- forward a Bearer credential to a redirect target
- retrieve an image URL on OpenWebUI's behalf
- honour ambient HTTP proxy environment variables

The bridge may log:

- generated bridge request ID
- operation
- model slug
- local and upstream status
- reference count
- duration
- non-sensitive error code

## 9. Network architecture

ImageJem binds to `127.0.0.1:8787` by default.

Docker CLI deployment uses:

```text
--network container:<OpenWebUI container>
```

Docker Compose deployment uses:

```yaml
network_mode: "service:open-webui"
```

This gives both processes the same loopback interface. The bridge publishes no port and declares no independent Compose network.

The default configuration must reject a non-loopback listener. An explicit escape hatch may permit one for controlled deployments, but it is not part of the recommended configuration.

## 10. Resource controls

Defaults:

| Control | Default |
|---|---:|
| Upstream timeout | 600 seconds |
| Total inbound body | 64 MiB |
| Total upstream body | 128 MiB |
| Multipart scalar field | 1 MiB |
| Reference images | 10 |
| Concurrent requests | 2 |

The Docker deployment additionally sets:

- non-root UID and GID 65532
- read-only root filesystem
- all Linux capabilities dropped
- `no-new-privileges`
- process, memory and CPU limits
- distroless runtime without a shell or package manager

## 11. Operational endpoints

- `GET /healthz`
- `GET /readyz`
- `GET /version`
- `POST /v1/images/generations`
- `POST /images/generations`
- `POST /v1/images/edits`
- `POST /images/edits`

Health endpoints are local process checks and do not contact OpenRouter or require a key.

## 12. Docker-only deployment requirement

A clean target host may have only:

- Docker Engine and its CLI
- a running OpenWebUI container
- an ordinary command shell

The documented installation must therefore be possible with:

1. `docker pull`
2. one `docker run` command
3. `docker inspect` and `docker exec` verification

Go, Git, curl, wget and Docker Compose must not be mandatory for the primary path.

## 13. Build and supply-chain requirements

GitHub Actions must:

- check formatting
- run `go vet`
- run tests with the race detector
- build a static Linux binary
- build OCI images for `linux/amd64` and `linux/arm64`
- publish default-branch and semantic-version tags to GHCR
- emit an SBOM, provenance and build attestation
- use the temporary repository `GITHUB_TOKEN`, not a stored registry password

The first GHCR package version may require the owner to change package visibility to Public once. Repository visibility and container-package visibility are separate settings.

## 14. Configuration

| Variable | Default | Validation |
|---|---|---|
| `LISTEN_ADDR` | `127.0.0.1:8787` | loopback unless explicitly overridden |
| `OPENROUTER_IMAGES_URL` | `https://openrouter.ai/api/v1/images` | absolute HTTPS URL without userinfo or fragment |
| `UPSTREAM_TIMEOUT` | `600s` | positive duration |
| `MAX_REQUEST_BYTES` | `67108864` | at least 1024 |
| `MAX_RESPONSE_BYTES` | `134217728` | at least 1024 |
| `MAX_SCALAR_FIELD_BYTES` | `1048576` | between 1024 and request limit |
| `MAX_REFERENCE_IMAGES` | `10` | 1 to 100 |
| `MAX_CONCURRENT_REQUESTS` | `2` | 1 to 64 |
| `ALLOW_HTTP_UPSTREAM` | `false` | test-only escape hatch |
| `ALLOW_NON_LOOPBACK_LISTEN` | `false` | explicit deployment escape hatch |

## 15. Acceptance criteria

Automated acceptance:

- formatting, vet, race tests and static build pass
- multi-architecture container build passes
- tests cover generation translation, multipart edits, multiple references, credential omission, request limits, invalid counts, upstream errors, redirect blocking, URL blocking, MIME normalisation and operational endpoints

Live acceptance:

- a Docker-only host can pull and start the image
- OpenWebUI can generate one image through OpenRouter
- OpenWebUI can edit one image through a compatible model
- no key, prompt or image content appears in logs
- a recreated OpenWebUI container is handled according to the deployment guide

## 16. Known limitations

- OpenWebUI does not currently send an edit mask through this UI path.
- OpenWebUI exposes generation Additional Parameters more broadly than edit parameters.
- Streaming is removed because OpenWebUI expects buffered JSON image results.
- ImageJem does not choose a compatible model or provider automatically.
- OpenWebUI may replace Admin UI settings after restart when persistent configuration is disabled.
- A package owner may need to make the first GHCR package public manually before anonymous pulls work.
