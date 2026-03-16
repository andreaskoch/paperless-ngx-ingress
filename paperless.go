package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
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
