package main

import (
	"strings"
	"testing"
	"time"
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
		{"missing Data", func(r *DocumentRequest) { r.Data = "" }, "Data"},
		{"missing OriginalFilename", func(r *DocumentRequest) { r.OriginalFilename = "" }, "OriginalFilename"},
		{"missing FileType", func(r *DocumentRequest) { r.FileType = "" }, "FileType"},
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

func TestFillDateDefaults_AllSet(t *testing.T) {
	r := DocumentRequest{DocumentDate: "2026-03-15", Year: "2026", Month: "03", Day: "15"}
	r.FillDateDefaults(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if r.DocumentDate != "2026-03-15" {
		t.Fatalf("expected 2026-03-15, got %s", r.DocumentDate)
	}
}

func TestFillDateDefaults_NoDateFields(t *testing.T) {
	now := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)
	r := DocumentRequest{}
	r.FillDateDefaults(now)
	if r.Year != "2026" {
		t.Fatalf("expected year 2026, got %s", r.Year)
	}
	if r.Month != "03" {
		t.Fatalf("expected month 03, got %s", r.Month)
	}
	// Day defaults to last day of month (March has 31 days)
	if r.Day != "31" {
		t.Fatalf("expected day 31, got %s", r.Day)
	}
	if r.DocumentDate != "2026-03-31" {
		t.Fatalf("expected 2026-03-31, got %s", r.DocumentDate)
	}
}

func TestFillDateDefaults_MissingDay(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := DocumentRequest{Year: "2026", Month: "02"}
	r.FillDateDefaults(now)
	// February 2026 has 28 days
	if r.Day != "28" {
		t.Fatalf("expected day 28, got %s", r.Day)
	}
	if r.DocumentDate != "2026-02-28" {
		t.Fatalf("expected 2026-02-28, got %s", r.DocumentDate)
	}
}

func TestFillDateDefaults_MissingYearAndMonth(t *testing.T) {
	now := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	r := DocumentRequest{Day: "15"}
	r.FillDateDefaults(now)
	if r.Year != "2026" {
		t.Fatalf("expected year 2026, got %s", r.Year)
	}
	if r.Month != "07" {
		t.Fatalf("expected month 07, got %s", r.Month)
	}
	if r.DocumentDate != "2026-07-15" {
		t.Fatalf("expected 2026-07-15, got %s", r.DocumentDate)
	}
}

func TestFillDateDefaults_DocumentDateOnly(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := DocumentRequest{DocumentDate: "2025-12-25"}
	r.FillDateDefaults(now)
	if r.Year != "2025" {
		t.Fatalf("expected year 2025, got %s", r.Year)
	}
	if r.Month != "12" {
		t.Fatalf("expected month 12, got %s", r.Month)
	}
	if r.Day != "25" {
		t.Fatalf("expected day 25, got %s", r.Day)
	}
}

func TestFillDateDefaults_LeapYear(t *testing.T) {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	r := DocumentRequest{Year: "2024", Month: "02"}
	r.FillDateDefaults(now)
	if r.Day != "29" {
		t.Fatalf("expected day 29 (leap year), got %s", r.Day)
	}
}
