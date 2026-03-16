# Paperless NGX Ingress API — Design Spec

## Overview

A Go-based REST API service that receives documents with rich metadata via a JSON endpoint and forwards them to a Paperless NGX instance. The service acts as a translation proxy: it validates input, ensures required Paperless entities exist (correspondents, document types, storage paths, tags, custom fields), and uploads the document via the Paperless API.

## API Surface

### `POST /api/documents`

Accepts a JSON body describing a document with metadata and binary data (base64-encoded).

**Request body fields:**

| Field | Type | Required | Description |
|---|---|---|---|
| SHA256Hash | string | yes | Hex-encoded SHA256 hash of the raw document data |
| Data | string | yes | Base64-encoded binary document data |
| OriginalFilename | string | yes | Original filename of the scanned document |
| FileType | string | yes | File extension (e.g. `pdf`) |
| DocumentDate | string | yes | ISO date `YYYY-MM-DD` |
| Year | string | yes | Year extracted from DocumentDate |
| Month | string | yes | Month extracted from DocumentDate |
| Day | string | yes | Day extracted from DocumentDate |
| DocumentType | string | yes | Paperless document type name |
| DocumentLanguageCode | string | yes | ISO language code (e.g. `de`) |
| Correspondent | string | yes | Correspondent name |
| CorrespondentDetails | string | yes | Full correspondent address/details |
| Recipient | string | yes | Recipient name (used as storage path name) |
| RecipientDetails | string | yes | Full recipient address/details |
| ShortSummary | string | yes | One-line summary |
| LongSummary | string | yes | Detailed multi-line summary |
| ProposedFilename | string | yes | Used as the Paperless document title |
| Amounts | array | yes | JSON array of `{type, Amount, CurrencyCode}` objects |
| Tags | array | yes | Array of tag name strings |

**Responses:**

| Status | Meaning |
|---|---|
| 201 Created | Document uploaded successfully. Body contains `{"task_id": "..."}` from Paperless. |
| 400 Bad Request | Validation failure (missing fields, SHA256 mismatch, invalid base64). |
| 409 Conflict | Document with this SHA256Hash already exists in Paperless. |
| 502 Bad Gateway | Paperless NGX API returned an error. |

### `GET /health`

Returns `200 OK` with `{"status": "ok"}`. Used for Docker/k8s health checks.

## Project Structure

```
paperless-ngx-ingress/
├── main.go              # Entry point, HTTP server, handler
├── paperless.go         # Paperless NGX API client
├── models.go            # Request/response structs
├── go.mod / go.sum
├── .env.example
├── Dockerfile
├── .github/workflows/docker-build.yml
└── README.md
```

Single-package monolith. All code in package `main`. No external framework — stdlib `net/http` only.

## Request Processing Flow

When a document is POSTed:

1. **Parse & validate JSON** — Decode request body, check all required fields are present.
2. **Validate SHA256** — Base64-decode `Data`, compute SHA256, compare against `SHA256Hash`. Reject on mismatch (400).
3. **Deduplicate** — Search Paperless NGX for a tag named `sha256:<full_hash>`. If any documents have this tag, return 409 Conflict. No local database needed.
4. **Ensure entities exist** (create if not found, reuse if found):
   - Correspondent (by exact name)
   - Document type (by exact name)
   - Storage path (name = Recipient, path pattern = `/{Recipient}/{{ created_year }}/{{ correspondent }}/{{ title }}`)
   - All tags from the `Tags` array + the `sha256:<hash>` deduplication tag
   - Custom fields (by name and correct data type)
5. **Upload document** — POST multipart form to Paperless `/api/documents/post_document/` with resolved entity IDs and custom field values.

Entity resolution does not persist a cache across requests — each request queries Paperless for the current state to stay consistent.

## Paperless NGX Field Mapping

| Ingress Field | Paperless Field | Notes |
|---|---|---|
| ProposedFilename | title | |
| DocumentDate | created | ISO date string |
| Correspondent | correspondent | Resolved to ID |
| DocumentType | document_type | Resolved to ID |
| Recipient | storage_path | Name of storage path, resolved to ID |
| Tags[] + sha256 tag | tags | Each resolved to ID |

### Storage Path

- **Name:** The `Recipient` value (e.g. "Andreas Koch Holding GmbH")
- **Path pattern:** `/{Recipient}/{{ created_year }}/{{ correspondent }}/{{ title }}`
- Looked up by name. If a storage path with that name exists but has a different path pattern, use the existing one (don't update it).

### Custom Fields

| Field Name | Paperless Data Type | Source |
|---|---|---|
| DocumentLanguageCode | string | `DocumentLanguageCode` |
| ShortSummary | string | `ShortSummary` |
| LongSummary | text | `LongSummary` |
| Amounts | json | `Amounts` array (stored as-is) |
| RecipientDetails | string | `RecipientDetails` |
| CorrespondentDetails | string | `CorrespondentDetails` |

Custom fields are auto-created if they don't exist in Paperless.

## Paperless NGX API Client

### Entity Resolution Pattern

All entity types (correspondents, document_types, storage_paths, tags) follow the same pattern:

1. `GET /api/<entity_type>/?name__iexact=<name>` — search by exact name (case-insensitive)
2. If found, return the existing ID
3. If not found, `POST /api/<entity_type>/` with `{name: "..."}` (plus `path` for storage paths)

### Custom Fields

1. `GET /api/custom_fields/` — list all custom fields
2. Find by name match
3. If not found, `POST /api/custom_fields/` with `{name, data_type}` where data_type is `string`, `text`, or `json`

### Document Upload

`POST /api/documents/post_document/` as multipart form:

- `document` — binary file data (the decoded base64 content)
- `title` — ProposedFilename
- `created` — DocumentDate
- `correspondent` — ID (integer)
- `document_type` — ID (integer)
- `storage_path` — ID (integer)
- `tags` — one form field per tag ID
- `custom_fields` — JSON string: `[{"field": <id>, "value": <value>}, ...]`

### Authentication

All requests include header: `Authorization: Token <token>`

### Error Handling

Non-2xx responses from Paperless are wrapped in structured errors containing status code and response body. The handler maps these to 502 Bad Gateway.

## Configuration

Read from `.env` file and/or environment variables (env vars take precedence):

```
PAPERLESS_BASE_URL=https://archive.fe83.de
PAPERLESS_API_TOKEN=your-token-here
PORT=8471
```

Default port: `8471`.

## Docker

### Dockerfile

Multi-stage build:
- **Build stage:** `golang:1.24-alpine` — compile with `CGO_ENABLED=0` for static binary
- **Runtime stage:** `alpine:3.21` — copy binary, include `ca-certificates` for HTTPS, run as non-root user

### GitHub Actions (`.github/workflows/docker-build.yml`)

- **Triggers:** Push to `main`, tags matching `v*`
- **Registry:** GitHub Container Registry (`ghcr.io`)
- **Tags:** `latest` for main branch, semver for version tags
- **PR builds:** Build only, no push
- Uses `docker/build-push-action` with buildx

## Non-Goals

- No authentication on the ingress endpoint (trusted network only)
- No persistent local storage or database
- No retry logic for Paperless API failures (caller can retry)
- No batch upload endpoint
