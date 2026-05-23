package main

import (
	"os"
	"strings"
	"testing"
)

func TestInitAuth(t *testing.T) {
	// Create a temporary file for auth
	tmpFile, err := os.CreateTemp("", "auth_test")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write([]byte("user@test.com\n"))
	if err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	err = InitAuth(tmpFile.Name())
	if err != nil {
		t.Fatalf("InitAuth failed: %v", err)
	}

	if !IsAllowed("user@test.com") {
		t.Error("user@test.com should be allowed after InitAuth")
	}
}

func TestInitAuth_FileError(t *testing.T) {
	err := InitAuth("/path/to/nonexistent/file")
	if err == nil {
		t.Error("expected error for nonexistent auth file, got nil")
	}
}

func TestAuth(t *testing.T) {
	authContent := `
user1@example.com
# comment
  user2@domain.org  
`
	err := LoadAuth(strings.NewReader(authContent))
	if err != nil {
		t.Fatalf("LoadAuth failed: %v", err)
	}

	tests := []struct {
		addr     string
		expected bool
	}{
		{"user1@example.com", true},
		{"user2@domain.org", true},
		{"user3@other.com", false},
		{"# comment", false},
		{"", false},
	}

	for _, tt := range tests {
		result := IsAllowed(tt.addr)
		if result != tt.expected {
			t.Errorf("IsAllowed(%q) = %v; want %v", tt.addr, result, tt.expected)
		}
	}
}

func TestRecordRejection(t *testing.T) {
	rejMu.Lock()
	rejections = nil
	rejMu.Unlock()

	RecordRejection("1.2.3.4")
	RecordRejection("5.6.7.8")

	rejMu.Lock()
	count := len(rejections)
	rejMu.Unlock()

	if count != 2 {
		t.Errorf("expected 2 rejections, got %d", count)
	}
}

func TestAuthEmpty(t *testing.T) {
	// Reset allowedAddrs
	authMu.Lock()
	allowedAddrs = nil
	authMu.Unlock()

	if !IsAllowed("any@any.com") {
		t.Errorf("IsAllowed should return true when no addresses are loaded")
	}
}
