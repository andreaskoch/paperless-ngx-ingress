package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
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

func TestHandleDocumentUpload_EmptySHA256(t *testing.T) {
	// Same mock server as success test
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "tags") {
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		if r.Method == http.MethodGet && !strings.Contains(r.URL.Path, "custom_fields") && !strings.Contains(r.URL.Path, "documents") {
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "custom_fields") {
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		if r.Method == http.MethodPost && !strings.Contains(r.URL.Path, "post_document") {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": 1, "name": "created"})
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "post_document") {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode("task-uuid-456")
			return
		}
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")

	data := []byte("test document content")
	docReq := validRequest()
	docReq.Data = base64.StdEncoding.EncodeToString(data)
	docReq.SHA256Hash = "" // empty — should be auto-calculated

	body, _ := json.Marshal(docReq)
	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, client)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleDocumentUpload_NormalizesAndDedupesInputs(t *testing.T) {
	var (
		correspondentSearches []string
		tagSearches           []string
		storagePathSearches   []string
		tagPosts              []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Dedup check — the sha256 tag lookup with count=0 so we don't short-circuit as duplicate document
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/api/tags/") {
			q := r.URL.Query().Get("name__iexact")
			tagSearches = append(tagSearches, q)
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/api/correspondents/") {
			correspondentSearches = append(correspondentSearches, r.URL.Query().Get("name__iexact"))
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/api/document_types/") {
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/api/storage_paths/") {
			storagePathSearches = append(storagePathSearches, r.URL.Query().Get("name__iexact"))
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/api/custom_fields/") {
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/api/tags/") {
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

	data := []byte("content for normalization test")
	hash := sha256.Sum256(data)
	docReq := validRequest()
	docReq.Data = base64.StdEncoding.EncodeToString(data)
	docReq.SHA256Hash = fmt.Sprintf("%x", hash)
	docReq.Correspondent = "  Test   Corp  "
	docReq.Recipient = "  My  Company  "
	// duplicate tags: exact, case-variant, whitespace-variant, empty
	docReq.Tags = []string{"invoice", "Invoice", " invoice ", "2026", "", "   ", "2026"}

	body, _ := json.Marshal(docReq)
	req := httptest.NewRequest(http.MethodPost, "/api/documents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handleDocumentUpload(w, req, client)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Correspondent must be normalized before lookup.
	if len(correspondentSearches) != 1 || correspondentSearches[0] != "Test Corp" {
		t.Errorf("expected correspondent search with normalized 'Test Corp', got %v", correspondentSearches)
	}
	// Storage path name must be normalized.
	if len(storagePathSearches) != 1 || storagePathSearches[0] != "My Company" {
		t.Errorf("expected storage path search with normalized 'My Company', got %v", storagePathSearches)
	}
	// Tags: exactly 3 distinct user tags should be POSTed ("invoice" first form + "2026") plus the sha256 tag = 3 creates.
	// "invoice", "Invoice", " invoice " should collapse to one; both "2026" should collapse to one; empty/whitespace dropped.
	expectedTagPosts := map[string]bool{
		"invoice":          true,
		"2026":             true,
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
