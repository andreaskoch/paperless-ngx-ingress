package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// defaultStoragePathName is the single shared Paperless storage_paths entity
// that every document is filed under. Per-recipient branching is expressed in
// the path template below, via the Recipient custom field.
const defaultStoragePathName = "Default"

// defaultStoragePathPattern is the Paperless storage path template applied to
// all documents. Recipient is referenced through the Recipient custom field
// because Paperless has no built-in {{ Recipient }} placeholder.
const defaultStoragePathPattern = `/{{ custom_fields|get_cf_value("Recipient") }}/{{ created_year }}/{{ document_type }}/{{ created_year }}/{{ correspondent }}/{{ title }}`

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

	taskTimeout := readTaskTimeout(os.Getenv("PAPERLESS_TASK_TIMEOUT_SECONDS"))

	client := NewPaperlessClient(baseURL, token)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/api/documents", func(w http.ResponseWriter, r *http.Request) {
		handleDocumentUpload(w, r, client, taskTimeout)
	})

	log.Printf("Starting server on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

// readTaskTimeout parses the PAPERLESS_TASK_TIMEOUT_SECONDS value (integer
// seconds, must be positive). On parse error, negative, or zero, it logs a
// warning and falls back to 120s.
func readTaskTimeout(raw string) time.Duration {
	const defaultTimeout = 120 * time.Second
	if raw == "" {
		return defaultTimeout
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		log.Printf("PAPERLESS_TASK_TIMEOUT_SECONDS=%q invalid; using default %s", raw, defaultTimeout)
		return defaultTimeout
	}
	return time.Duration(n) * time.Second
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// writeError writes a structured error response. details may be nil.
func writeError(w http.ResponseWriter, statusCode int, code, message string, details map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(ErrorResponse{Code: code, Error: message, Details: details})
}

// writeJSON writes any payload with the given status code.
func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(payload)
}

// paperlessErr maps a Paperless-side failure to a structured 502 response.
func paperlessErr(w http.ResponseWriter, stage string, err error) {
	writeError(w, http.StatusBadGateway, "paperless_error", fmt.Sprintf("%s: %s", stage, err.Error()), map[string]any{
		"Stage":   stage,
		"Message": err.Error(),
	})
}

func handleDocumentUpload(w http.ResponseWriter, r *http.Request, client *PaperlessClient, taskTimeout time.Duration) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
		return
	}

	var docReq DocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&docReq); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid JSON: "+err.Error(), nil)
		return
	}

	docReq.FillDateDefaults(time.Now())

	// Normalize entity-name fields before validation so all-whitespace values
	// are caught by the required-field check.
	docReq.Correspondent = normalizeName(docReq.Correspondent)
	docReq.DocumentType = normalizeName(docReq.DocumentType)
	docReq.Recipient = normalizeName(docReq.Recipient)
	docReq.Tags = dedupTagNames(docReq.Tags)

	if err := docReq.Validate(); err != nil {
		var ve *ValidationError
		if errors.As(err, &ve) {
			writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), map[string]any{
				"MissingFields": ve.MissingFields,
			})
			return
		}
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		return
	}

	docData, err := base64.StdEncoding.DecodeString(docReq.Data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_base64", "invalid base64 Data: "+err.Error(), nil)
		return
	}

	computed := sha256.Sum256(docData)
	computedHex := fmt.Sprintf("%x", computed)
	if docReq.SHA256Hash == "" {
		docReq.SHA256Hash = computedHex
	} else if computedHex != docReq.SHA256Hash {
		writeError(w, http.StatusBadRequest, "sha256_mismatch", "SHA256 hash mismatch", map[string]any{
			"Expected": computedHex,
			"Got":      docReq.SHA256Hash,
		})
		return
	}

	existingID, found, err := client.CheckDuplicate(docReq.SHA256Hash)
	if err != nil {
		paperlessErr(w, "dedup_check", err)
		return
	}
	if found {
		response := buildDocumentResponse(docReq, "")
		response.DocumentURL = fmt.Sprintf("%s/documents/%d/", client.baseURL, existingID)
		writeJSON(w, http.StatusOK, response)
		return
	}

	correspondentID, err := client.GetOrCreateEntity("correspondents", docReq.Correspondent, nil)
	if err != nil {
		paperlessErr(w, "correspondent", err)
		return
	}

	docTypeID, err := client.GetOrCreateEntity("document_types", docReq.DocumentType, nil)
	if err != nil {
		paperlessErr(w, "document_type", err)
		return
	}

	// User tags + sha256 dedup tag. docReq.Tags is already deduped above; we
	// just append the sha256 tag and re-dedup defensively.
	allTags := dedupTagNames(append(append([]string{}, docReq.Tags...), "sha256:"+docReq.SHA256Hash))
	tagIDs := make([]int, 0, len(allTags))
	for _, tagName := range allTags {
		tagID, err := client.GetOrCreateEntity("tags", tagName, nil)
		if err != nil {
			paperlessErr(w, "tag", fmt.Errorf("%q: %w", tagName, err))
			return
		}
		tagIDs = append(tagIDs, tagID)
	}
	tagIDs = dedupInts(tagIDs)

	type customFieldDef struct {
		Name     string
		DataType string
		Value    any
	}
	fieldDefs := []customFieldDef{
		{"Recipient", "string", docReq.Recipient},
		{"DocumentLanguageCode", "string", docReq.DocumentLanguageCode},
		{"ShortSummary", "longtext", docReq.ShortSummary},
		{"LongSummary", "longtext", docReq.LongSummary},
		{"RecipientDetails", "longtext", docReq.RecipientDetails},
		{"CorrespondentDetails", "longtext", docReq.CorrespondentDetails},
	}
	if len(docReq.Amounts) > 0 {
		fieldDefs = append(fieldDefs, customFieldDef{"Amounts", "longtext", mustJSON(docReq.Amounts)})
	}

	customFields := make(map[string]any)
	for _, fd := range fieldDefs {
		fieldID, err := client.GetOrCreateCustomField(fd.Name, fd.DataType)
		if err != nil {
			paperlessErr(w, "custom_field", fmt.Errorf("%q: %w", fd.Name, err))
			return
		}
		customFields[fmt.Sprintf("%d", fieldID)] = fd.Value
	}

	// Single default storage_paths entity referenced by every document. Its
	// template uses the Recipient custom field (created above) to branch by
	// recipient at file-write time.
	storagePathID, err := client.GetOrCreateStoragePath(defaultStoragePathName, defaultStoragePathPattern)
	if err != nil {
		paperlessErr(w, "storage_path", err)
		return
	}

	// Paperless appends the file extension when writing to disk, so the title
	// must not already carry one — otherwise the on-disk name ends in
	// "<name>.pdf.pdf".
	title := strings.TrimSuffix(docReq.ProposedFilename, filepath.Ext(docReq.ProposedFilename))

	taskID, err := client.UploadDocument(UploadParams{
		DocumentData:     docData,
		OriginalFilename: docReq.OriginalFilename,
		Title:            title,
		Created:          docReq.DocumentDate,
		CorrespondentID:  correspondentID,
		DocumentTypeID:   docTypeID,
		StoragePathID:    storagePathID,
		TagIDs:           tagIDs,
		CustomFields:     customFields,
	})
	if err != nil {
		paperlessErr(w, "upload", err)
		return
	}

	response := buildDocumentResponse(docReq, taskID)

	docID, waitErr := client.WaitForDocument(r.Context(), taskID, taskTimeout)
	switch {
	case waitErr == nil:
		response.DocumentURL = fmt.Sprintf("%s/documents/%d/", client.baseURL, docID)
		writeJSON(w, http.StatusCreated, response)
	case isTimeout(waitErr):
		response.TaskURL = fmt.Sprintf("%s/api/tasks/?task_id=%s", client.baseURL, url.QueryEscape(taskID))
		writeJSON(w, http.StatusAccepted, response)
	case isTaskFailed(waitErr):
		var f *ErrTaskFailed
		errors.As(waitErr, &f)
		writeError(w, http.StatusBadGateway, "paperless_task_failed", waitErr.Error(), map[string]any{
			"TaskID": f.TaskID,
			"Result": f.Result,
		})
	default:
		paperlessErr(w, "task_poll", waitErr)
	}
}

func isTimeout(err error) bool {
	var e *ErrTaskTimeout
	return errors.As(err, &e)
}

func isTaskFailed(err error) bool {
	var e *ErrTaskFailed
	return errors.As(err, &e)
}

// buildDocumentResponse copies the cleaned request fields into a DocumentResponse.
// URL fields are left empty; callers attach DocumentURL or TaskURL based on the
// polling outcome. Tags exclude any "sha256:" dedup tag.
func buildDocumentResponse(req DocumentRequest, taskID string) DocumentResponse {
	tags := make([]string, 0, len(req.Tags))
	for _, t := range req.Tags {
		if strings.HasPrefix(t, "sha256:") {
			continue
		}
		tags = append(tags, t)
	}
	return DocumentResponse{
		TaskID:               taskID,
		SHA256Hash:           req.SHA256Hash,
		OriginalFilename:     req.OriginalFilename,
		FileType:             req.FileType,
		DocumentDate:         req.DocumentDate,
		Year:                 req.Year,
		Month:                req.Month,
		Day:                  req.Day,
		DocumentType:         req.DocumentType,
		DocumentLanguageCode: req.DocumentLanguageCode,
		Correspondent:        req.Correspondent,
		CorrespondentDetails: req.CorrespondentDetails,
		Recipient:            req.Recipient,
		RecipientDetails:     req.RecipientDetails,
		ShortSummary:         req.ShortSummary,
		LongSummary:          req.LongSummary,
		ProposedFilename:     req.ProposedFilename,
		Amounts:              req.Amounts,
		Tags:                 tags,
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
