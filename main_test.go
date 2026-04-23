package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testTaskTimeout is a short timeout used in tests where the handler polls
// the task endpoint. Individual tests may shorten it further via the client's
// taskPollInterval.
const testTaskTimeout = 500 * time.Millisecond

func TestBuildDocumentResponse(t *testing.T) {
	req := validRequest()
	req.Data = "should-not-appear"
	req.SHA256Hash = "abc123"

	resp := buildDocumentResponse(req, "task-uuid-xyz")

	if resp.TaskID != "task-uuid-xyz" {
		t.Errorf("expected TaskID=task-uuid-xyz, got %q", resp.TaskID)
	}
	if resp.SHA256Hash != "abc123" {
		t.Errorf("expected SHA256Hash=abc123, got %q", resp.SHA256Hash)
	}
	if resp.Correspondent != req.Correspondent {
		t.Errorf("expected Correspondent=%q, got %q", req.Correspondent, resp.Correspondent)
	}
	if resp.DocumentURL != "" {
		t.Errorf("expected DocumentURL empty until set by handler, got %q", resp.DocumentURL)
	}
	if resp.TaskURL != "" {
		t.Errorf("expected TaskURL empty until set by handler, got %q", resp.TaskURL)
	}
	// Data must not be exposed in any field.
	raw, _ := json.Marshal(resp)
	if strings.Contains(string(raw), "should-not-appear") {
		t.Errorf("response leaked Data field: %s", string(raw))
	}
}

func TestBuildDocumentResponse_StripsSha256Tag(t *testing.T) {
	req := validRequest()
	req.Tags = []string{"invoice", "2026", "sha256:abc"}

	resp := buildDocumentResponse(req, "t")

	for _, tag := range resp.Tags {
		if strings.HasPrefix(tag, "sha256:") {
			t.Errorf("response Tags should not contain sha256 tag, got %v", resp.Tags)
		}
	}
	if len(resp.Tags) != 2 {
		t.Errorf("expected 2 user tags, got %d: %v", len(resp.Tags), resp.Tags)
	}
}

func TestReadTaskTimeout(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Duration
	}{
		{"", 120 * time.Second},
		{"30", 30 * time.Second},
		{"abc", 120 * time.Second},
		{"-5", 120 * time.Second},
		{"0", 120 * time.Second},
	}
	for _, c := range cases {
		if got := readTaskTimeout(c.raw); got != c.want {
			t.Errorf("readTaskTimeout(%q) = %s, want %s", c.raw, got, c.want)
		}
	}
}

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

func decodeErrResponse(t *testing.T, body string) ErrorResponse {
	t.Helper()
	var er ErrorResponse
	if err := json.Unmarshal([]byte(body), &er); err != nil {
		t.Fatalf("decoding error response %q: %v", body, err)
	}
	return er
}

func TestHandleDocumentUpload_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/documents", nil)
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, nil, testTaskTimeout)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
	er := decodeErrResponse(t, w.Body.String())
	if er.Code != "method_not_allowed" {
		t.Errorf("expected Code=method_not_allowed, got %q", er.Code)
	}
}

func TestHandleDocumentUpload_InvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/documents", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, nil, testTaskTimeout)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	er := decodeErrResponse(t, w.Body.String())
	if er.Code != "invalid_json" {
		t.Errorf("expected Code=invalid_json, got %q", er.Code)
	}
}

func TestHandleDocumentUpload_SHA256Mismatch(t *testing.T) {
	docReq := validRequest()
	docReq.SHA256Hash = "0000000000000000000000000000000000000000000000000000000000000000"
	body, _ := json.Marshal(docReq)
	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, nil, testTaskTimeout)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	er := decodeErrResponse(t, w.Body.String())
	if er.Code != "sha256_mismatch" {
		t.Errorf("expected Code=sha256_mismatch, got %q", er.Code)
	}
	if er.Details["Expected"] == nil || er.Details["Got"] == nil {
		t.Errorf("expected Details.Expected and Details.Got, got %v", er.Details)
	}
	if er.Details["Got"] != docReq.SHA256Hash {
		t.Errorf("expected Details.Got=%q, got %v", docReq.SHA256Hash, er.Details["Got"])
	}
}

func TestHandleDocumentUpload_ValidationErrorDetails(t *testing.T) {
	docReq := validRequest()
	docReq.Correspondent = ""
	docReq.ShortSummary = ""
	docReq.LongSummary = ""
	body, _ := json.Marshal(docReq)
	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, nil, testTaskTimeout)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	er := decodeErrResponse(t, w.Body.String())
	if er.Code != "validation_failed" {
		t.Errorf("expected Code=validation_failed, got %q", er.Code)
	}
	missingRaw, ok := er.Details["MissingFields"].([]any)
	if !ok {
		t.Fatalf("expected Details.MissingFields as list, got %T: %v", er.Details["MissingFields"], er.Details)
	}
	missing := make(map[string]bool, len(missingRaw))
	for _, v := range missingRaw {
		if s, ok := v.(string); ok {
			missing[s] = true
		}
	}
	for _, want := range []string{"Correspondent", "ShortSummary", "LongSummary"} {
		if !missing[want] {
			t.Errorf("expected MissingFields to contain %q, got %v", want, missingRaw)
		}
	}
}

// mockPaperlessServer returns a handler covering the full happy-path Paperless API:
// entity lookups/creates, duplicate check, post_document upload, and task polling.
// taskStatus controls the /api/tasks/ response ("SUCCESS" immediately, "PENDING"
// forever, "FAILURE" with result).
func mockPaperlessServer(t *testing.T, taskStatus string, documentID int, failureResult string, returnedTaskID string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Task polling
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/tasks/") {
			resp := map[string]any{"task_id": returnedTaskID, "status": taskStatus}
			switch taskStatus {
			case "SUCCESS":
				resp["related_document"] = documentID
			case "FAILURE":
				resp["result"] = failureResult
			}
			json.NewEncoder(w).Encode([]map[string]any{resp})
			return
		}
		// Dedup document search — count=0 means no duplicate
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/documents/") {
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		// Entity search (tags, correspondents, document_types, storage_paths) — not found
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/") {
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		// Document upload
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "post_document") {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(returnedTaskID)
			return
		}
		// Entity creation (tags, correspondents, types, storage_paths, custom_fields)
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": 1, "name": "created"})
			return
		}
	}))
}

func decodeDocResponse(t *testing.T, body string) DocumentResponse {
	t.Helper()
	var dr DocumentResponse
	if err := json.Unmarshal([]byte(body), &dr); err != nil {
		t.Fatalf("decoding response %q: %v", body, err)
	}
	return dr
}

func TestHandleDocumentUpload_Success201(t *testing.T) {
	server := mockPaperlessServer(t, "SUCCESS", 42, "", "task-uuid-123")
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	client.taskPollInterval = 5 * time.Millisecond

	data := []byte("test document content")
	hash := sha256.Sum256(data)
	docReq := validRequest()
	docReq.Data = base64.StdEncoding.EncodeToString(data)
	docReq.SHA256Hash = fmt.Sprintf("%x", hash)
	docReq.Tags = []string{"invoice", "2026"}

	body, _ := json.Marshal(docReq)
	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, client, testTaskTimeout)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	dr := decodeDocResponse(t, w.Body.String())
	if dr.TaskID != "task-uuid-123" {
		t.Errorf("expected TaskID=task-uuid-123, got %q", dr.TaskID)
	}
	wantURL := server.URL + "/documents/42/"
	if dr.DocumentURL != wantURL {
		t.Errorf("expected DocumentURL=%q, got %q", wantURL, dr.DocumentURL)
	}
	if dr.TaskURL != "" {
		t.Errorf("expected TaskURL empty on 201, got %q", dr.TaskURL)
	}
	if dr.Correspondent != docReq.Correspondent {
		t.Errorf("expected mirrored Correspondent=%q, got %q", docReq.Correspondent, dr.Correspondent)
	}
	for _, tag := range dr.Tags {
		if strings.HasPrefix(tag, "sha256:") {
			t.Errorf("response Tags should not contain sha256 tag, got %v", dr.Tags)
		}
	}
	if len(dr.Tags) != 2 {
		t.Errorf("expected 2 user tags in response, got %d: %v", len(dr.Tags), dr.Tags)
	}
	// Data must never leak
	if strings.Contains(w.Body.String(), docReq.Data) {
		t.Error("response leaked base64 Data")
	}
}

// Paperless appends the file extension when writing to disk, so the "title"
// form field sent in the upload must not already carry one — otherwise the
// stored filename ends in "<name>.pdf.pdf".
func TestHandleDocumentUpload_TitleStripsExtension(t *testing.T) {
	var uploadedTitle string
	var uploadedCustomFields string
	customFieldsByName := map[string]int{}
	var nextCustomFieldID = 100
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "post_document") {
			fields := parseMultipartFields(t, r)
			uploadedTitle = fields["title"]
			uploadedCustomFields = fields["custom_fields"]
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode("task-uuid-ext")
			return
		}
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/tasks/") {
			json.NewEncoder(w).Encode([]map[string]any{
				{"task_id": "task-uuid-ext", "status": "SUCCESS", "related_document": 1},
			})
			return
		}
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/custom_fields/") {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			name, _ := body["name"].(string)
			id := nextCustomFieldID
			nextCustomFieldID++
			customFieldsByName[name] = id
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": id, "name": name})
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id": 1, "name": "created"})
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	client.taskPollInterval = 5 * time.Millisecond

	data := []byte("ext-stripping test")
	hash := sha256.Sum256(data)
	docReq := validRequest()
	docReq.Data = base64.StdEncoding.EncodeToString(data)
	docReq.SHA256Hash = fmt.Sprintf("%x", hash)
	docReq.ProposedFilename = "2026-04-02_IHK_Bremen_Beitragsbescheid_2026_wambo.pdf"

	body, _ := json.Marshal(docReq)
	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, client, testTaskTimeout)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	wantTitle := "2026-04-02_IHK_Bremen_Beitragsbescheid_2026_wambo"
	if uploadedTitle != wantTitle {
		t.Errorf("expected title %q, got %q", wantTitle, uploadedTitle)
	}
	if strings.HasSuffix(uploadedTitle, ".pdf") {
		t.Errorf("title should not retain extension, got %q", uploadedTitle)
	}
	// Recipient must be in the custom_fields JSON, under the field ID assigned
	// to the "Recipient" custom field, so the storage-path template can resolve
	// {{ custom_fields|get_cf_value("Recipient") }}.
	recipientID, ok := customFieldsByName["Recipient"]
	if !ok {
		t.Fatalf("Recipient custom field was not created; created fields: %v", customFieldsByName)
	}
	var cf map[string]any
	if err := json.Unmarshal([]byte(uploadedCustomFields), &cf); err != nil {
		t.Fatalf("parsing custom_fields JSON %q: %v", uploadedCustomFields, err)
	}
	if got := cf[fmt.Sprintf("%d", recipientID)]; got != "My Company" {
		t.Errorf("expected custom_fields[%d]=\"My Company\", got %v (full: %v)", recipientID, got, cf)
	}
}

// parseMultipartFields reads the non-file form fields from a multipart POST.
func parseMultipartFields(t *testing.T, r *http.Request) map[string]string {
	t.Helper()
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parsing content-type: %v", err)
	}
	mr := multipart.NewReader(r.Body, params["boundary"])
	out := map[string]string{}
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		if part.FileName() != "" {
			continue
		}
		buf := new(bytes.Buffer)
		buf.ReadFrom(part)
		out[part.FormName()] = buf.String()
	}
	return out
}

func TestHandleDocumentUpload_EmptySHA256(t *testing.T) {
	server := mockPaperlessServer(t, "SUCCESS", 1, "", "task-uuid-456")
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	client.taskPollInterval = 5 * time.Millisecond

	data := []byte("test document content")
	docReq := validRequest()
	docReq.Data = base64.StdEncoding.EncodeToString(data)
	docReq.SHA256Hash = ""

	body, _ := json.Marshal(docReq)
	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, client, testTaskTimeout)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	dr := decodeDocResponse(t, w.Body.String())
	computed := fmt.Sprintf("%x", sha256.Sum256(data))
	if dr.SHA256Hash != computed {
		t.Errorf("expected response SHA256Hash=%q (computed), got %q", computed, dr.SHA256Hash)
	}
}

func TestHandleDocumentUpload_TaskTimeout202(t *testing.T) {
	server := mockPaperlessServer(t, "PENDING", 0, "", "task-timeout-uuid")
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	client.taskPollInterval = 5 * time.Millisecond

	data := []byte("body")
	hash := sha256.Sum256(data)
	docReq := validRequest()
	docReq.Data = base64.StdEncoding.EncodeToString(data)
	docReq.SHA256Hash = fmt.Sprintf("%x", hash)

	body, _ := json.Marshal(docReq)
	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, client, 30*time.Millisecond)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	dr := decodeDocResponse(t, w.Body.String())
	if dr.DocumentURL != "" {
		t.Errorf("expected DocumentURL empty on 202, got %q", dr.DocumentURL)
	}
	if !strings.Contains(dr.TaskURL, "task-timeout-uuid") {
		t.Errorf("expected TaskURL to contain task UUID, got %q", dr.TaskURL)
	}
	if !strings.HasPrefix(dr.TaskURL, server.URL+"/api/tasks/") {
		t.Errorf("expected TaskURL to point at /api/tasks/, got %q", dr.TaskURL)
	}
}

func TestHandleDocumentUpload_TaskFailed502(t *testing.T) {
	server := mockPaperlessServer(t, "FAILURE", 0, "consumer couldn't parse PDF", "task-fail-uuid")
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	client.taskPollInterval = 5 * time.Millisecond

	data := []byte("body")
	hash := sha256.Sum256(data)
	docReq := validRequest()
	docReq.Data = base64.StdEncoding.EncodeToString(data)
	docReq.SHA256Hash = fmt.Sprintf("%x", hash)

	body, _ := json.Marshal(docReq)
	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, client, testTaskTimeout)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", w.Code, w.Body.String())
	}
	er := decodeErrResponse(t, w.Body.String())
	if er.Code != "paperless_task_failed" {
		t.Errorf("expected Code=paperless_task_failed, got %q", er.Code)
	}
	if er.Details["TaskID"] != "task-fail-uuid" {
		t.Errorf("expected Details.TaskID=task-fail-uuid, got %v", er.Details["TaskID"])
	}
	if er.Details["Result"] != "consumer couldn't parse PDF" {
		t.Errorf("expected Details.Result to pass through, got %v", er.Details["Result"])
	}
}

func TestHandleDocumentUpload_NormalizesAndDedupesInputs(t *testing.T) {
	var (
		correspondentSearches []string
		storagePathSearches   []string
		tagPosts              []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/tasks/") {
			json.NewEncoder(w).Encode([]map[string]any{
				{"task_id": "task-uuid-dedup", "status": "SUCCESS", "related_document": 5},
			})
			return
		}
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/documents/") {
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/correspondents/") {
			correspondentSearches = append(correspondentSearches, r.URL.Query().Get("name__iexact"))
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/storage_paths/") {
			storagePathSearches = append(storagePathSearches, r.URL.Query().Get("name__iexact"))
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/") {
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/tags/") {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if name, ok := body["name"].(string); ok {
				tagPosts = append(tagPosts, name)
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": 1, "name": body["name"]})
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "post_document") {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode("task-uuid-dedup")
			return
		}
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": 1, "name": "created"})
			return
		}
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	client.taskPollInterval = 5 * time.Millisecond

	data := []byte("content for normalization test")
	hash := sha256.Sum256(data)
	docReq := validRequest()
	docReq.Data = base64.StdEncoding.EncodeToString(data)
	docReq.SHA256Hash = fmt.Sprintf("%x", hash)
	docReq.Correspondent = "  Test   Corp  "
	docReq.Recipient = "  My  Company  "
	docReq.Tags = []string{"invoice", "Invoice", " invoice ", "2026", "", "   ", "2026"}

	body, _ := json.Marshal(docReq)
	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, client, testTaskTimeout)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	if len(correspondentSearches) != 1 || correspondentSearches[0] != "Test Corp" {
		t.Errorf("expected correspondent search with normalized 'Test Corp', got %v", correspondentSearches)
	}
	if len(storagePathSearches) != 1 || storagePathSearches[0] != "Default" {
		t.Errorf("expected storage path search with name 'Default', got %v", storagePathSearches)
	}
	expectedTagPosts := map[string]bool{
		"invoice": true,
		"2026":    true,
		"sha256:" + docReq.SHA256Hash: true,
	}
	if len(tagPosts) != len(expectedTagPosts) {
		t.Errorf("expected %d tag creations, got %d: %v", len(expectedTagPosts), len(tagPosts), tagPosts)
	}
	for _, p := range tagPosts {
		if !expectedTagPosts[p] {
			t.Errorf("unexpected tag POSTed: %q", p)
		}
	}

	// Response mirrors the cleaned input (normalized values, deduped tags, no sha256 tag).
	dr := decodeDocResponse(t, w.Body.String())
	if dr.Correspondent != "Test Corp" {
		t.Errorf("expected response Correspondent='Test Corp' (normalized), got %q", dr.Correspondent)
	}
	if dr.Recipient != "My Company" {
		t.Errorf("expected response Recipient='My Company' (normalized), got %q", dr.Recipient)
	}
	wantTags := map[string]bool{"invoice": true, "2026": true}
	if len(dr.Tags) != len(wantTags) {
		t.Errorf("expected %d response Tags, got %d: %v", len(wantTags), len(dr.Tags), dr.Tags)
	}
	for _, tag := range dr.Tags {
		if !wantTags[tag] {
			t.Errorf("unexpected response tag: %q", tag)
		}
	}
}

func TestHandleDocumentUpload_DuplicateReturns200(t *testing.T) {
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
				"results": []map[string]any{{"id": 42}},
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
	docReq.Correspondent = "  Test   Corp  " // verify normalization still echoes

	body, _ := json.Marshal(docReq)
	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, client, testTaskTimeout)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	dr := decodeDocResponse(t, w.Body.String())
	wantURL := server.URL + "/documents/42/"
	if dr.DocumentURL != wantURL {
		t.Errorf("expected DocumentURL=%q, got %q", wantURL, dr.DocumentURL)
	}
	if dr.TaskURL != "" {
		t.Errorf("expected TaskURL empty on 200, got %q", dr.TaskURL)
	}
	if dr.Correspondent != "Test Corp" {
		t.Errorf("expected normalized Correspondent='Test Corp', got %q", dr.Correspondent)
	}
	// TaskID is omitempty and must not appear in the wire body on 200 responses.
	if strings.Contains(w.Body.String(), "\"TaskID\"") {
		t.Errorf("expected TaskID to be omitted on 200 duplicate, body: %s", w.Body.String())
	}
}

func TestHandleDocumentUpload_DuplicateDoesNotUpload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The duplicate check touches /api/tags/ and /api/documents/. Any other
		// endpoint (correspondents, document_types, storage_paths, post_document,
		// custom_fields, tasks) would mean the handler ran past the short-circuit.
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/api/tags/"):
			json.NewEncoder(w).Encode(map[string]any{
				"count":   1,
				"results": []map[string]any{{"id": 55, "name": "sha256:test"}},
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/api/documents/"):
			json.NewEncoder(w).Encode(map[string]any{
				"count":   1,
				"results": []map[string]any{{"id": 7}},
			})
		default:
			t.Errorf("unexpected %s %s — handler did not short-circuit on duplicate", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
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
	handleDocumentUpload(w, req, client, testTaskTimeout)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
