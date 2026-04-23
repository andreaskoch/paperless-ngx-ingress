# Duplicate Returns Existing — Design

## Problem

Currently, re-submitting a document whose SHA256 hash is already in Paperless
returns `409 Conflict` with `Code: duplicate_document`. Callers that treat
ingestion as idempotent (a pipeline that may retry or replay events) have to
special-case this error. The ingress should instead return the existing
document's URL so that callers can treat a re-submit as a successful no-op.

## Goals

- On a SHA256 collision, respond `200 OK` with a `DocumentResponse` whose
  `DocumentURL` points at the existing Paperless document.
- Skip all side-effecting work (entity resolution, upload, polling) when a
  duplicate is detected.
- Mirror the cleaned incoming request in the response body, consistent with
  option A from the brainstorm (we do not fetch the existing document's
  authoritative metadata from Paperless; the response echoes what the caller
  just sent, plus the URL).

## Non-goals

- Reverse-resolving Paperless IDs to return authoritative stored metadata.
- An opt-in "strict mode" that preserves the old 409 behavior. If a caller
  ever needs strict semantics, they can compare SHA256 client-side before
  sending.

## Design

### 1. `CheckDuplicate` returns the document ID

Change the signature:

```go
// before
func (c *PaperlessClient) CheckDuplicate(sha256Hash string) (bool, error)

// after
func (c *PaperlessClient) CheckDuplicate(sha256Hash string) (docID int, found bool, err error)
```

Internal flow is unchanged: find the `sha256:<hash>` tag, then search documents
filtered by that tag. When `docResult.Count > 0`, read
`results[0]["id"].(float64)` and return `(int(id), true, nil)`. When the tag
or the document set is empty, return `(0, false, nil)`. On any transport or
decode error, return `(0, false, err)`.

### 2. Handler short-circuits to 200

In `handleDocumentUpload`, replace the current 409 branch:

```go
existingID, found, err := client.CheckDuplicate(docReq.SHA256Hash)
if err != nil {
    paperlessErr(w, "dedup_check", err)
    return
}
if found {
    response := buildDocumentResponse(docReq, "")
    response.DocumentURL = fmt.Sprintf("%s/documents/%d/", client.baseURL, existingID)
    writeJSON(w, http.StatusOK, response)
    return
}
```

After this branch, execution proceeds to entity resolution and upload as
before. Nothing between this branch and the upload runs on a duplicate.

### 3. `TaskID` becomes `omitempty`

Since a 200 duplicate response has no task, `TaskID` is left as `""`. To avoid
noise in the response body, the struct tag changes from
`json:"TaskID"` to `json:"TaskID,omitempty"`. On 201/202 responses the field
is still set and emitted; only on 200 (duplicate) does it disappear.

### 4. README

- Add `200 OK` to the success-status table: "Document already exists;
  response contains `DocumentURL` of the existing document."
- Remove the `409 Conflict` row from both the HTTP-status table and the error
  code table (the `duplicate_document` code is gone).
- Note that on a 200 duplicate, `TaskID` is absent because no new task was
  created.

## Testing

- Rewrite `TestHandleDocumentUpload_DuplicateDetails` as
  `TestHandleDocumentUpload_DuplicateReturns200`: the mock returns the dedup
  tag and one matching document; the handler returns 200 with the correct
  `DocumentURL` and echoed cleaned input; the response body has no `TaskID`
  key and no `TaskURL` key.
- Update `TestCheckDuplicate_Found` to assert the returned `docID` equals the
  fixture document ID and `found` is `true`.
- Update `TestCheckDuplicate_NoDuplicate` to assert `docID == 0` and
  `found == false`.
- Add `TestHandleDocumentUpload_DuplicateDoesNotUpload`: the mock's handler
  calls `t.Fatal` for any path that is not the dedup tag/document lookup,
  proving the handler short-circuits before entity creation, upload, or
  polling.

## Known limitations

- If the stored document's metadata in Paperless has drifted from what's in
  the incoming request (different tags, different correspondent), the 200
  response echoes the request, not the stored state. This is the conscious
  tradeoff from option A in the brainstorm. Callers that need the
  authoritative stored metadata can read it from the returned `DocumentURL`.
