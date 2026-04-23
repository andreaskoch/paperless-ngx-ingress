# Paperless NGX Ingress

A Go REST API that receives documents with rich metadata and forwards them to a [Paperless-ngx](https://docs.paperless-ngx.com/) instance.

## Features

- Accepts documents via JSON with base64-encoded file data
- Validates SHA256 hash integrity
- Rejects duplicate documents (using SHA256 hash tags in Paperless)
- Auto-creates correspondents, document types, storage paths, tags, and custom fields
- Maps metadata to Paperless fields including custom fields

## Configuration

Create a `.env` file (or set environment variables):

```env
PAPERLESS_BASE_URL=https://archive.fe83.de
PAPERLESS_API_TOKEN=your-api-token-here
PORT=8471
PAPERLESS_TASK_TIMEOUT_SECONDS=120
```

| Variable | Default | Description |
|----------|---------|-------------|
| `PAPERLESS_BASE_URL` | *(required)* | Base URL of the Paperless-ngx instance, no trailing slash. |
| `PAPERLESS_API_TOKEN` | *(required)* | API token for the Paperless-ngx user. |
| `PORT` | `8471` | Listen port. |
| `PAPERLESS_TASK_TIMEOUT_SECONDS` | `120` | Max seconds to wait for Paperless to finish processing a document before returning 202. |

## Running

### Locally

```bash
go run .
```

### Docker

```bash
docker build -t paperless-ngx-ingress .
docker run -p 8471:8471 --env-file .env paperless-ngx-ingress
```

### Docker Compose

```yaml
services:
  ingress:
    image: ghcr.io/andreaskoch/paperless-ngx-ingress:latest
    ports:
      - "8471:8471"
    environment:
      - PAPERLESS_BASE_URL=https://archive.fe83.de
      - PAPERLESS_API_TOKEN=your-token
```

## API

### POST /api/documents

Upload a document to Paperless NGX.

```bash
curl -X POST http://localhost:8471/api/documents \
  -H "Content-Type: application/json" \
  -d '{
    "SHA256Hash": "...",
    "Data": "base64-encoded-data",
    "OriginalFilename": "scan.pdf",
    "FileType": "pdf",
    "DocumentDate": "2026-01-01",
    "DocumentType": "Invoice",
    "DocumentLanguageCode": "en",
    "Correspondent": "Test Corp",
    "CorrespondentDetails": "Test Corp, 123 Main St",
    "Recipient": "My Company",
    "RecipientDetails": "My Company, 456 Oak Ave",
    "ShortSummary": "Test invoice",
    "LongSummary": "Detailed description...",
    "ProposedFilename": "2026-01-01 Invoice from Test Corp",
    "Amounts": [{"type": "Total", "Amount": 100, "CurrencyCode": "EUR"}],
    "Tags": ["invoice", "2026"]
  }'
```

**Responses:**

Success responses mirror the cleaned input (normalized whitespace, filled-in
date defaults, deduped tags) minus the base64 `Data` payload, and include a
`TaskID` plus exactly one of `DocumentURL` (when Paperless has finished
processing) or `TaskURL` (when polling timed out).

| Status | Description |
|--------|-------------|
| 201 Created | Document is ready; response contains `DocumentURL`. |
| 202 Accepted | Upload accepted; Paperless still processing. Response contains `TaskURL` instead of `DocumentURL`. |
| 400 Bad Request | Validation error. See error codes below. |
| 405 Method Not Allowed | Wrong HTTP method. |
| 409 Conflict | A document with the same SHA256 hash already exists. |
| 502 Bad Gateway | Paperless-side error (entity creation, upload, task failure, etc.). |

Error responses use a consistent envelope:

```json
{
  "Code": "<stable_machine_code>",
  "Error": "<human readable message>",
  "Details": { ... optional, omitted when empty ... }
}
```

| HTTP | `Code` | `Details` |
|------|--------|-----------|
| 400 | `invalid_json` | — |
| 400 | `invalid_base64` | — |
| 400 | `sha256_mismatch` | `{"Expected":"<computed>","Got":"<supplied>"}` |
| 400 | `validation_failed` | `{"MissingFields":[...]}` (all missing fields, not just the first) |
| 405 | `method_not_allowed` | — |
| 409 | `duplicate_document` | `{"SHA256Hash":"<hex>"}` |
| 502 | `paperless_error` | `{"Stage":"<dedup_check\|correspondent\|document_type\|storage_path\|tag\|custom_field\|upload\|task_poll>","Message":"..."}` |
| 502 | `paperless_task_failed` | `{"TaskID":"<uuid>","Result":"<paperless result text>"}` |

### GET /health

Health check endpoint. Returns `{"status": "ok"}`.

## Field Mapping

| Input Field | Paperless Field |
|-------------|----------------|
| ProposedFilename | title |
| DocumentDate | created |
| Correspondent | correspondent (auto-created) |
| DocumentType | document_type (auto-created) |
| Recipient | storage_path name (auto-created) |
| Tags[] | tags (auto-created) |

### Custom Fields (auto-created)

| Field | Paperless Type |
|-------|---------------|
| DocumentLanguageCode | string |
| ShortSummary | longtext |
| LongSummary | longtext |
| Amounts | longtext (JSON) |
| RecipientDetails | longtext |
| CorrespondentDetails | longtext |

### Storage Path Pattern

```
/{Recipient}/{{ created_year }}/{{ correspondent }}/{{ title }}
```
