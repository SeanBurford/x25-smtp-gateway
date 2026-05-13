
package main

import (
	"testing"
)

func TestExtractEmail(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"MAIL FROM:<user@example.com>", "user@example.com"},
		{"RCPT TO: <other@domain.org>", "other@domain.org"},
		{"<simple@test.com>", "simple@test.com"},
		{"no brackets", "no brackets"},
		{"<missing-end", ""},
		{"<user@example.com>", "user@example.com"},
		{"user@example.com", "user@example.com"},
		{"MAIL FROM:<user@example.com> SIZE=100", "user@example.com"},
		{"", ""},
	}

	for _, tt := range tests {
		result := extractEmail(tt.input)
		if result != tt.expected {
			t.Errorf("extractEmail(%q) = %q; want %q", tt.input, result, tt.expected)
		}
	}
}

func TestGenerateConnId(t *testing.T) {
	id1 := GenerateConnId()
	id2 := GenerateConnId()
	if len(id1) != 8 {
		t.Errorf("expected length 8, got %d", len(id1))
	}
	if id1 == id2 {
		t.Errorf("expected unique IDs, got same: %s", id1)
	}
}

func TestInitStats(t *testing.T) {
	InitStats()
	if stats.StartTime.IsZero() {
		t.Error("expected StartTime to be set")
	}
}

func TestStats(t *testing.T) {
	statsMu.Lock()
	stats.SMTPToX25.Messages = 0
	statsMu.Unlock()

	statsMu.Lock()
	stats.SMTPToX25.Messages++
	statsMu.Unlock()

	if stats.SMTPToX25.Messages != 1 {
		t.Errorf("expected 1 message, got %d", stats.SMTPToX25.Messages)
	}

	// Just ensure LogStats doesn't panic
	LogStats()
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello\r\nworld", "helloworld"},
		{"no change", "no change"},
		{"\n\r", ""},
		{"no changes", "no changes"},
		{"\r\n\r\n", ""},
	}
	for _, tt := range tests {
		got := Sanitize(tt.input)
		if got != tt.expected {
			t.Errorf("Sanitize(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"12345", true},
		{"0", true},
		{"12a45", false},
		{"", true},
		{" ", false},
	}

	for _, tt := range tests {
		result := isNumeric(tt.input)
		if result != tt.expected {
			t.Errorf("isNumeric(%q) = %v; want %v", tt.input, result, tt.expected)
		}
	}
}
