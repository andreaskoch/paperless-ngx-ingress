package main

import (
	"reflect"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Test Corp", "Test Corp"},
		{"  Test Corp  ", "Test Corp"},
		{"\tTest\tCorp\n", "Test Corp"},
		{"Test   Corp", "Test Corp"},
		{"Test \t Corp", "Test Corp"},
		{"", ""},
		{"   ", ""},
		{"\t\n ", ""},
		{"single", "single"},
		{"  a  b  c  ", "a b c"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := normalizeName(tt.in)
			if got != tt.want {
				t.Errorf("normalizeName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDedupTagNames(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, []string{}},
		{"single", []string{"invoice"}, []string{"invoice"}},
		{"exact duplicate", []string{"invoice", "invoice"}, []string{"invoice"}},
		{"case variant", []string{"invoice", "Invoice", "INVOICE"}, []string{"invoice"}},
		{"whitespace variant", []string{"invoice", " invoice ", "invoice\t"}, []string{"invoice"}},
		{"internal whitespace collapse", []string{"two words", "two  words"}, []string{"two words"}},
		{"preserves order, first form wins", []string{"Invoice", "invoice", "2026"}, []string{"Invoice", "2026"}},
		{"drops empty after normalize", []string{"", "   ", "invoice"}, []string{"invoice"}},
		{"distinct tags all kept", []string{"a", "b", "c"}, []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dedupTagNames(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dedupTagNames(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestDedupInts(t *testing.T) {
	tests := []struct {
		name string
		in   []int
		want []int
	}{
		{"empty", nil, []int{}},
		{"single", []int{7}, []int{7}},
		{"all duplicates", []int{3, 3, 3}, []int{3}},
		{"preserves order", []int{5, 1, 5, 2, 1}, []int{5, 1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dedupInts(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dedupInts(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
