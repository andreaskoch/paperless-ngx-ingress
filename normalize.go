package main

import "strings"

// normalizeName canonicalizes a user-supplied entity name by trimming surrounding
// whitespace and collapsing internal runs of whitespace to a single ASCII space.
// An all-whitespace input returns "".
func normalizeName(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// dedupTagNames normalizes each tag and drops empties and case-insensitive
// duplicates, preserving the first occurrence's insertion order and form.
func dedupTagNames(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		n := normalizeName(t)
		if n == "" {
			continue
		}
		key := strings.ToLower(n)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, n)
	}
	return out
}

// dedupInts returns the input with repeated values removed, preserving order.
func dedupInts(in []int) []int {
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, v := range in {
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
