package main

import "fmt"

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

func (r *DocumentRequest) Validate() error {
	type fieldCheck struct {
		name  string
		value string
	}
	checks := []fieldCheck{
		{"Data", r.Data},
		{"OriginalFilename", r.OriginalFilename},
		{"FileType", r.FileType},
		{"DocumentDate", r.DocumentDate},
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
	for _, c := range checks {
		if c.value == "" {
			return fmt.Errorf("missing required field: %s", c.name)
		}
	}
	if len(r.Tags) == 0 {
		return fmt.Errorf("missing required field: Tags")
	}
	if len(r.Amounts) == 0 {
		return fmt.Errorf("missing required field: Amounts")
	}
	return nil
}
