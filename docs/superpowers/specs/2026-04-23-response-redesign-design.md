# Response Redesign — Design

## Problem

The current `POST /api/documents` response is minimal: `{"task_id": "<uuid>"}` on success, `{"error": "<message>"}` on failure. It leaks no context about what the server actually ingested and provides no link to the resulting Paperless document. Error bodies are human-readable only — clients have to regex-match messages to branch on specific failure modes.

## Goals

- Success responses mirror the **cleaned** input (post-normalization, post-default-filling) minus the base64 payload, and carry a URL to the Paperless document.
- If the document cannot be resolved within a configurable timeout, return `202 Accepted` with a task URL instead of blocking forever.
- Error responses carry a stable machine code plus structured details for validation failures.
- Field casing in the response matches the request (PascalCase throughout).

## Non-goals

- Backward compatibility. Response shape changes in breaking ways (PascalCase keys, new envelope for errors). The only known caller is the README; it will be updated in the same commit.
- Pagination, streaming, or multi-document responses. Still one request, one response.
- Exposing Paperless-internal IDs (correspondent, document type, tag IDs). These stay implementation details.
- A 504 status code. Poll timeout → 202, consistent with "the upload was accepted, indexing is just pending."

## Success Response

### Body (shared by 201 and 202)

```json
{
  "TaskID": "abc-123-uuid",
  "DocumentURL": "https://archive.fe83.de/documents/42/",
  "SHA256Hash": "<hex>",
  "OriginalFilename": "scan.pdf",
  "FileType": "pdf",
  "DocumentDate": "2026-01-01",
  "Year": "2026",
  "Month": "01",
  "Day": "01",
  "DocumentType": "Invoice",
  "DocumentLanguageCode": "en",
  "Correspondent": "Test Corp",
  "CorrespondentDetails": "...",
  "Recipient": "My Company",
  "RecipientDetails": "...",
  "ShortSummary": "...",
  "LongSummary": "...",
  "ProposedFilename": "2026-01-01 Invoice from Test Corp",
  "Amounts": [...],
  "Tags": ["invoice", "2026"]
}
```

Rules:
- `Data` (base64 payload) is **never** echoed.
- `Tags` is the user-facing, normalized + deduped list. The `sha256:<hash>` dedup tag is **stripped** from the response.
- Normalized/defaulted values are echoed (e.g., `Correspondent` reflects trimmed/collapsed whitespace; `Year/Month/Day/DocumentDate` reflect defaults applied by `FillDateDefaults`).
- `TaskID` is always present — cheap, useful for debugging.
- Resolved Paperless IDs are **not** included.

### Status codes

| HTTP | Meaning | URL field |
|------|---------|-----------|
| 201 Created | Document is ready in Paperless | `DocumentURL` present, `TaskURL` absent |
| 202 Accepted | Upload accepted; polling timed out before Paperless finished processing | `TaskURL` present, `DocumentURL` absent |

### Model

A single `DocumentResponse` struct in `models.go`, with `DocumentURL` and `TaskURL` as `*string` and `omitempty` JSON tags so each case produces only the relevant field.

## Task polling & URL resolution

### Config

- **New env var**: `PAPERLESS_TASK_TIMEOUT_SECONDS` (integer seconds). Default: `120`.
- Read once at startup in `main()`. Passed into the handler and into the Paperless client method.

### Polling

New method `func (c *PaperlessClient) WaitForDocument(ctx context.Context, taskID string, timeout time.Duration) (docID int, err error)`:

- Endpoint: `GET /api/tasks/?task_id=<uuid>`. Paperless returns a list; the matching task is at index 0.
- Poll interval: fixed **1 second** between polls.
- Terminal states:
  - `status == "SUCCESS"` → read `related_document` (int); return its ID.
  - `status == "FAILURE"` → return typed `ErrTaskFailed{TaskID, Result}` carrying Paperless's `result` string.
  - Anything else (`PENDING`, `STARTED`, empty list, etc.) → sleep 1s and retry.
- Timeout: compare `time.Since(start) >= timeout`; on expiry return typed `ErrTaskTimeout{TaskID}`.

### Poll interval testability

The 1-second interval is too slow for unit tests. To avoid a package-level var (global mutable state), store the interval on `PaperlessClient`:

```go
type PaperlessClient struct {
    // ...existing fields
    taskPollInterval time.Duration
}
```

`NewPaperlessClient` defaults it to `1 * time.Second`. Tests construct the client and override `taskPollInterval` (e.g., `10 * time.Millisecond`) before calling `WaitForDocument`.

### URL construction

- `DocumentURL`: `fmt.Sprintf("%s/documents/%d/", baseURL, docID)`.
- `TaskURL`:     `fmt.Sprintf("%s/api/tasks/?task_id=%s", baseURL, url.QueryEscape(taskID))`.
- `baseURL` is `PAPERLESS_BASE_URL` verbatim; trailing-slash handling is left as-is (current code assumes no trailing slash).

### Handler flow

After `UploadDocument` returns the task UUID, `handleDocumentUpload`:

1. Calls `WaitForDocument(r.Context(), taskID, timeout)`.
2. On success (int ID) → builds response with `DocumentURL`; responds `201`.
3. On `ErrTaskTimeout` → builds response with `TaskURL`; responds `202`.
4. On `ErrTaskFailed` → responds `502` with `Code: "paperless_task_failed"` and `Details: {"TaskID": "...", "Result": "..."}`.
5. On any other error (transport, decode, unexpected status from the task endpoint) → responds `502` with `Code: "paperless_error"` and `Details: {"Stage": "task_poll", "Message": "..."}`.

## Error Response

### Shape

```json
{
  "Code": "<stable_machine_code>",
  "Error": "<human_readable_message>",
  "Details": { ... optional, omitted when empty ... }
}
```

- `Code` and `Error` always present.
- `Details` is a JSON object whose keys vary by `Code` (see table). Modeled as `map[string]any` on the wire; constructed in the handler per error path. Tagged `omitempty`.

### Status × code map

| HTTP | `Code` | When | `Details` |
|------|--------|------|-----------|
| 400 | `invalid_json` | JSON decode of request body fails | — |
| 400 | `invalid_base64` | Base64 decode of `Data` fails | — |
| 400 | `sha256_mismatch` | Supplied `SHA256Hash` ≠ computed | `{"Expected":"<computed>", "Got":"<supplied>"}` |
| 400 | `validation_failed` | Required-field check fails | `{"MissingFields":["Correspondent","ShortSummary", ...]}` (all missing, not just the first) |
| 405 | `method_not_allowed` | Wrong method on an endpoint | — |
| 409 | `duplicate_document` | SHA256 already present as a `sha256:` tag with ≥1 document | `{"SHA256Hash":"<hex>"}` |
| 502 | `paperless_error` | Any non-task error from Paperless (entity resolution, upload, dedup check, task-poll transport) | `{"Stage":"<dedup_check\|correspondent\|document_type\|storage_path\|tag\|custom_field\|upload\|task_poll>", "Message":"<upstream body/err>"}` |
| 502 | `paperless_task_failed` | Task status is `FAILURE` | `{"TaskID":"<uuid>", "Result":"<paperless result text>"}` |

### Validation change

`DocumentRequest.Validate()` currently returns the first missing field as a plain `error`. Replace with a typed error:

```go
type ValidationError struct {
    MissingFields []string
}

func (e *ValidationError) Error() string { /* "missing required fields: a, b, c" */ }
```

`Validate()` walks all fields, collects every blank one, and returns `*ValidationError` if the list is non-empty. The handler type-asserts to populate `Details.MissingFields`.

## Code organization

### Files touched

- `models.go`
  - New `DocumentResponse` struct.
  - New `ErrorResponse` struct.
  - `ValidationError` type.
  - `Validate()` returns `*ValidationError`.
- `paperless.go`
  - New `WaitForDocument(ctx, taskID, timeout) (int, error)`.
  - New typed errors `ErrTaskTimeout`, `ErrTaskFailed`.
  - `PaperlessClient.taskPollInterval` field; `NewPaperlessClient` defaults it to `1s`.
- `main.go`
  - Read `PAPERLESS_TASK_TIMEOUT_SECONDS` at startup (default `120`).
  - Wire timeout into handler.
  - New `buildDocumentResponse(req DocumentRequest, taskID string) DocumentResponse` helper (fills everything except the URL fields).
  - New `writeError(w, statusCode, code, error, details)` helper; remove `writeJSONError` (or reimplement it on top).
- `README.md` — update the API section (response shape, status codes, error codes, new env var).

### Response builder

`buildDocumentResponse` copies fields from the normalized `DocumentRequest`. The handler attaches `DocumentURL` or `TaskURL` depending on polling outcome. The `Tags` field in the response is sourced from the deduped user tags **before** the `sha256:` tag is appended for Paperless.

## Testing

Add tests (TDD, consistent with the existing httptest pattern):

1. `TestBuildDocumentResponse` — given a normalized `DocumentRequest` and a task ID, produces a response with all expected fields, no `Data`, no `sha256:` in `Tags`.
2. `TestValidate_ReturnsAllMissingFields` — several blank fields; `*ValidationError` surfaces all of them.
3. `TestWaitForDocument_Success` — task endpoint returns `PENDING` once, then `SUCCESS` with `related_document`; method returns the ID within the timeout. Uses `taskPollInterval = 10ms`.
4. `TestWaitForDocument_Failure` — task endpoint returns `FAILURE` with `result`; method returns `ErrTaskFailed` carrying `result`.
5. `TestWaitForDocument_Timeout` — endpoint always returns `PENDING`; method returns `ErrTaskTimeout` after a short test timeout.
6. `TestHandleDocumentUpload_Success201` — end-to-end: task resolves to a document ID; verifies 201, `DocumentURL`, echoed fields, `sha256:` tag stripped from `Tags`.
7. `TestHandleDocumentUpload_TaskTimeout202` — task never resolves; verifies 202, `TaskURL` present, `DocumentURL` absent.
8. `TestHandleDocumentUpload_TaskFailed502` — verifies 502, `Code=paperless_task_failed`, `Details.Result` populated.
9. `TestHandleDocumentUpload_ValidationErrorDetails` — multiple blank required fields; verifies 400, `Code=validation_failed`, `Details.MissingFields` lists all of them.
10. `TestHandleDocumentUpload_SHA256MismatchDetails` — verifies 400, `Code=sha256_mismatch`, `Details.Expected`/`Details.Got`.
11. `TestHandleDocumentUpload_DuplicateDetails` — verifies 409, `Code=duplicate_document`, `Details.SHA256Hash`.

Existing tests that assert on the old `{"task_id": ...}` shape or the old error shape will be updated in the same change.

## Known limitations

- **Poll interval is fixed at 1s** (no exponential backoff). For a private-use tool this is simple and predictable; if it ever becomes a performance issue, revisit.
- **Task-list ordering**: we read `results[0]` from `/api/tasks/?task_id=<uuid>`. If Paperless ever returns multiple tasks for the same UUID, only the first is considered. The filter is by UUID, so in practice this is always exactly one row.
- **`PAPERLESS_TASK_TIMEOUT_SECONDS` parsing errors** (non-integer, negative) fall back to the default with a log warning — no fatal error on startup.
