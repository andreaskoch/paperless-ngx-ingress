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
