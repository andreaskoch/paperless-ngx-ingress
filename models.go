package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

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

// DocumentResponse mirrors the cleaned request plus a TaskID and exactly one
// of DocumentURL (when the document is ready) or TaskURL (when polling timed
// out). Data is never echoed.
type DocumentResponse struct {
	TaskID               string   `json:"TaskID"`
	DocumentURL          string   `json:"DocumentURL,omitempty"`
	TaskURL              string   `json:"TaskURL,omitempty"`
	SHA256Hash           string   `json:"SHA256Hash"`
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

// ErrorResponse is the wire shape for all error responses.
type ErrorResponse struct {
	Code    string         `json:"Code"`
	Error   string         `json:"Error"`
	Details map[string]any `json:"Details,omitempty"`
}

// FillDateDefaults sets Year/Month/Day from DocumentDate if present,
// or uses current date components as defaults, then rebuilds DocumentDate.
// If Day is empty, it defaults to the last day of the month.
func (r *DocumentRequest) FillDateDefaults(now time.Time) {
	// If DocumentDate is set, parse Year/Month/Day from it
	if r.DocumentDate != "" {
		if t, err := time.Parse("2006-01-02", r.DocumentDate); err == nil {
			if r.Year == "" {
				r.Year = strconv.Itoa(t.Year())
			}
			if r.Month == "" {
				r.Month = fmt.Sprintf("%02d", t.Month())
			}
			if r.Day == "" {
				r.Day = fmt.Sprintf("%02d", t.Day())
			}
			return
		}
	}

	// Default Year/Month to current if not set
	if r.Year == "" {
		r.Year = strconv.Itoa(now.Year())
	}
	if r.Month == "" {
		r.Month = fmt.Sprintf("%02d", now.Month())
	}

	// Default Day to last day of the month if not set
	if r.Day == "" {
		year, _ := strconv.Atoi(r.Year)
		month, _ := strconv.Atoi(r.Month)
		lastDay := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
		r.Day = fmt.Sprintf("%02d", lastDay)
	}

	// Rebuild DocumentDate
	r.DocumentDate = fmt.Sprintf("%s-%s-%s", r.Year, r.Month, r.Day)
}

// ValidationError carries the list of required fields that were empty after
// normalization and default-filling.
type ValidationError struct {
	MissingFields []string
}

func (e *ValidationError) Error() string {
	return "missing required fields: " + strings.Join(e.MissingFields, ", ")
}

func (r *DocumentRequest) Validate() error {
	type fieldCheck struct {
		name  string
		value string
	}
	checks := []fieldCheck{
		{"Data", r.Data},
		{"OriginalFilename", r.OriginalFilename},
		{"FileType", r.FileType},
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
	var missing []string
	for _, c := range checks {
		if c.value == "" {
			missing = append(missing, c.name)
		}
	}
	if len(missing) > 0 {
		return &ValidationError{MissingFields: missing}
	}
	return nil
}
