# Security policy

## Supported versions

Until the first stable release, only the current default branch is supported. After stable tags begin, the latest stable minor release and the current default branch will receive security fixes.

## Reporting a vulnerability

Please do not open a public issue for a vulnerability that could expose credentials, prompts, images or the host running OpenWebUI.

Use GitHub's private vulnerability reporting feature for this repository. Include:

- affected commit or image digest
- request path and minimal reproduction steps
- expected and observed behaviour
- potential credential, data or network impact
- suggested remediation, when known

Do not include a working OpenRouter key or private source image. Redact bearer tokens and image payloads before submitting evidence.

## Security boundaries

ImageJem is designed to:

- accept requests only from loopback by default
- forward the incoming Bearer header only to the configured upstream URL
- avoid all API-key storage
- avoid logging prompts, image data and filenames
- reject upstream redirects
- reject URL-only image responses
- run as a non-root process in a read-only distroless container

The OpenRouter key remains under OpenWebUI's configuration and storage controls. Docker daemon access, OpenWebUI administration, host security and OpenRouter account controls remain outside this project's security boundary.
