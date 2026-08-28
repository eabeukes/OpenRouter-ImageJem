# Architecture

## Request path

```text
OpenWebUI backend
    |
    | OpenAI-compatible request
    | Authorization: Bearer <OpenRouter key>
    v
127.0.0.1:8787 inside OpenWebUI's network namespace
    |
    | OpenRouter ImageJem
    | - validates bounded input
    | - translates generation or edit request
    | - does not persist credentials or images
    v
https://openrouter.ai/api/v1/images
    |
    | JSON response with data[].b64_json
    v
ImageJem MIME normalisation
    |
    v
OpenWebUI image handling and storage
```

## Why a protocol bridge is required

A path rewrite alone is insufficient:

- OpenWebUI generation calls are JSON at `/images/generations`
- OpenWebUI edit calls are multipart form data at `/images/edits`
- OpenRouter's dedicated endpoint accepts both operations as JSON at `/api/v1/images`
- edit files must become ordered base64 data URLs in `input_references`
- OpenRouter reports output MIME information separately from base64 data

## Network placement

ImageJem shares OpenWebUI's Docker network namespace:

- Docker CLI: `--network container:<openwebui-container>`
- Docker Compose: `network_mode: "service:open-webui"`

It binds only to `127.0.0.1:8787`, publishes no host port and is therefore reachable by OpenWebUI through its own loopback interface.

## Credential path

1. The administrator enters an OpenRouter key in OpenWebUI.
2. OpenWebUI emits an `Authorization: Bearer ...` header.
3. ImageJem validates the header shape.
4. ImageJem forwards that header only to the fixed configured upstream.
5. The bridge does not copy the key into an environment variable, file, database, metric or log.

## Failure behaviour

- malformed local requests fail before contacting OpenRouter
- upstream 4xx and 5xx bodies and safe headers are preserved
- redirects are converted to a bridge 502 and never followed
- timeouts become 504 responses
- oversized upstream bodies become 502 responses
- URL-only image results are rejected
- there are no automatic retries for chargeable POST requests

## Resource controls

The server applies limits to:

- total request body
- total upstream response body
- multipart scalar field size
- reference image count
- concurrent requests
- upstream duration

The container adds memory, CPU, process, privilege and filesystem controls.
