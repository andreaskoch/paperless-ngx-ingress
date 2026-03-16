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
