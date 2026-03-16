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
```

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

| Status | Description |
|--------|-------------|
| 201 | Document accepted. Body: `{"task_id": "<uuid>"}` |
| 400 | Validation error (missing fields, SHA256 mismatch) |
| 409 | Duplicate document (SHA256 already exists) |
| 502 | Paperless NGX API error |

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
