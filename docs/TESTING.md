# Test and acceptance plan

The automated test suite does not contact OpenRouter and cannot spend credits. Live acceptance should be performed deliberately with a low-cost model and non-sensitive images.

## 0. Build the test candidate on a Docker-only host

From the extracted repository directory:

```bash
./deploy/docker-only-local-build.sh open-webui
```

This builds the application inside Docker and installs the resulting local image without attempting to pull from GHCR. No Go, Git or Docker Compose installation is required. Replace `open-webui` with the actual OpenWebUI container name.

## 1. Docker-only infrastructure check

```bash
docker inspect --format 'status={{.State.Status}} health={{.State.Health.Status}} network={{.HostConfig.NetworkMode}}' openrouter-imagejem
docker exec openrouter-imagejem /bridge version
docker exec openrouter-imagejem /bridge healthcheck
```

Expected result:

- bridge status is `running`
- health is `healthy`
- network mode is `container:<OpenWebUI container ID or name>`
- healthcheck exits with status zero

## 2. Generation acceptance

In OpenWebUI:

1. Select a current OpenRouter image model.
2. Use `n: 1` and a modest supported output size.
3. Generate an ordinary non-sensitive test image.
4. Confirm the image appears in OpenWebUI.
5. Check ImageJem logs for an HTTP 200 without inspecting or printing the API key.

```bash
docker logs --tail 50 openrouter-imagejem
```

Expected log fields include operation, model, status, upstream status, request ID and duration. Prompts, bearer tokens and image bytes must not appear.

## 3. Edit acceptance

1. Choose a model whose input modalities include image and whose endpoint supports `input_references`.
2. Upload a disposable test image.
3. Request an obvious visual change.
4. Confirm OpenWebUI displays the edited result.
5. Confirm the bridge log reports `operation: edit` and `reference_count: 1`.

## 4. Error-path checks

Perform these without using a real secret in screenshots or issue reports:

- Temporarily use an invalid API key and confirm the upstream 401 is surfaced.
- Select a text-only model and confirm the provider error is preserved.
- Stop outbound Internet access and confirm the bridge returns a gateway error rather than retrying indefinitely.
- Recreate the OpenWebUI container and confirm ImageJem is recreated or restarted against the new network namespace.

## 5. Acceptance record

Record:

- repository commit SHA
- container digest
- OpenWebUI version
- selected OpenRouter model and provider endpoint
- generation result
- edit result
- observed response times
- whether OpenWebUI retained settings after restart

Do not record the API key, full request body or source image.

## 6. Release decision

A release can move from test candidate to production-proven after:

- GitHub CI passes
- a clean host can pull the published image
- one generation request succeeds
- one edit request succeeds
- no key or prompt appears in logs
- OpenWebUI restart behaviour is documented for the target deployment
