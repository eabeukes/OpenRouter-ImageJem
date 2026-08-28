# Troubleshooting

## OpenWebUI says it cannot connect

Check the bridge container and network mode:

```bash
docker ps --filter name=openrouter-imagejem
docker inspect --format 'status={{.State.Status}} health={{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}} network={{.HostConfig.NetworkMode}}' openrouter-imagejem
docker exec openrouter-imagejem /bridge healthcheck
```

The network mode must reference the running OpenWebUI container. If OpenWebUI was recreated, recreate ImageJem too:

```bash
docker rm -f openrouter-imagejem
# Repeat the documented docker run command.
```

A restart is not the same as a recreation. The latter gives OpenWebUI a new container and network namespace.

## `connection refused` for `127.0.0.1:8787`

Common causes:

- ImageJem is stopped or unhealthy
- ImageJem was started on Docker's ordinary bridge network
- OpenWebUI was recreated after ImageJem started
- `LISTEN_ADDR` was changed to another port

Do not replace the base URL with the Docker host address merely to get around the namespace problem. That exposes a credential-bearing endpoint unnecessarily.

## `unauthorized` or `missing_api_key`

OpenWebUI must send a Bearer header. In the UI, enter the raw OpenRouter key without the word `Bearer`. OpenWebUI adds the scheme itself.

Generation and edit settings have separate API-key fields. Populate both.

## OpenRouter returns a model or parameter error

Check the selected model's current image capabilities. The following vary by model and provider endpoint:

- reference-image support
- accepted image count
- resolution
- aspect ratio
- output format
- quality
- provider-specific options

Remove Additional Parameters and retry with only model and prompt. Then add controls one at a time.

## Generation works but editing fails

The selected model may generate images without accepting image input. Confirm that its input modalities include image and that `input_references` is supported by the routed endpoint.

OpenWebUI's current edit request does not provide all controls exposed by its generation settings. In particular, ImageJem cannot manufacture a mask that OpenWebUI did not send.

## Settings disappear after OpenWebUI restarts

Check whether OpenWebUI is configured with:

```yaml
ENABLE_PERSISTENT_CONFIG: "False"
```

That mode can override values entered through the UI. Change the OpenWebUI persistence policy or manage the image settings through OpenWebUI's own environment or secret configuration.

## Image pull is denied

The repository can be public while the first GHCR package version remains private. After the first successful publish workflow, the package owner must change the container package visibility to Public once in GitHub's package settings.

Then verify anonymous access:

```bash
docker logout ghcr.io 2>/dev/null || true
docker pull ghcr.io/eabeukes/openrouter-imagejem:latest
```

## Inspecting errors safely

Use:

```bash
docker logs --tail 100 openrouter-imagejem
```

ImageJem intentionally logs no bearer token, prompt, filename or image content. Include the bridge request ID, HTTP status and model when reporting a problem. Do not paste OpenWebUI configuration screens containing an API key.
