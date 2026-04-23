package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func TestGetOrCreateEntity_RaceRetry(t *testing.T) {
	var (
		getCount  int
		postCount int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			getCount++
			if getCount == 1 {
				// initial lookup: not yet present
				json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
				return
			}
			// re-search after conflict: now present (created by concurrent request)
			json.NewEncoder(w).Encode(map[string]any{
				"count":   1,
				"results": []map[string]any{{"id": 77, "name": "Test Corp"}},
			})
			return
		}
		if r.Method == http.MethodPost {
			postCount++
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"name":["correspondent with this name already exists."]}`))
			return
		}
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	id, err := client.GetOrCreateEntity("correspondents", "Test Corp", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 77 {
		t.Fatalf("expected id 77 after race-retry, got %d", id)
	}
	if getCount != 2 || postCount != 1 {
		t.Fatalf("expected 2 GETs + 1 POST, got %d GETs + %d POSTs", getCount, postCount)
	}
}

func TestGetOrCreateEntity_400NotNameConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
			return
		}
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"detail":"some other validation error"}`))
			return
		}
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	_, err := client.GetOrCreateEntity("correspondents", "Test Corp", nil)
	if err == nil {
		t.Fatal("expected error for non-name 400, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected error to mention 400, got: %v", err)
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

func TestGetOrCreateCustomField_CaseInsensitive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("expected GET only (existing match), got %s", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"count": 1,
			"results": []map[string]any{
				{"id": 7, "name": "ShortSummary", "data_type": "longtext"},
			},
		})
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	id, err := client.GetOrCreateCustomField("shortsummary", "longtext")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 7 {
		t.Fatalf("expected id 7, got %d", id)
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

func TestGetOrCreateCustomField_RaceRetry(t *testing.T) {
	var getCount, postCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			getCount++
			if getCount == 1 {
				json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []map[string]any{
					{"id": 42, "name": "Amounts", "data_type": "longtext"},
				},
			})
			return
		}
		if r.Method == http.MethodPost {
			postCount++
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"name":["custom field with this name already exists."]}`))
			return
		}
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	id, err := client.GetOrCreateCustomField("Amounts", "longtext")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 42 {
		t.Fatalf("expected id 42 after race-retry, got %d", id)
	}
	if getCount != 2 || postCount != 1 {
		t.Fatalf("expected 2 GETs + 1 POST, got %d GETs + %d POSTs", getCount, postCount)
	}
}

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

func TestGetOrCreateStoragePath_PathMatches(t *testing.T) {
	var postCalled, patchCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []map[string]any{
					{"id": 10, "name": "MyCo", "path": "/MyCo/{{ created_year }}/{{ correspondent }}/{{ title }}"},
				},
			})
		case http.MethodPost:
			postCalled = true
			w.WriteHeader(http.StatusCreated)
		case http.MethodPatch:
			patchCalled = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	id, err := client.GetOrCreateStoragePath("MyCo", "/MyCo/{{ created_year }}/{{ correspondent }}/{{ title }}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 10 {
		t.Fatalf("expected id 10, got %d", id)
	}
	if postCalled {
		t.Error("expected no POST when path matches")
	}
	if patchCalled {
		t.Error("expected no PATCH when path matches")
	}
}

func TestGetOrCreateStoragePath_PathDiverges(t *testing.T) {
	var patchedBody map[string]any
	var patchedURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []map[string]any{
					{"id": 11, "name": "MyCo", "path": "/old/path"},
				},
			})
		case http.MethodPatch:
			patchedURL = r.URL.Path
			json.NewDecoder(r.Body).Decode(&patchedBody)
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{"id": 11, "name": "MyCo", "path": patchedBody["path"]})
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	desired := "/MyCo/{{ created_year }}/{{ correspondent }}/{{ title }}"
	id, err := client.GetOrCreateStoragePath("MyCo", desired)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 11 {
		t.Fatalf("expected id 11, got %d", id)
	}
	if patchedBody["path"] != desired {
		t.Errorf("expected PATCH path=%q, got %v", desired, patchedBody["path"])
	}
	if patchedURL != "/api/storage_paths/11/" {
		t.Errorf("expected PATCH on /api/storage_paths/11/, got %s", patchedURL)
	}
}

func TestGetOrCreateStoragePath_NotFound(t *testing.T) {
	var postBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
		case http.MethodPost:
			json.NewDecoder(r.Body).Decode(&postBody)
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": 99, "name": postBody["name"], "path": postBody["path"]})
		}
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	desired := "/NewCo/{{ created_year }}/{{ correspondent }}/{{ title }}"
	id, err := client.GetOrCreateStoragePath("NewCo", desired)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 99 {
		t.Fatalf("expected id 99, got %d", id)
	}
	if postBody["name"] != "NewCo" {
		t.Errorf("expected POST name=NewCo, got %v", postBody["name"])
	}
	if postBody["path"] != desired {
		t.Errorf("expected POST path=%q, got %v", desired, postBody["path"])
	}
}

func TestGetOrCreateStoragePath_RaceRetry(t *testing.T) {
	var getCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			getCount++
			if getCount == 1 {
				json.NewEncoder(w).Encode(map[string]any{"count": 0, "results": []map[string]any{}})
				return
			}
			// re-search after conflict — now present with the already-desired path
			json.NewEncoder(w).Encode(map[string]any{
				"count": 1,
				"results": []map[string]any{
					{"id": 33, "name": "RaceCo", "path": "/RaceCo/x"},
				},
			})
		case http.MethodPost:
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"name":["storage path with this name already exists."]}`))
		}
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	id, err := client.GetOrCreateStoragePath("RaceCo", "/RaceCo/x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 33 {
		t.Fatalf("expected id 33 after race-retry, got %d", id)
	}
}

func TestWaitForDocument_Success(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			json.NewEncoder(w).Encode([]map[string]any{
				{"task_id": "uuid-1", "status": "STARTED"},
			})
			return
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"task_id": "uuid-1", "status": "SUCCESS", "related_document": 777},
		})
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	client.taskPollInterval = 5 * time.Millisecond

	id, err := client.WaitForDocument(context.Background(), "uuid-1", 1*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 777 {
		t.Errorf("expected doc ID 777, got %d", id)
	}
	if calls < 2 {
		t.Errorf("expected at least 2 poll calls, got %d", calls)
	}
}

// Paperless-ngx 2.20+ returns related_document as a JSON string; earlier
// versions returned it as a JSON number. Both must work.
func TestWaitForDocument_SuccessWithStringRelatedDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"task_id": "uuid-str", "status": "SUCCESS", "related_document": "4"},
		})
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	client.taskPollInterval = 5 * time.Millisecond

	id, err := client.WaitForDocument(context.Background(), "uuid-str", 1*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != 4 {
		t.Errorf("expected doc ID 4, got %d", id)
	}
}

func TestWaitForDocument_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"task_id": "uuid-2", "status": "FAILURE", "result": "consumer couldn't parse PDF"},
		})
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	client.taskPollInterval = 5 * time.Millisecond

	_, err := client.WaitForDocument(context.Background(), "uuid-2", 1*time.Second)
	if err == nil {
		t.Fatal("expected ErrTaskFailed, got nil")
	}
	var taskErr *ErrTaskFailed
	if !errors.As(err, &taskErr) {
		t.Fatalf("expected *ErrTaskFailed, got %T: %v", err, err)
	}
	if taskErr.TaskID != "uuid-2" {
		t.Errorf("expected TaskID=uuid-2, got %q", taskErr.TaskID)
	}
	if taskErr.Result != "consumer couldn't parse PDF" {
		t.Errorf("expected Result passed through, got %q", taskErr.Result)
	}
}

func TestWaitForDocument_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]any{
			{"task_id": "uuid-3", "status": "PENDING"},
		})
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	client.taskPollInterval = 5 * time.Millisecond

	_, err := client.WaitForDocument(context.Background(), "uuid-3", 30*time.Millisecond)
	if err == nil {
		t.Fatal("expected ErrTaskTimeout, got nil")
	}
	var toErr *ErrTaskTimeout
	if !errors.As(err, &toErr) {
		t.Fatalf("expected *ErrTaskTimeout, got %T: %v", err, err)
	}
	if toErr.TaskID != "uuid-3" {
		t.Errorf("expected TaskID=uuid-3, got %q", toErr.TaskID)
	}
}

func TestCheckDuplicate_NoDuplicate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"count":   0,
			"results": []map[string]any{},
		})
	}))
	defer server.Close()

	client := NewPaperlessClient(server.URL, "test-token")
	docID, found, err := client.CheckDuplicate("abc123hash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected no duplicate")
	}
	if docID != 0 {
		t.Errorf("expected docID=0, got %d", docID)
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
	docID, found, err := client.CheckDuplicate("abc123hash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected duplicate to be found")
	}
	if docID != 100 {
		t.Errorf("expected docID=100, got %d", docID)
	}
}
