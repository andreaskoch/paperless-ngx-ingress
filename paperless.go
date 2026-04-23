package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type PaperlessClient struct {
	baseURL          string
	token            string
	httpClient       *http.Client
	taskPollInterval time.Duration
}

func NewPaperlessClient(baseURL, token string) *PaperlessClient {
	return &PaperlessClient{
		baseURL:          baseURL,
		token:            token,
		httpClient:       &http.Client{},
		taskPollInterval: 1 * time.Second,
	}
}

// ErrTaskTimeout is returned by WaitForDocument when the timeout elapses
// before the task reaches a terminal state.
type ErrTaskTimeout struct{ TaskID string }

func (e *ErrTaskTimeout) Error() string {
	return fmt.Sprintf("timed out waiting for task %s", e.TaskID)
}

// ErrTaskFailed is returned by WaitForDocument when the task's status is
// FAILURE. Result carries the Paperless-supplied failure message.
type ErrTaskFailed struct {
	TaskID string
	Result string
}

func (e *ErrTaskFailed) Error() string {
	return fmt.Sprintf("task %s failed: %s", e.TaskID, e.Result)
}

// WaitForDocument polls /api/tasks/ for the given task UUID until it reaches
// a terminal state or the timeout elapses. On SUCCESS it returns the
// related_document ID. The context can be used to cancel the poll early.
func (c *PaperlessClient) WaitForDocument(ctx context.Context, taskID string, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	pollPath := fmt.Sprintf("/api/tasks/?task_id=%s", url.QueryEscape(taskID))

	for {
		done, id, err := c.checkTask(pollPath, taskID)
		if err != nil {
			return 0, err
		}
		if done {
			return id, nil
		}

		if time.Now().After(deadline) {
			return 0, &ErrTaskTimeout{TaskID: taskID}
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(c.taskPollInterval):
		}
	}
}

// checkTask performs a single poll. done=true means we got a terminal result.
func (c *PaperlessClient) checkTask(pollPath, taskID string) (done bool, docID int, err error) {
	resp, err := c.doRequest(http.MethodGet, pollPath, nil, "")
	if err != nil {
		return false, 0, fmt.Errorf("polling task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, 0, fmt.Errorf("polling task: status %d: %s", resp.StatusCode, string(body))
	}

	var tasks []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		return false, 0, fmt.Errorf("decoding task response: %w", err)
	}
	if len(tasks) == 0 {
		return false, 0, nil
	}
	task := tasks[0]
	status, _ := task["status"].(string)
	switch status {
	case "SUCCESS":
		// Paperless-ngx 2.20+ returns related_document as a string, older versions as a number.
		var relatedID int
		switch v := task["related_document"].(type) {
		case float64:
			relatedID = int(v)
		case string:
			n, err := strconv.Atoi(v)
			if err != nil {
				return false, 0, fmt.Errorf("task %s SUCCESS with non-numeric related_document %q", taskID, v)
			}
			relatedID = n
		default:
			return false, 0, fmt.Errorf("task %s SUCCESS without related_document", taskID)
		}
		return true, relatedID, nil
	case "FAILURE":
		result, _ := task["result"].(string)
		return false, 0, &ErrTaskFailed{TaskID: taskID, Result: result}
	default:
		return false, 0, nil
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

// searchEntityByName issues a case-insensitive lookup and returns the first
// result's id, plus whether any result was found.
func (c *PaperlessClient) searchEntityByName(entityType, name string) (int, bool, error) {
	searchPath := fmt.Sprintf("/api/%s/?name__iexact=%s", entityType, url.QueryEscape(name))
	resp, err := c.doRequest(http.MethodGet, searchPath, nil, "")
	if err != nil {
		return 0, false, fmt.Errorf("searching %s: %w", entityType, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, false, fmt.Errorf("searching %s: status %d: %s", entityType, resp.StatusCode, string(body))
	}

	var paginated paginatedResponse
	if err := json.NewDecoder(resp.Body).Decode(&paginated); err != nil {
		return 0, false, fmt.Errorf("decoding %s response: %w", entityType, err)
	}

	if paginated.Count == 0 || len(paginated.Results) == 0 {
		return 0, false, nil
	}
	id, ok := paginated.Results[0]["id"].(float64)
	if !ok {
		return 0, false, fmt.Errorf("invalid id type in %s response", entityType)
	}
	return int(id), true, nil
}

// isNameConflict reports whether a 400 response body looks like a uniqueness
// conflict on the "name" field (Paperless/DRF shape: {"name":["..."]}).
func isNameConflict(body []byte) bool {
	var m map[string][]string
	if err := json.Unmarshal(body, &m); err != nil {
		return false
	}
	errs, ok := m["name"]
	return ok && len(errs) > 0
}

// GetOrCreateEntity finds an entity by name or creates it. extraFields are
// included in the POST body when creating (e.g. "path" for storage_paths).
// On a 400 uniqueness conflict (concurrent create), it re-runs the search
// once and returns the winner's ID.
func (c *PaperlessClient) GetOrCreateEntity(entityType, name string, extraFields map[string]string) (int, error) {
	if id, found, err := c.searchEntityByName(entityType, name); err != nil {
		return 0, err
	} else if found {
		return id, nil
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
	resp, err := c.doRequest(http.MethodPost, createPath, bytes.NewReader(jsonBody), "application/json")
	if err != nil {
		return 0, fmt.Errorf("creating %s: %w", entityType, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var created map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			return 0, fmt.Errorf("decoding created %s: %w", entityType, err)
		}
		id, ok := created["id"].(float64)
		if !ok {
			return 0, fmt.Errorf("invalid id in created %s", entityType)
		}
		return int(id), nil
	}

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusBadRequest && isNameConflict(body) {
		if id, found, err := c.searchEntityByName(entityType, name); err == nil && found {
			return id, nil
		}
	}
	return 0, fmt.Errorf("creating %s: status %d: %s", entityType, resp.StatusCode, string(body))
}

// GetOrCreateStoragePath finds a storage_paths entity by name and ensures its
// "path" field matches the desired pattern. If found with a different path,
// it issues a PATCH to update. If not found, it creates with race-retry on
// uniqueness conflict.
func (c *PaperlessClient) GetOrCreateStoragePath(name, path string) (int, error) {
	id, existingPath, found, err := c.findStoragePath(name)
	if err != nil {
		return 0, err
	}
	if found {
		if existingPath == path {
			return id, nil
		}
		if err := c.patchStoragePath(id, path); err != nil {
			return 0, err
		}
		return id, nil
	}

	createBody, err := json.Marshal(map[string]string{"name": name, "path": path})
	if err != nil {
		return 0, fmt.Errorf("marshaling storage path body: %w", err)
	}
	resp, err := c.doRequest(http.MethodPost, "/api/storage_paths/", bytes.NewReader(createBody), "application/json")
	if err != nil {
		return 0, fmt.Errorf("creating storage path: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var created map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			return 0, fmt.Errorf("decoding created storage path: %w", err)
		}
		newID, ok := created["id"].(float64)
		if !ok {
			return 0, fmt.Errorf("invalid id in created storage path")
		}
		return int(newID), nil
	}

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusBadRequest && isNameConflict(body) {
		if raceID, _, found, err := c.findStoragePath(name); err == nil && found {
			return raceID, nil
		}
	}
	return 0, fmt.Errorf("creating storage path: status %d: %s", resp.StatusCode, string(body))
}

// findStoragePath looks up a storage_paths entity by name and returns its id
// and current path, along with whether it was found.
func (c *PaperlessClient) findStoragePath(name string) (int, string, bool, error) {
	searchPath := fmt.Sprintf("/api/storage_paths/?name__iexact=%s", url.QueryEscape(name))
	resp, err := c.doRequest(http.MethodGet, searchPath, nil, "")
	if err != nil {
		return 0, "", false, fmt.Errorf("searching storage paths: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, "", false, fmt.Errorf("searching storage paths: status %d: %s", resp.StatusCode, string(body))
	}
	var paginated paginatedResponse
	if err := json.NewDecoder(resp.Body).Decode(&paginated); err != nil {
		return 0, "", false, fmt.Errorf("decoding storage paths: %w", err)
	}
	if paginated.Count == 0 || len(paginated.Results) == 0 {
		return 0, "", false, nil
	}
	idVal, ok := paginated.Results[0]["id"].(float64)
	if !ok {
		return 0, "", false, fmt.Errorf("invalid id in storage paths response")
	}
	existingPath, _ := paginated.Results[0]["path"].(string)
	return int(idVal), existingPath, true, nil
}

// patchStoragePath updates the "path" field of an existing storage_paths entity.
func (c *PaperlessClient) patchStoragePath(id int, path string) error {
	body, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		return fmt.Errorf("marshaling storage path patch: %w", err)
	}
	patchURL := fmt.Sprintf("/api/storage_paths/%d/", id)
	resp, err := c.doRequest(http.MethodPatch, patchURL, bytes.NewReader(body), "application/json")
	if err != nil {
		return fmt.Errorf("patching storage path: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("patching storage path: status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// searchCustomFieldByName lists the first page of custom fields (up to 100)
// and returns the first case-insensitive match's id.
func (c *PaperlessClient) searchCustomFieldByName(name string) (int, bool, error) {
	resp, err := c.doRequest(http.MethodGet, "/api/custom_fields/?page_size=100", nil, "")
	if err != nil {
		return 0, false, fmt.Errorf("listing custom fields: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, false, fmt.Errorf("listing custom fields: status %d: %s", resp.StatusCode, string(body))
	}

	var paginated paginatedResponse
	if err := json.NewDecoder(resp.Body).Decode(&paginated); err != nil {
		return 0, false, fmt.Errorf("decoding custom fields: %w", err)
	}

	for _, field := range paginated.Results {
		if fieldName, ok := field["name"].(string); ok && strings.EqualFold(fieldName, name) {
			id, ok := field["id"].(float64)
			if !ok {
				return 0, false, fmt.Errorf("invalid id for custom field %s", name)
			}
			return int(id), true, nil
		}
	}
	return 0, false, nil
}

// GetOrCreateCustomField finds a custom field by name (case-insensitive) or
// creates it with the given data type. On a 400 uniqueness conflict from a
// concurrent create, it re-runs the search once and returns the winner's ID.
func (c *PaperlessClient) GetOrCreateCustomField(name, dataType string) (int, error) {
	if id, found, err := c.searchCustomFieldByName(name); err != nil {
		return 0, err
	} else if found {
		return id, nil
	}

	createBody := map[string]string{
		"name":      name,
		"data_type": dataType,
	}
	jsonBody, err := json.Marshal(createBody)
	if err != nil {
		return 0, fmt.Errorf("marshaling custom field: %w", err)
	}

	resp, err := c.doRequest(http.MethodPost, "/api/custom_fields/", bytes.NewReader(jsonBody), "application/json")
	if err != nil {
		return 0, fmt.Errorf("creating custom field: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var created map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			return 0, fmt.Errorf("decoding created custom field: %w", err)
		}
		id, ok := created["id"].(float64)
		if !ok {
			return 0, fmt.Errorf("invalid id in created custom field")
		}
		return int(id), nil
	}

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusBadRequest && isNameConflict(body) {
		if id, found, err := c.searchCustomFieldByName(name); err == nil && found {
			return id, nil
		}
	}
	return 0, fmt.Errorf("creating custom field: status %d: %s", resp.StatusCode, string(body))
}

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
	idVal, ok := paginated.Results[0]["id"].(float64)
	if !ok {
		return false, fmt.Errorf("invalid id type in dedup tag response")
	}
	tagID := int(idVal)
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
