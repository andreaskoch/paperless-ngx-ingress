package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

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
	if r.Method != http.MethodGet {
		writeJSONError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

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

	// Fill date defaults before validation
	docReq.FillDateDefaults(time.Now())

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

	// Compute SHA256 — use provided hash for validation, or calculate if empty
	computed := sha256.Sum256(docData)
	computedHex := fmt.Sprintf("%x", computed)
	if docReq.SHA256Hash == "" {
		docReq.SHA256Hash = computedHex
	} else if computedHex != docReq.SHA256Hash {
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
