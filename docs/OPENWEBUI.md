# OpenWebUI configuration

This guide assumes OpenRouter ImageJem is healthy inside the same network namespace as OpenWebUI and listening at `127.0.0.1:8787`.

## Generation settings

Open **Admin Panel → Settings → Images** and configure **Create Image**:

| Setting | Value |
|---|---|
| Image Generation | On |
| Image Generation Engine | `Default (Open AI)` |
| API Base URL | `http://127.0.0.1:8787/v1` |
| API Key | Raw OpenRouter key, without `Bearer` |
| API Version | Blank |
| Model | Full OpenRouter image-model slug |
| Image Size | Supported `WIDTHxHEIGHT` value or blank |

A conservative Additional Parameters example is:

```json
{
  "quality": "high",
  "output_format": "png"
}
```

Provider routing can be passed through generation requests when OpenRouter and the selected endpoint support it:

```json
{
  "provider": {
    "sort": "price"
  }
}
```

For ratio and resolution controls, leave OpenWebUI's Image Size blank and use:

```json
{
  "resolution": "2K",
  "aspect_ratio": "16:9",
  "output_format": "png"
}
```

Avoid sending contradictory `size`, `resolution` and `aspect_ratio` combinations.

## Edit settings

Configure **Edit Image** separately:

| Setting | Value |
|---|---|
| Image Edit | On |
| Image Edit Engine | `Default (Open AI)` |
| API Base URL | `http://127.0.0.1:8787/v1` |
| API Key | The same raw OpenRouter key |
| API Version | Blank |
| Model | A model accepting image input and producing image output |

OpenWebUI stores generation and edit settings independently. Entering the key in the generation section does not populate the edit section.

## Selecting a model

Use OpenRouter's image-model catalogue or its image-model API. For editing, check that:

- `architecture.input_modalities` includes `image`
- `architecture.output_modalities` includes `image`
- `supported_parameters` or the selected provider endpoint includes `input_references`

Capabilities differ between model providers. A model slug alone does not guarantee that every routed endpoint accepts the same image controls.

## Persistence warning

When OpenWebUI is started with:

```yaml
ENABLE_PERSISTENT_CONFIG: "False"
```

settings entered in the Admin UI can be replaced after restart. ImageJem remains keyless, but the base URL, model and OpenRouter key may disappear from OpenWebUI.

To keep these values solely in OpenWebUI's database, allow persistent UI configuration. Otherwise configure them through OpenWebUI's own environment or secret mechanism.
