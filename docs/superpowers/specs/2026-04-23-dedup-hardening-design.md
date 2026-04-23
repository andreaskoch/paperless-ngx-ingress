# Deduplication Hardening — Design

## Problem

The ingress currently forwards documents to Paperless-ngx and lazily creates the referenced correspondents, document types, storage paths, tags, and custom fields. The existing get-or-create logic has five gaps that can produce duplicate entities or misrouted documents:

1. **Whitespace variants miss `name__iexact`**. `" Test Corp"` (leading space) does not match an existing `"Test Corp"`, so a near-duplicate is created.
2. **Custom field matching is case-sensitive**, inconsistent with the case-insensitive lookup used for every other entity type.
3. **Incoming tag lists are not deduplicated**. `["invoice", "Invoice", " invoice "]` causes three API calls and repeated IDs in the upload payload.
4. **Concurrent requests race on create**. Two requests resolving the same new entity both GET-miss, both POST; Paperless rejects the loser, which bubbles up as a 502.
5. **Storage-path `path` field diverges silently**. If the template pattern in code changes, existing `storage_paths` rows are re-used by name but keep their stale `path` — documents are filed under a now-unintended layout without any warning.

## Goals

- After the change, no path in the request handler can create a duplicate correspondent, document type, storage path, or custom field under the scenarios above.
- Tag inputs are normalized and deduplicated before hitting Paperless.
- Storage-path templates stay in sync with the code's desired pattern without human intervention.

## Non-goals

- **Pagination of custom-field listing** (concern #2) is out of scope. The current single-page-of-100 fetch stays; this is noted as a known limitation. If a deployment ever approaches 100 custom fields, re-open this.
- **Cross-process caching** of entity IDs. Paperless's DB is the source of truth; we do not introduce a shared cache.
- **Retroactive cleanup** of duplicates created before this change. Only prevention of new ones.

## Approach

Targeted, layered fixes. Normalization and deduplication happen once at the request boundary (`main.go`); case-sensitivity and race handling are fixed inside the Paperless client (`paperless.go`); storage-path divergence is handled by a new dedicated client method.

We do not introduce in-process mutexes or caches. Paperless's own uniqueness constraints are the source of truth for "does this already exist," and a single re-search after a POST conflict is sufficient to recover from races.

## Design

### 1. Input normalization

Add `normalizeName(s string) string` (new file `normalize.go`, or alongside the request struct in `models.go`):

- `strings.TrimSpace`
- Collapse internal runs of whitespace to a single ASCII space using `strings.Fields` + `strings.Join`.

Apply in `handleDocumentUpload` **before** passing to any `GetOrCreate*`:

- `Correspondent`, `DocumentType`, `Recipient` — normalize in place.
- Each element of `Tags` — normalize as part of the dedup pass (see §2).

Custom-field names are hardcoded literals (`"DocumentLanguageCode"` etc.); no normalization needed.

**Required-field behavior**: normalization runs before `Validate()` so a field containing only whitespace is treated as empty and fails validation with the existing clear error. (Validation currently runs after `FillDateDefaults`; normalization slots in between.)

### 2. Tag de-duplication

In `handleDocumentUpload`, replace the current `allTags := make([]string, len(docReq.Tags)+1)` construction with:

```go
func dedupTagNames(raw []string) []string {
    seen := make(map[string]struct{})
    out := make([]string, 0, len(raw))
    for _, t := range raw {
        n := normalizeName(t)
        if n == "" {
            continue
        }
        key := strings.ToLower(n)
        if _, dup := seen[key]; dup {
            continue
        }
        seen[key] = struct{}{}
        out = append(out, n)
    }
    return out
}
```

- Dedup key: `strings.ToLower(normalizeName(tag))`. Insertion order preserved; first occurrence wins.
- Empty-after-normalize tags are dropped silently.
- The `sha256:<hash>` dedup tag is appended **after** user-tag dedup and re-checked against the `seen` set (cheap defense; a user-supplied `"sha256:..."` string would otherwise produce a duplicate).

After resolving IDs, also dedupe the resulting `tagIDs` slice (two input names could collapse to the same existing tag under Paperless's collation). A small `dedupInts([]int) []int` helper handles this.

### 3. Case-insensitive custom field match

In `paperless.go`, inside `GetOrCreateCustomField`, replace:

```go
if fieldName, ok := field["name"].(string); ok && fieldName == name {
```

with:

```go
if fieldName, ok := field["name"].(string); ok && strings.EqualFold(fieldName, name) {
```

The POST body keeps the caller-supplied `name` verbatim — we relax matching, not writing.

### 4. Race-safe create (retry on uniqueness conflict)

Refactor the create branch of both `GetOrCreateEntity` and `GetOrCreateCustomField`:

1. POST.
2. On `201` — decode and return ID (unchanged).
3. On `400` — attempt to parse the body as `map[string][]string`. If it contains a `"name"` key with a non-empty list, treat as a uniqueness conflict: re-run the original GET search and return that ID.
4. On any other status, or a `400` that is not a name-key conflict — return the error as today.

**Single retry only**. If the re-search still returns zero results, return the original 400 error unchanged. Do not loop.

Extract the retry helper (`searchEntity(entityType, name)` returning an ID or a "not found" sentinel) if the branches duplicate cleanly between the two call sites; otherwise inline.

### 5. Storage path divergence

Introduce `func (c *PaperlessClient) GetOrCreateStoragePath(name, path string) (int, error)` in `paperless.go`:

1. GET `/api/storage_paths/?name__iexact=<url-encoded name>`.
2. If found:
   - Read `results[0]["path"]`.
   - If it equals the desired `path` — return the ID.
   - If not — `PATCH /api/storage_paths/<id>/` with `{"path": "<desired>"}`; return the ID.
3. If not found — POST with `{"name": name, "path": path}`, applying the race-retry from §4.

In `main.go`, replace:

```go
storagePathID, err := client.GetOrCreateEntity("storage_paths", docReq.Recipient, map[string]string{
    "path": storagePathPattern,
})
```

with:

```go
storagePathID, err := client.GetOrCreateStoragePath(docReq.Recipient, storagePathPattern)
```

**Why PATCH rather than error or new-entity**:

- Error-out would break every ingest after a template change until a human intervenes — bad UX for a private tool.
- Creating under a suffixed name clutters Paperless and breaks the implicit 1:1 recipient-to-storage-path mapping the code already relies on.
- PATCH matches the "upsert" intent of `GetOrCreate*`, self-heals after template edits, and is safe: Paperless storage paths are templates applied at ingest time; updating the template does not retroactively move existing documents.

PATCH is idempotent, so a concurrent double-update converges.

## Testing

Unit tests using `httptest.Server` (the existing pattern in `paperless_test.go`):

- `TestNormalizeName` — trimming, internal whitespace collapse, all-whitespace → empty.
- `TestDedupTagNames` — exact duplicates, case variants, whitespace variants, empty-after-normalize dropped, order preserved.
- `TestGetOrCreateEntity_RaceRetry` — GET returns 0, POST returns 400 with `{"name":["already exists"]}`, re-GET returns the entity; the method returns the existing ID.
- `TestGetOrCreateEntity_400NotUnique` — POST returns 400 with a non-name body; original error propagates.
- `TestGetOrCreateCustomField_CaseInsensitive` — list contains `"ShortSummary"`, lookup `"shortsummary"` resolves to the existing ID.
- `TestGetOrCreateCustomField_RaceRetry` — same pattern as entity race test.
- `TestGetOrCreateStoragePath_PathMatches` — existing row with same path, no PATCH issued.
- `TestGetOrCreateStoragePath_PathDiverges` — existing row with stale path; PATCH is issued to the desired value and ID returned.
- `TestGetOrCreateStoragePath_NotFound` — POST path with race-retry.

Integration-ish: extend an existing `handleDocumentUpload`-level test (if any) or add one that verifies whitespace-variant correspondent names resolve to a single ID across two requests.

## Known limitations

- **Custom field pagination**: still single-page (100 items). Tracked as concern #2; intentionally deferred.
- **Paperless 400-body shape**: the race-retry relies on Paperless returning `{"name":[...]}` on uniqueness conflicts. If a future Paperless version changes the error shape, the retry falls through to the original error and the request fails — no silent wrong behavior, just a visible 502.
