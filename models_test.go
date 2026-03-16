package main

import (
	"strings"
	"testing"
)

func TestValidateDocumentRequest_Valid(t *testing.T) {
	req := validRequest()
	if err := req.Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateDocumentRequest_MissingFields(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*DocumentRequest)
		wantErr string
	}{
		{"missing SHA256Hash", func(r *DocumentRequest) { r.SHA256Hash = "" }, "SHA256Hash"},
		{"missing Data", func(r *DocumentRequest) { r.Data = "" }, "Data"},
		{"missing OriginalFilename", func(r *DocumentRequest) { r.OriginalFilename = "" }, "OriginalFilename"},
		{"missing FileType", func(r *DocumentRequest) { r.FileType = "" }, "FileType"},
		{"missing DocumentDate", func(r *DocumentRequest) { r.DocumentDate = "" }, "DocumentDate"},
		{"missing DocumentType", func(r *DocumentRequest) { r.DocumentType = "" }, "DocumentType"},
		{"missing DocumentLanguageCode", func(r *DocumentRequest) { r.DocumentLanguageCode = "" }, "DocumentLanguageCode"},
		{"missing Correspondent", func(r *DocumentRequest) { r.Correspondent = "" }, "Correspondent"},
		{"missing CorrespondentDetails", func(r *DocumentRequest) { r.CorrespondentDetails = "" }, "CorrespondentDetails"},
		{"missing Recipient", func(r *DocumentRequest) { r.Recipient = "" }, "Recipient"},
		{"missing RecipientDetails", func(r *DocumentRequest) { r.RecipientDetails = "" }, "RecipientDetails"},
		{"missing ShortSummary", func(r *DocumentRequest) { r.ShortSummary = "" }, "ShortSummary"},
		{"missing LongSummary", func(r *DocumentRequest) { r.LongSummary = "" }, "LongSummary"},
		{"missing ProposedFilename", func(r *DocumentRequest) { r.ProposedFilename = "" }, "ProposedFilename"},
		{"missing Tags", func(r *DocumentRequest) { r.Tags = nil }, "Tags"},
		{"missing Amounts", func(r *DocumentRequest) { r.Amounts = nil }, "Amounts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validRequest()
			tt.mutate(&req)
			err := req.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func validRequest() DocumentRequest {
	return DocumentRequest{
		SHA256Hash:           "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
		Data:                 "dGVzdA==",
		OriginalFilename:     "test.pdf",
		FileType:             "pdf",
		DocumentDate:         "2026-01-01",
		DocumentType:         "Invoice",
		DocumentLanguageCode: "en",
		Correspondent:        "Test Corp",
		CorrespondentDetails: "Test Corp, 123 Main St",
		Recipient:            "My Company",
		RecipientDetails:     "My Company, 456 Oak Ave",
		ShortSummary:         "Test invoice",
		LongSummary:          "A detailed test invoice description",
		ProposedFilename:     "2026-01-01 Invoice from Test Corp",
		Amounts: []Amount{
			{Type: "Total", Amount: 100, CurrencyCode: "EUR"},
		},
		Tags: []string{"test", "invoice"},
	}
}
