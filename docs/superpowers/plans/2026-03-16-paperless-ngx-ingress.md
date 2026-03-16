# Paperless NGX Ingress Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go REST API that receives documents via JSON and forwards them to Paperless NGX with full entity resolution.

**Architecture:** Single-package Go service with stdlib `net/http`. Three source files: `models.go` (structs), `paperless.go` (API client), `main.go` (server + handler). Plus Dockerfile, GitHub Actions, README.

**Tech Stack:** Go 1.24, stdlib `net/http`, `github.com/joho/godotenv`

---

## File Structure

| File | Responsibility |
|---|---|
| `models.go` | Request struct (`DocumentRequest`), amount struct, response structs |
| `paperless.go` | Paperless NGX API client: entity resolution (get-or-create), custom field management, document upload |
| `main.go` | HTTP server, `/api/documents` handler, `/health` handler, config loading, request validation, SHA256 verification, orchestration |
| `models_test.go` | Tests for validation logic |
| `paperless_test.go` | Tests for Paperless client using httptest server |
| `main_test.go` | Integration tests for HTTP handlers |
| `.env.example` | Example environment config |
| `Dockerfile` | Multi-stage Docker build |
| `.github/workflows/docker-build.yml` | CI/CD for Docker image |
| `README.md` | Usage documentation |

---

## Chunk 1: Project Scaffolding and Models

### Task 1: Initialize Go module and dependencies

**Files:**
- Create: `go.mod`

- [ ] **Step 1: Initialize Go module**

```bash
cd /Users/andreaskoch/projects/private/paperless-ngx-ingress
go mod init github.com/andreaskoch/paperless-ngx-ingress
```

- [ ] **Step 2: Add godotenv dependency**

```bash
go get github.com/joho/godotenv
```

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "feat: initialize Go module with godotenv dependency"
```

### Task 2: Create request/response models

**Files:**
- Create: `models.go`

- [ ] **Step 1: Write models_test.go with validation tests**

Create `models_test.go`:

```go
package main

import (
	"strings"
	"testing"
)

func TestValidateDocumentRequest_Valid(t *testing.T) {
	req := validRequest()
	if err := req.Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateDocumentRequest_MissingFields(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*DocumentRequest)
		wantErr string
	}{
		{"missing SHA256Hash", func(r *DocumentRequest) { r.SHA256Hash = "" }, "SHA256Hash"},
		{"missing Data", func(r *DocumentRequest) { r.Data = "" }, "Data"},
		{"missing OriginalFilename", func(r *DocumentRequest) { r.OriginalFilename = "" }, "OriginalFilename"},
		{"missing FileType", func(r *DocumentRequest) { r.FileType = "" }, "FileType"},
		{"missing DocumentDate", func(r *DocumentRequest) { r.DocumentDate = "" }, "DocumentDate"},
		{"missing DocumentType", func(r *DocumentRequest) { r.DocumentType = "" }, "DocumentType"},
		{"missing DocumentLanguageCode", func(r *DocumentRequest) { r.DocumentLanguageCode = "" }, "DocumentLanguageCode"},
		{"missing Correspondent", func(r *DocumentRequest) { r.Correspondent = "" }, "Correspondent"},
		{"missing CorrespondentDetails", func(r *DocumentRequest) { r.CorrespondentDetails = "" }, "CorrespondentDetails"},
		{"missing Recipient", func(r *DocumentRequest) { r.Recipient = "" }, "Recipient"},
		{"missing RecipientDetails", func(r *DocumentRequest) { r.RecipientDetails = "" }, "RecipientDetails"},
		{"missing ShortSummary", func(r *DocumentRequest) { r.ShortSummary = "" }, "ShortSummary"},
		{"missing LongSummary", func(r *DocumentRequest) { r.LongSummary = "" }, "LongSummary"},
		{"missing ProposedFilename", func(r *DocumentRequest) { r.ProposedFilename = "" }, "ProposedFilename"},
		{"missing Tags", func(r *DocumentRequest) { r.Tags = nil }, "Tags"},
		{"missing Amounts", func(r *DocumentRequest) { r.Amounts = nil }, "Amounts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRequest()
			tt.mutate(&req)
			err := req.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func validRequest() DocumentRequest {
	return DocumentRequest{
		SHA256Hash:           "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		Data:                 "dGVzdA==",
		OriginalFilename:     "test.pdf",
		FileType:             "pdf",
		DocumentDate:         "2026-01-01",
		DocumentType:         "Invoice",
		DocumentLanguageCode: "en",
		Correspondent:        "Test Corp",
		CorrespondentDetails: "Test Corp, 123 Main St",
		Recipient:            "My Company",
		RecipientDetails:     "My Company, 456 Oak Ave",
		ShortSummary:         "Test invoice",
		LongSummary:          "A detailed test invoice description",
		ProposedFilename:     "2026-01-01 Invoice from Test Corp",
		Amounts: []Amount{
			{Type: "Total", Amount: 100, CurrencyCode: "EUR"},
		},
		Tags: []string{"test", "invoice"},
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -run TestValidateDocumentRequest -v
```

Expected: FAIL — `DocumentRequest` and `Amount` types not defined.

- [ ] **Step 3: Write models.go**

Create `models.go`:

```go
package main

import "fmt"

// DocumentRequest is the JSON body accepted by POST /api/documents.
type DocumentRequest struct {
	SHA256Hash           string   `json:"SHA256Hash"`
	Data                 string   `json:"Data"`
	OriginalFilename     string   `json:"OriginalFilename"`
	FileType             string   `json:"FileType"`
	DocumentDate         string   `json:"DocumentDate"`
	Year                 string   `json:"Year"`
	Month                string   `json:"Month"`
	Day                  string   `json:"Day"`
	DocumentType         string   `json:"DocumentType"`
	DocumentLanguageCode string   `json:"DocumentLanguageCode"`
	Correspondent        string   `json:"Correspondent"`
	CorrespondentDetails string   `json:"CorrespondentDetails"`
	Recipient            string   `json:"Recipient"`
	RecipientDetails     string   `json:"RecipientDetails"`
	ShortSummary         string   `json:"ShortSummary"`
	LongSummary          string   `json:"LongSummary"`
	ProposedFilename     string   `json:"ProposedFilename"`
	Amounts              []Amount `json:"Amounts"`
	Tags                 []string `json:"Tags"`
}

type Amount struct {
	Type         string  `json:"type"`
	Amount       float64 `json:"Amount"`
	CurrencyCode string  `json:"CurrencyCode"`
}

func (r *DocumentRequest) Validate() error {
	type fieldCheck struct {
		name  string
		value string
	}
	checks := []fieldCheck{
		{"SHA256Hash", r.SHA256Hash},
		{"Data", r.Data},
		{"OriginalFilename", r.OriginalFilename},
		{"FileType", r.FileType},
		{"DocumentDate", r.DocumentDate},
		{"DocumentType", r.DocumentType},
		{"DocumentLanguageCode", r.DocumentLanguageCode},
		{"Correspondent", r.Correspondent},
		{"CorrespondentDetails", r.CorrespondentDetails},
		{"Recipient", r.Recipient},
		{"RecipientDetails", r.RecipientDetails},
		{"ShortSummary", r.ShortSummary},
		{"LongSummary", r.LongSummary},
		{"ProposedFilename", r.ProposedFilename},
	}
	for _, c := range checks {
		if c.value == "" {
			return fmt.Errorf("missing required field: %s", c.name)
		}
	}
	if len(r.Tags) == 0 {
		return fmt.Errorf("missing required field: Tags")
	}
	if len(r.Amounts) == 0 {
		return fmt.Errorf("missing required field: Amounts")
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -run TestValidateDocumentRequest -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add models.go models_test.go
git commit -m "feat: add document request models with validation"
```

---

## Chunk 2: Paperless NGX API Client

### Task 3: Paperless client — entity resolution (get-or-create)

**Files:**
- Create: `paperless.go`
- Create: `paperless_test.go`

- [ ] **Step 1: Write tests for entity resolution**

Create `paperless_test.go`:

```go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetOrCreateEntity_ExistingEntity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []map[string]any{
					{"id": 42, "name": "Test Corp"},
				},
			})
			return
		}
		t.Fatal("unexpected request method:", r.Method)
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	id, err := client.GetOrCreateEntity("correspondents", "Test Corp", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 42 {
		t.Fatalf("expected id 42, got %d", id)
	}
}

func TestGetOrCreateEntity_CreateNew(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{
				"count":   0,
				"results": []map[string]any{},
			})
			return
		}
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"id":   99,
				"name": "New Entity",
			})
			return
		}
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	id, err := client.GetOrCreateEntity("correspondents", "New Entity", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 99 {
		t.Fatalf("expected id 99, got %d", id)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 calls (GET + POST), got %d", callCount)
	}
}

func TestGetOrCreateEntity_WithExtraFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{
				"count":   0,
				"results": []map[string]any{},
			})
			return
		}
		if r.Method == http.MethodPost {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["path"] != "/{Recipient}/{{ created_year }}/{{ correspondent }}/{{ title }}" {
				t.Fatalf("expected path field in POST body, got: %v", body)
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"id":   10,
				"name": "My Company",
			})
			return
		}
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	extra := map[string]string{
		"path": "/{Recipient}/{{ created_year }}/{{ correspondent }}/{{ title }}",
	}
	id, err := client.GetOrCreateEntity("storage_paths", "My Company", extra)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 10 {
		t.Fatalf("expected id 10, got %d", id)
	}
}

func TestGetOrCreateEntity_AuthHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Token my-secret" {
			t.Fatalf("expected 'Token my-secret', got %q", auth)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"count":   1,
			"results": []map[string]any{{"id": 1, "name": "x"}},
		})
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "my-secret")
	_, err := client.GetOrCreateEntity("tags", "x", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
func TestGetOrCreateEntity_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	_, err := client.GetOrCreateEntity("correspondents", "Test", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to contain status code, got: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -run TestGetOrCreateEntity -v
```

Expected: FAIL — `PaperlessClient` not defined.

- [ ] **Step 3: Write paperless.go with entity resolution**

Create `paperless.go`:

```go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type PaperlessClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

func NewPaperlessClient(baseURL, token string) *PaperlessClient {
	return &PaperlessClient{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{},
	}
}

func (c *PaperlessClient) doRequest(method, path string, body io.Reader, contentType string) (*http.Response, error) {
	reqURL := c.baseURL + path
	req, err := http.NewRequest(method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+c.token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return c.httpClient.Do(req)
}

type paginatedResponse struct {
	Count   int              `json:"count"`
	Results []map[string]any `json:"results"`
}

// GetOrCreateEntity finds an entity by name or creates it. extraFields are
// included in the POST body when creating (e.g. "path" for storage_paths).
func (c *PaperlessClient) GetOrCreateEntity(entityType, name string, extraFields map[string]string) (int, error) {
	// Search for existing
	searchPath := fmt.Sprintf("/api/%s/?name__iexact=%s", entityType, url.QueryEscape(name))
	resp, err := c.doRequest(http.MethodGet, searchPath, nil, "")
	if err != nil {
		return 0, fmt.Errorf("searching %s: %w", entityType, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("searching %s: status %d: %s", entityType, resp.StatusCode, string(body))
	}

	var paginated paginatedResponse
	if err := json.NewDecoder(resp.Body).Decode(&paginated); err != nil {
		return 0, fmt.Errorf("decoding %s response: %w", entityType, err)
	}

	if paginated.Count > 0 && len(paginated.Results) > 0 {
		id, ok := paginated.Results[0]["id"].(float64)
		if !ok {
			return 0, fmt.Errorf("invalid id type in %s response", entityType)
		}
		return int(id), nil
	}

	// Create new
	createBody := map[string]string{"name": name}
	for k, v := range extraFields {
		createBody[k] = v
	}
	jsonBody, err := json.Marshal(createBody)
	if err != nil {
		return 0, fmt.Errorf("marshaling %s body: %w", entityType, err)
	}

	createPath := fmt.Sprintf("/api/%s/", entityType)
	resp2, err := c.doRequest(http.MethodPost, createPath, bytes.NewReader(jsonBody), "application/json")
	if err != nil {
		return 0, fmt.Errorf("creating %s: %w", entityType, err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp2.Body)
		return 0, fmt.Errorf("creating %s: status %d: %s", entityType, resp2.StatusCode, string(body))
	}

	var created map[string]any
	if err := json.NewDecoder(resp2.Body).Decode(&created); err != nil {
		return 0, fmt.Errorf("decoding created %s: %w", entityType, err)
	}

	id, ok := created["id"].(float64)
	if !ok {
		return 0, fmt.Errorf("invalid id in created %s", entityType)
	}
	return int(id), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -run TestGetOrCreateEntity -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add paperless.go paperless_test.go
git commit -m "feat: add Paperless API client with entity resolution"
```

### Task 4: Paperless client — custom field resolution

**Files:**
- Modify: `paperless.go`
- Modify: `paperless_test.go`

- [ ] **Step 1: Write tests for custom field resolution**

Append to `paperless_test.go`:

```go
func TestGetOrCreateCustomField_Existing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"count": 2,
			"results": []map[string]any{
				{"id": 5, "name": "ShortSummary", "data_type": "longtext"},
				{"id": 6, "name": "LongSummary", "data_type": "longtext"},
			},
		})
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	id, err := client.GetOrCreateCustomField("ShortSummary", "longtext")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 5 {
		t.Fatalf("expected id 5, got %d", id)
	}
}

func TestGetOrCreateCustomField_CreateNew(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{
				"count":   0,
				"results": []map[string]any{},
			})
			return
		}
		if r.Method == http.MethodPost {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["name"] != "Amounts" {
				t.Fatalf("expected name 'Amounts', got %q", body["name"])
			}
			if body["data_type"] != "longtext" {
				t.Fatalf("expected data_type 'longtext', got %q", body["data_type"])
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{
				"id":        12,
				"name":      "Amounts",
				"data_type": "longtext",
			})
			return
		}
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	id, err := client.GetOrCreateCustomField("Amounts", "longtext")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 12 {
		t.Fatalf("expected id 12, got %d", id)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -run TestGetOrCreateCustomField -v
```

Expected: FAIL — `GetOrCreateCustomField` not defined.

- [ ] **Step 3: Add GetOrCreateCustomField to paperless.go**

Append to `paperless.go`:

```go
// GetOrCreateCustomField finds a custom field by name or creates it with the given data type.
func (c *PaperlessClient) GetOrCreateCustomField(name, dataType string) (int, error) {
	// List all custom fields
	resp, err := c.doRequest(http.MethodGet, "/api/custom_fields/?page_size=100", nil, "")
	if err != nil {
		return 0, fmt.Errorf("listing custom fields: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("listing custom fields: status %d: %s", resp.StatusCode, string(body))
	}

	var paginated paginatedResponse
	if err := json.NewDecoder(resp.Body).Decode(&paginated); err != nil {
		return 0, fmt.Errorf("decoding custom fields: %w", err)
	}

	for _, field := range paginated.Results {
		if fieldName, ok := field["name"].(string); ok && fieldName == name {
			id, ok := field["id"].(float64)
			if !ok {
				return 0, fmt.Errorf("invalid id for custom field %s", name)
			}
			return int(id), nil
		}
	}

	// Create new
	createBody := map[string]string{
		"name":      name,
		"data_type": dataType,
	}
	jsonBody, err := json.Marshal(createBody)
	if err != nil {
		return 0, fmt.Errorf("marshaling custom field: %w", err)
	}

	resp2, err := c.doRequest(http.MethodPost, "/api/custom_fields/", bytes.NewReader(jsonBody), "application/json")
	if err != nil {
		return 0, fmt.Errorf("creating custom field: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp2.Body)
		return 0, fmt.Errorf("creating custom field: status %d: %s", resp2.StatusCode, string(body))
	}

	var created map[string]any
	if err := json.NewDecoder(resp2.Body).Decode(&created); err != nil {
		return 0, fmt.Errorf("decoding created custom field: %w", err)
	}

	id, ok := created["id"].(float64)
	if !ok {
		return 0, fmt.Errorf("invalid id in created custom field")
	}
	return int(id), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -run TestGetOrCreateCustomField -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add paperless.go paperless_test.go
git commit -m "feat: add custom field resolution to Paperless client"
```

### Task 5: Paperless client — document upload

**Files:**
- Modify: `paperless.go`
- Modify: `paperless_test.go`

- [ ] **Step 1: Write test for document upload**

Append to `paperless_test.go`:

```go
import (
	"io"
	"mime"
	"mime/multipart"
	"strings"
)

func TestUploadDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/documents/post_document/" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("parsing content type: %v", err)
		}
		if !strings.HasPrefix(mediaType, "multipart/") {
			t.Fatalf("expected multipart, got %s", mediaType)
		}

		reader := multipart.NewReader(r.Body, params["boundary"])
		fields := map[string]string{}
		var tagValues []string
		var docContent []byte
		var docFilename string
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("reading part: %v", err)
			}
			val, _ := io.ReadAll(part)
			if part.FormName() == "document" {
				docContent = val
				docFilename = part.FileName()
			} else if part.FormName() == "tags" {
				tagValues = append(tagValues, string(val))
			} else {
				fields[part.FormName()] = string(val)
			}
		}

		// Verify document content and filename
		if string(docContent) != "hello pdf" {
			t.Errorf("unexpected document content: %s", string(docContent))
		}
		if docFilename != "scan.pdf" {
			t.Errorf("expected filename 'scan.pdf', got %q", docFilename)
		}
		if fields["title"] != "2026-01-01 Test Doc" {
			t.Errorf("unexpected title: %s", fields["title"])
		}
		if fields["created"] != "2026-01-01" {
			t.Errorf("unexpected created: %s", fields["created"])
		}
		if fields["correspondent"] != "1" {
			t.Errorf("unexpected correspondent: %s", fields["correspondent"])
		}
		if fields["document_type"] != "2" {
			t.Errorf("unexpected document_type: %s", fields["document_type"])
		}
		if fields["storage_path"] != "3" {
			t.Errorf("unexpected storage_path: %s", fields["storage_path"])
		}
		if len(tagValues) != 2 {
			t.Errorf("expected 2 tag values, got %d", len(tagValues))
		}
		// Verify custom_fields JSON
		cfJSON := fields["custom_fields"]
		if cfJSON == "" {
			t.Error("custom_fields field is missing")
		}
		var cf map[string]any
		if err := json.Unmarshal([]byte(cfJSON), &cf); err != nil {
			t.Errorf("invalid custom_fields JSON: %v", err)
		}
		if cf["5"] != "de" {
			t.Errorf("expected custom field 5='de', got %v", cf["5"])
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode("abc-123-uuid")
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	params := UploadParams{
		DocumentData:     []byte("hello pdf"),
		OriginalFilename: "scan.pdf",
		Title:            "2026-01-01 Test Doc",
		Created:          "2026-01-01",
		CorrespondentID:  1,
		DocumentTypeID:   2,
		StoragePathID:    3,
		TagIDs:           []int{10, 20},
		CustomFields:     map[string]any{"5": "de", "6": "summary text"},
	}
	taskID, err := client.UploadDocument(params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskID != "abc-123-uuid" {
		t.Fatalf("expected task ID 'abc-123-uuid', got %q", taskID)
	}
}
```

Note: the import block at the top of `paperless_test.go` needs to be merged with the existing imports. The test file should have a single import block containing all needed packages.

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -run TestUploadDocument -v
```

Expected: FAIL — `UploadParams` and `UploadDocument` not defined.

- [ ] **Step 3: Add UploadDocument to paperless.go**

Append to `paperless.go`:

```go
import (
	"mime/multipart"
	"strconv"
)

// UploadParams contains all resolved IDs and data for uploading a document.
type UploadParams struct {
	DocumentData     []byte
	OriginalFilename string
	Title            string
	Created          string
	CorrespondentID  int
	DocumentTypeID   int
	StoragePathID    int
	TagIDs           []int
	CustomFields     map[string]any // field_id (as string) -> value
}

// UploadDocument uploads a document to Paperless NGX and returns the task UUID.
func (c *PaperlessClient) UploadDocument(params UploadParams) (string, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add document file
	part, err := writer.CreateFormFile("document", params.OriginalFilename)
	if err != nil {
		return "", fmt.Errorf("creating form file: %w", err)
	}
	if _, err := part.Write(params.DocumentData); err != nil {
		return "", fmt.Errorf("writing document data: %w", err)
	}

	// Add metadata fields
	writer.WriteField("title", params.Title)
	writer.WriteField("created", params.Created)
	writer.WriteField("correspondent", strconv.Itoa(params.CorrespondentID))
	writer.WriteField("document_type", strconv.Itoa(params.DocumentTypeID))
	writer.WriteField("storage_path", strconv.Itoa(params.StoragePathID))

	// Add tags — one form field per tag
	for _, tagID := range params.TagIDs {
		writer.WriteField("tags", strconv.Itoa(tagID))
	}

	// Add custom fields as JSON
	if len(params.CustomFields) > 0 {
		cfJSON, err := json.Marshal(params.CustomFields)
		if err != nil {
			return "", fmt.Errorf("marshaling custom fields: %w", err)
		}
		writer.WriteField("custom_fields", string(cfJSON))
	}

	writer.Close()

	resp, err := c.doRequest(http.MethodPost, "/api/documents/post_document/", &buf, writer.FormDataContentType())
	if err != nil {
		return "", fmt.Errorf("uploading document: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("uploading document: status %d: %s", resp.StatusCode, string(body))
	}

	// Paperless returns a JSON-encoded UUID string
	var taskID string
	if err := json.NewDecoder(resp.Body).Decode(&taskID); err != nil {
		return "", fmt.Errorf("decoding task ID: %w", err)
	}
	return taskID, nil
}
```

Note: merge the import block with the existing one at the top of `paperless.go`.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -run TestUploadDocument -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add paperless.go paperless_test.go
git commit -m "feat: add document upload to Paperless client"
```

### Task 6: Paperless client — tag-based deduplication check

**Files:**
- Modify: `paperless.go`
- Modify: `paperless_test.go`

- [ ] **Step 1: Write tests for deduplication**

Append to `paperless_test.go`:

```go
func TestCheckDuplicate_NoDuplicate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"count":   0,
			"results": []map[string]any{},
		})
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	exists, err := client.CheckDuplicate("abc123hash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Fatal("expected no duplicate")
	}
}

func TestCheckDuplicate_Found(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// First call: search for tag
		if strings.Contains(r.URL.Path, "tags") {
			json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []map[string]any{
					{"id": 55, "name": "sha256:abc123hash"},
				},
			})
			return
		}
		// Second call: search for documents with that tag
		json.NewEncoder(w).Encode(map[string]any{
			"count": 1,
			"results": []map[string]any{
				{"id": 100, "title": "existing doc"},
			},
		})
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	exists, err := client.CheckDuplicate("abc123hash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatal("expected duplicate to be found")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -run TestCheckDuplicate -v
```

Expected: FAIL — `CheckDuplicate` not defined.

- [ ] **Step 3: Add CheckDuplicate to paperless.go**

Append to `paperless.go`:

```go
// CheckDuplicate checks if a document with the given SHA256 hash already exists
// by looking for a tag named "sha256:<hash>" that is assigned to at least one document.
func (c *PaperlessClient) CheckDuplicate(sha256Hash string) (bool, error) {
	tagName := "sha256:" + sha256Hash

	// Search for the tag
	searchPath := fmt.Sprintf("/api/tags/?name__iexact=%s", url.QueryEscape(tagName))
	resp, err := c.doRequest(http.MethodGet, searchPath, nil, "")
	if err != nil {
		return false, fmt.Errorf("searching for dedup tag: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("searching for dedup tag: status %d: %s", resp.StatusCode, string(body))
	}

	var paginated paginatedResponse
	if err := json.NewDecoder(resp.Body).Decode(&paginated); err != nil {
		return false, fmt.Errorf("decoding tag search: %w", err)
	}

	if paginated.Count == 0 {
		return false, nil
	}

	// Tag exists — check if any documents use it
	tagID := int(paginated.Results[0]["id"].(float64))
	docPath := fmt.Sprintf("/api/documents/?tags__id=%d", tagID)
	resp2, err := c.doRequest(http.MethodGet, docPath, nil, "")
	if err != nil {
		return false, fmt.Errorf("checking documents for dedup tag: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		return false, fmt.Errorf("checking documents: status %d: %s", resp2.StatusCode, string(body))
	}

	var docResult paginatedResponse
	if err := json.NewDecoder(resp2.Body).Decode(&docResult); err != nil {
		return false, fmt.Errorf("decoding document search: %w", err)
	}

	return docResult.Count > 0, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -run TestCheckDuplicate -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add paperless.go paperless_test.go
git commit -m "feat: add SHA256-based deduplication check"
```

---

## Chunk 3: HTTP Server, Handler, and Orchestration

### Task 7: Main handler — health endpoint and server setup

**Files:**
- Create: `main.go`
- Create: `main_test.go`

- [ ] **Step 1: Write test for health endpoint**

Create `main_test.go`:

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	expected := `{"status":"ok"}`
	got := strings.TrimSpace(w.Body.String())
	if got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test -run TestHealthEndpoint -v
```

Expected: FAIL — `handleHealth` not defined.

- [ ] **Step 3: Write main.go with health endpoint and server setup**

Create `main.go`:

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
)

func main() {
	// Load .env file (ignore error if not present — env vars may be set directly)
	godotenv.Load()

	baseURL := os.Getenv("PAPERLESS_BASE_URL")
	token := os.Getenv("PAPERLESS_API_TOKEN")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8471"
	}

	if baseURL == "" || token == "" {
		log.Fatal("PAPERLESS_BASE_URL and PAPERLESS_API_TOKEN must be set")
	}

	client := NewPaperlessClient(baseURL, token)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/api/documents", func(w http.ResponseWriter, r *http.Request) {
		handleDocumentUpload(w, r, client)
	})

	log.Printf("Starting server on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test -run TestHealthEndpoint -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add main.go main_test.go
git commit -m "feat: add health endpoint and server scaffolding"
```

### Task 8: Document upload handler — full orchestration

**Files:**
- Modify: `main.go`
- Modify: `main_test.go`

- [ ] **Step 1: Write tests for document upload handler**

Append to `main_test.go`:

```go
import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

func TestHandleDocumentUpload_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, nil)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}

func TestHandleDocumentUpload_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/documents", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandleDocumentUpload_SHA256Mismatch(t *testing.T) {
	docReq := validRequest()
	docReq.SHA256Hash = "0000000000000000000000000000000000000000000000000000000000000000"
	body, _ := json.Marshal(docReq)
	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "SHA256") {
		t.Fatalf("expected SHA256 error, got: %s", w.Body.String())
	}
}

func TestHandleDocumentUpload_Success(t *testing.T) {
	// Build a mock Paperless server that handles all the calls
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Dedup check — tag search returns no results
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "tags") {
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		// Entity search — always not found
		if r.Method == http.MethodGet && !strings.Contains(r.URL.Path, "custom_fields") && !strings.Contains(r.URL.Path, "documents") {
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		// Custom fields list
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "custom_fields") {
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		// Entity creation
		if r.Method == http.MethodPost && !strings.Contains(r.URL.Path, "post_document") {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": 1, "name": "created"})
			return
		}
		// Document upload
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "post_document") {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode("task-uuid-123")
			return
		}
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")

	// Build request with valid SHA256
	data := []byte("test document content")
	hash := sha256.Sum256(data)
	docReq := validRequest()
	docReq.Data = base64.StdEncoding.EncodeToString(data)
	docReq.SHA256Hash = fmt.Sprintf("%x", hash)

	body, _ := json.Marshal(docReq)
	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, client)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["task_id"] != "task-uuid-123" {
		t.Fatalf("expected task_id 'task-uuid-123', got %q", resp["task_id"])
	}
}

func TestHandleDocumentUpload_Duplicate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "tags") {
			json.NewEncoder(w).Encode(map[string]any{
				"count":   1,
				"results": []map[string]any{{"id": 55, "name": "sha256:test"}},
			})
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "documents") {
			json.NewEncoder(w).Encode(map[string]any{
				"count":   1,
				"results": []map[string]any{{"id": 100}},
			})
			return
		}
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")

	data := []byte("test")
	hash := sha256.Sum256(data)
	docReq := validRequest()
	docReq.Data = base64.StdEncoding.EncodeToString(data)
	docReq.SHA256Hash = fmt.Sprintf("%x", hash)

	body, _ := json.Marshal(docReq)
	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, client)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -run TestHandleDocumentUpload -v
```

Expected: FAIL — `handleDocumentUpload` not defined.

- [ ] **Step 3: Add handleDocumentUpload to main.go**

Append to `main.go`:

```go
import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

func writeJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func handleDocumentUpload(w http.ResponseWriter, r *http.Request, client *PaperlessClient) {
	if r.Method != http.MethodPost {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var docReq DocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&docReq); err != nil {
		writeJSONError(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if err := docReq.Validate(); err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Decode base64 data
	docData, err := base64.StdEncoding.DecodeString(docReq.Data)
	if err != nil {
		writeJSONError(w, "invalid base64 Data: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate SHA256
	computed := sha256.Sum256(docData)
	computedHex := fmt.Sprintf("%x", computed)
	if computedHex != docReq.SHA256Hash {
		writeJSONError(w, "SHA256 hash mismatch", http.StatusBadRequest)
		return
	}

	// Check for duplicates
	exists, err := client.CheckDuplicate(docReq.SHA256Hash)
	if err != nil {
		writeJSONError(w, "deduplication check failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	if exists {
		writeJSONError(w, "document with this SHA256 hash already exists", http.StatusConflict)
		return
	}

	// Resolve correspondent
	correspondentID, err := client.GetOrCreateEntity("correspondents", docReq.Correspondent, nil)
	if err != nil {
		writeJSONError(w, "resolving correspondent: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Resolve document type
	docTypeID, err := client.GetOrCreateEntity("document_types", docReq.DocumentType, nil)
	if err != nil {
		writeJSONError(w, "resolving document type: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Resolve storage path
	storagePathPattern := fmt.Sprintf("/%s/{{ created_year }}/{{ correspondent }}/{{ title }}", docReq.Recipient)
	storagePathID, err := client.GetOrCreateEntity("storage_paths", docReq.Recipient, map[string]string{
		"path": storagePathPattern,
	})
	if err != nil {
		writeJSONError(w, "resolving storage path: "+err.Error(), http.StatusBadGateway)
		return
	}

	// Resolve tags (user tags + sha256 dedup tag)
	allTags := make([]string, len(docReq.Tags)+1)
	copy(allTags, docReq.Tags)
	allTags[len(docReq.Tags)] = "sha256:" + docReq.SHA256Hash
	tagIDs := make([]int, 0, len(allTags))
	for _, tagName := range allTags {
		tagID, err := client.GetOrCreateEntity("tags", tagName, nil)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("resolving tag %q: %s", tagName, err.Error()), http.StatusBadGateway)
			return
		}
		tagIDs = append(tagIDs, tagID)
	}

	// Resolve custom fields and build values map
	type customFieldDef struct {
		Name     string
		DataType string
		Value    any
	}
	fieldDefs := []customFieldDef{
		{"DocumentLanguageCode", "string", docReq.DocumentLanguageCode},
		{"ShortSummary", "longtext", docReq.ShortSummary},
		{"LongSummary", "longtext", docReq.LongSummary},
		{"Amounts", "longtext", mustJSON(docReq.Amounts)},
		{"RecipientDetails", "longtext", docReq.RecipientDetails},
		{"CorrespondentDetails", "longtext", docReq.CorrespondentDetails},
	}

	customFields := make(map[string]any)
	for _, fd := range fieldDefs {
		fieldID, err := client.GetOrCreateCustomField(fd.Name, fd.DataType)
		if err != nil {
			writeJSONError(w, fmt.Sprintf("resolving custom field %q: %s", fd.Name, err.Error()), http.StatusBadGateway)
			return
		}
		customFields[fmt.Sprintf("%d", fieldID)] = fd.Value
	}

	// Upload document
	taskID, err := client.UploadDocument(UploadParams{
		DocumentData:     docData,
		OriginalFilename: docReq.OriginalFilename,
		Title:            docReq.ProposedFilename,
		Created:          docReq.DocumentDate,
		CorrespondentID:  correspondentID,
		DocumentTypeID:   docTypeID,
		StoragePathID:    storagePathID,
		TagIDs:           tagIDs,
		CustomFields:     customFields,
	})
	if err != nil {
		writeJSONError(w, "uploading document: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"task_id": taskID})
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
```

Note: merge the import block with the existing one at the top of `main.go`.

- [ ] **Step 4: Run all tests to verify they pass**

```bash
go test -v
```

Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add main.go main_test.go
git commit -m "feat: add document upload handler with full orchestration"
```

---

## Chunk 4: Configuration, Docker, CI/CD, and README

### Task 9: Create .env.example

**Files:**
- Create: `.env.example`

- [ ] **Step 1: Create .env.example**

```
PAPERLESS_BASE_URL=https://archive.fe83.de
PAPERLESS_API_TOKEN=your-api-token-here
PORT=8471
```

- [ ] **Step 2: Commit**

```bash
git add .env.example
git commit -m "feat: add .env.example with configuration template"
```

### Task 10: Create Dockerfile

**Files:**
- Create: `Dockerfile`

- [ ] **Step 1: Create Dockerfile**

```dockerfile
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /paperless-ngx-ingress .

FROM alpine:3.21

RUN apk --no-cache add ca-certificates
RUN adduser -D -u 1000 appuser

COPY --from=builder /paperless-ngx-ingress /usr/local/bin/paperless-ngx-ingress

USER appuser
EXPOSE 8471

ENTRYPOINT ["paperless-ngx-ingress"]
```

- [ ] **Step 2: Verify Docker build**

```bash
docker build -t paperless-ngx-ingress .
```

Expected: Build succeeds.

- [ ] **Step 3: Commit**

```bash
git add Dockerfile
git commit -m "feat: add multi-stage Dockerfile"
```

### Task 11: Create GitHub Actions workflow

**Files:**
- Create: `.github/workflows/docker-build.yml`

- [ ] **Step 1: Create workflow file**

```yaml
name: Build and Push Docker Image

on:
  push:
    branches: [main]
    tags: ['v*']
  pull_request:
    branches: [main]

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ${{ github.repository }}

jobs:
  build:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.24'

      - name: Run tests
        run: go test -v ./...

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Log in to Container Registry
        if: github.event_name != 'pull_request'
        uses: docker/login-action@v3
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Extract metadata
        id: meta
        uses: docker/metadata-action@v5
        with:
          images: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}
          tags: |
            type=raw,value=latest,enable={{is_default_branch}}
            type=semver,pattern={{version}}
            type=semver,pattern={{major}}.{{minor}}
            type=sha

      - name: Build and push
        uses: docker/build-push-action@v6
        with:
          context: .
          push: ${{ github.event_name != 'pull_request' }}
          tags: ${{ steps.meta.outputs.tags }}
          labels: ${{ steps.meta.outputs.labels }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/docker-build.yml
git commit -m "feat: add GitHub Actions workflow for Docker build"
```

### Task 12: Create README

**Files:**
- Create: `README.md`

- [ ] **Step 1: Create README.md**

````markdown
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
````

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: add README with usage instructions"
```

### Task 13: Add .gitignore

**Files:**
- Create: `.gitignore`

- [ ] **Step 1: Create .gitignore**

```
.env
paperless-ngx-ingress
```

- [ ] **Step 2: Commit**

```bash
git add .gitignore
git commit -m "chore: add .gitignore"
```

### Task 14: Final verification

- [ ] **Step 1: Run all tests**

```bash
go test -v ./...
```

Expected: ALL PASS

- [ ] **Step 2: Build binary**

```bash
go build -o paperless-ngx-ingress .
```

Expected: Builds successfully.

- [ ] **Step 3: Docker build**

```bash
docker build -t paperless-ngx-ingress .
```

Expected: Builds successfully.

- [ ] **Step 4: Verify with go vet**

```bash
go vet ./...
```

Expected: No issues.
