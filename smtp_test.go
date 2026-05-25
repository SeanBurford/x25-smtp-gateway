package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
)

func TestFqdnToX121(t *testing.T) {
	for fqdn, expected := range map[string]string{
		"":                        "",
		".":                       "",
		"01234567890123":          "01234567890123",
		"012345678901234":         "",
		"example.com":             "",
		"1-day.example.com":       "",
		"day.1.example.com":       "",
		"0":                       "0",
		"0.com":                   "0",
		"0.example.com":           "0",
		"com.0":                   "",
		"999999":                  "999999",
		"999999.com":              "999999",
		"999999.example.com":      "999999",
		"456.123":                 "123456",
		"456.123.com":             "123456",
		"456.123.example.com":     "123456",
		"456.123.example.7.com":   "123456",
		"4.5.6.1.2.3.example.com": "321654",
		"127.0.0.1":               "100127",
		"2130706433":              "2130706433",
		"𝟐𝟏𝟑𝟎𝟕𝟎𝟔𝟒𝟑𝟑":              "",
	} {
		addr := fqdnToX121(fqdn)
		if addr != expected {
			t.Errorf("expected %q, got %q", expected, addr)
		}
	}
}

func TestReadSMTPResponse(t *testing.T) {
	tests := []struct {
		input       string
		wantSuccess bool
		wantResp    string
	}{
		{"220 Ready\r\n", true, "220 Ready"},
		{"250-Go ahead\r\n250 End\r\n", true, "250-Go ahead\n250 End"},
		{"550 Denied\r\n", false, "550 Denied"},
		{"250-First\r\n550 Error\r\n", false, "250-First\n550 Error"},
	}

	for _, tt := range tests {
		reader := bufio.NewReader(strings.NewReader(tt.input))
		success, resp, err := readSMTPResponse(reader)
		if err != nil {
			t.Errorf("readSMTPResponse(%q) error: %v", tt.input, err)
			continue
		}
		if success != tt.wantSuccess {
			t.Errorf("readSMTPResponse(%q) success = %v; want %v", tt.input, success, tt.wantSuccess)
		}
		if resp != tt.wantResp {
			t.Errorf("readSMTPResponse(%q) resp = %q; want %q", tt.input, resp, tt.wantResp)
		}
	}
}

func TestProcessEnvelope(t *testing.T) {
	input := "EHLO test.com\r\nMAIL FROM:<sender@test.com>\r\nRCPT TO:<rcpt@target.com>\r\nDATA\r\n"
	reader := bufio.NewReader(strings.NewReader(input))
	var output bytes.Buffer

	ehlo, from, to, err := processEnvelope(reader, &output, "test-id", "1.2.3.4")
	if err != nil {
		t.Fatalf("processEnvelope failed: %v", err)
	}

	if ehlo != "test.com" {
		t.Errorf("ehlo = %q; want %q", ehlo, "test.com")
	}
	if from != "sender@test.com" {
		t.Errorf("from = %q; want %q", from, "sender@test.com")
	}
	if to != "rcpt@target.com" {
		t.Errorf("to = %q; want %q", to, "rcpt@target.com")
	}

	// Check if server sent responses
	outStr := output.String()
	if !strings.Contains(outStr, "250") {
		t.Errorf("output missing 250 response: %q", outStr)
	}
}

func TestProcessEnvelope_Quit(t *testing.T) {
	input := "QUIT\r\n"
	reader := bufio.NewReader(strings.NewReader(input))
	var output bytes.Buffer

	_, _, _, err := processEnvelope(reader, &output, "test-id", "1.2.3.4")
	if err == nil {
		t.Fatal("processEnvelope should have returned error on QUIT")
	}
}

func TestProcessEnvelope_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"Empty line", "\r\nQUIT\r\n", true},
		{"MAIL before EHLO", "MAIL FROM:<s@t.com>\r\nQUIT\r\n", true},
		{"RCPT before MAIL", "EHLO t.com\r\nRCPT TO:<r@t.com>\r\nQUIT\r\n", true},
		{"DATA before MAIL", "HELO t.com\r\nDATA\r\nQUIT\r\n", true},
		{"DATA before RCPT", "EHLO t.com\r\nMAIL FROM:<s@t.com>\r\nDATA\r\nQUIT\r\n", true},
		{"Invalid RCPT", "EHLO t.com\r\nMAIL FROM:<s@t.com>\r\nRCPT TO:invalid\r\nQUIT\r\n", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			var output bytes.Buffer
			_, _, _, err := processEnvelope(reader, &output, "test", "1.1.1.1")
			if (err != nil) != tt.wantErr {
				t.Errorf("%s: err = %v, wantErr %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestProcessEnvelope_Disconnect(t *testing.T) {
	reader := bufio.NewReader(&errReader{})
	var output bytes.Buffer
	_, _, _, err := processEnvelope(reader, &output, "test", "1.1.1.1")
	if err == nil || !strings.Contains(err.Error(), "disconnected") {
		t.Errorf("expected disconnect error, got %v", err)
	}
}

func TestSendEnvelope(t *testing.T) {
	// Mock server responses
	responses := "220 Greeting\r\n" + // Response to connection
		"250 Hello\r\n" + // Response to EHLO
		"250 Sender OK\r\n" + // Response to MAIL FROM
		"250 Recipient OK\r\n" + // Response to RCPT TO
		"354 Start mail input\r\n" // Response to DATA

	reader := bufio.NewReader(strings.NewReader(responses))
	var sink bytes.Buffer

	err := sendEnvelope(reader, &sink, "test.com", "sender@test.com", "rcpt@target.com")
	if err != nil {
		t.Fatalf("sendEnvelope failed: %v", err)
	}

	sent := sink.String()
	expected := "EHLO test.com\r\nMAIL FROM: sender@test.com\r\nRCPT TO: rcpt@target.com\r\nDATA\r\n"
	if sent != expected {
		t.Errorf("sent commands = %q; want %q", sent, expected)
	}
}

func TestReadSMTPResponse_Multiline(t *testing.T) {
	input := "250-First line\r\n250-Second line\r\n250 Last line\r\n"
	reader := bufio.NewReader(strings.NewReader(input))
	success, resp, err := readSMTPResponse(reader)
	if err != nil {
		t.Fatalf("readSMTPResponse failed: %v", err)
	}
	if !success {
		t.Error("expected success for 250 response")
	}
	expectedResp := "250-First line\n250-Second line\n250 Last line"
	if resp != expectedResp {
		t.Errorf("resp = %q; want %q", resp, expectedResp)
	}
}

func TestHandleRelay(t *testing.T) {
	// Reset stats for testing
	statsMu.Lock()
	stats.SMTPToX25.Success = 0
	statsMu.Unlock()

	// Use net.Pipe for mock net.Conn
	client, server := net.Pipe()

	// Create another pipe for the destination
	destClient, destServer := net.Pipe()

	cfg := RelayConfig{
		Direction: "TEST",
		Greeting:  "220 Ready",
		Stats:     &stats.SMTPToX25,
		GetDestConn: func(connId, ehlo, from, to string) (net.Conn, string, error) {
			return destClient, "mock-dest", nil
		},
		ReceivedHeader: func(connId, ehlo, from, to, destAddr string) string {
			return "Received: test\r\n"
		},
	}

	// Run relay in goroutine
	go handleRelay(server, cfg)

	// In another goroutine, mock the destination server
	go func() {
		dsReader := bufio.NewReader(destServer)
		// Send Greeting
		destServer.Write([]byte("220 Greeting\r\n"))
		// Read EHLO
		dsReader.ReadString('\n')
		destServer.Write([]byte("250 Hello\r\n"))

		// Read MAIL FROM
		dsReader.ReadString('\n')
		destServer.Write([]byte("250 Sender OK\r\n"))

		// Read RCPT TO
		dsReader.ReadString('\n')
		destServer.Write([]byte("250 Recipient OK\r\n"))

		// Read DATA
		dsReader.ReadString('\n')
		destServer.Write([]byte("354 Start mail\r\n"))

		// Read Received header + Data
		for {
			line, _ := dsReader.ReadString('\n')
			if strings.TrimSpace(line) == "." {
				break
			}
		}
		destServer.Write([]byte("250 Message accepted\r\n"))

		// Read QUIT
		dsReader.ReadString('\n')
		destServer.Write([]byte("221 Goodbye\r\n"))
		destServer.Close()
	}()

	// Client side simulation
	cReader := bufio.NewReader(client)
	cReader.ReadString('\n') // Greeting

	// Create a dummy net.Conn for relayData/relayQUIT calls within handleRelay
	// net.Pipe already gives us net.Conn, so we just use them.

	client.Write([]byte("EHLO test.com\r\n"))
	readSMTPResponse(cReader) // 250s

	client.Write([]byte("MAIL FROM:<sender@test.com>\r\n"))
	readSMTPResponse(cReader) // 250

	client.Write([]byte("RCPT TO:<rcpt@test.com>\r\n"))
	readSMTPResponse(cReader) // 250

	client.Write([]byte("DATA\r\n"))
	readSMTPResponse(cReader) // 354

	client.Write([]byte("Subject: test\r\n\r\nHello\r\n.\r\n"))
	readSMTPResponse(cReader) // 250

	client.Write([]byte("QUIT\r\n"))
	client.Close()

	// End of relay will close 'server'

	statsMu.Lock()
	if stats.SMTPToX25.Success != 1 {
		t.Errorf("expected 1 success in stats, got %d", stats.SMTPToX25.Success)
	}
	statsMu.Unlock()
}

func TestHandleInboundSMTP_Greeting(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go handleInboundSMTP(server, "12345", "C0F7")

	reader := bufio.NewReader(client)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read greeting: %v", err)
	}
	if !strings.Contains(line, "220") {
		t.Errorf("expected greeting, got %q", line)
	}
}

func TestHandleInboundX25_CUDMismatch(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	// Since we refactored it to take a net.Conn and handle non-X25Conn gracefully
	go handleInboundX25(c2, "mock", "DEADBEEF")

	reader := bufio.NewReader(c1)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read CUD mismatch response: %v", err)
	}
	if !strings.Contains(line, "521") {
		t.Errorf("expected 521 for CUD mismatch, got %q", line)
	}
}

func TestHandleInboundX25_CUDMatch(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	// Refactored to handle non-X25Conn gracefully
	go handleInboundX25(c2, "127.0.0.1:25", "") // Empty expected CUD matches anything

	reader := bufio.NewReader(c1)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read greeting: %v", err)
	}
	if !strings.Contains(line, "220") {
		t.Errorf("expected greeting for CUD match, got %q", line)
	}
}

func TestHandleRelay_DestFail(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	cfg := RelayConfig{
		Direction: "TEST",
		Greeting:  "220 Ready",
		Stats:     &stats.SMTPToX25,
		GetDestConn: func(connId, ehlo, from, to string) (net.Conn, string, error) {
			return nil, "", fmt.Errorf("dial failed")
		},
	}

	go handleRelay(c2, cfg)

	reader := bufio.NewReader(c1)
	reader.ReadString('\n') // Greeting

	c1.Write([]byte("EHLO t.com\r\n"))
	readSMTPResponse(reader)
	c1.Write([]byte("MAIL FROM:<s@t.com>\r\n"))
	readSMTPResponse(reader)
	c1.Write([]byte("RCPT TO:<r@t.com>\r\n"))
	readSMTPResponse(reader)
	c1.Write([]byte("DATA\r\n"))

	line, _ := reader.ReadString('\n')
	if !strings.Contains(line, "421") {
		t.Errorf("expected 421 on dest failure, got %q", line)
	}
}

func TestRelayData_EOF(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	input := "Line 1\nLine 2"
	reader := bufio.NewReader(strings.NewReader(input))
	var out bytes.Buffer
	n, err := relayData(reader, &out, "test", "DIR", c2)
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
	if n != uint64(len(input)) {
		t.Errorf("expected %d bytes, got %d. Output: %q", len(input), n, out.String())
	}
	if out.String() != input {
		t.Errorf("expected output %q, got %q", input, out.String())
	}
}

func TestRelayData_Timeout(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	// Set a very short timeout
	*recvTimeout = 1

	// No data sent on c1
	reader := bufio.NewReader(c2)
	var out bytes.Buffer
	_, err := relayData(reader, &out, "test", "DIR", c2)
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
	if netErr, ok := err.(net.Error); !ok || !netErr.Timeout() {
		t.Errorf("expected timeout net.Error, got %v", err)
	}
}

func TestReadSMTPResponse_Error(t *testing.T) {
	reader := bufio.NewReader(&errReader{})
	_, _, err := readSMTPResponse(reader)
	if err == nil {
		t.Error("expected error from readSMTPResponse with erroring reader")
	}
}

func TestSendEnvelope_Failures(t *testing.T) {
	tests := []struct {
		name      string
		responses string
	}{
		{"Greet Fail", "500 Error\r\n"},
		{"EHLO Fail", "220 Greeting\r\n500 Error\r\n"},
		{"MAIL Fail", "220 Greeting\r\n250 Hello\r\n500 Error\r\n"},
		{"RCPT Fail", "220 Greeting\r\n250 Hello\r\n250 OK\r\n500 Error\r\n"},
		{"DATA Fail", "220 Greeting\r\n250 Hello\r\n250 OK\r\n250 OK\r\n500 Error\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.responses))
			var out bytes.Buffer
			err := sendEnvelope(reader, &out, "e", "f", "t")
			if err == nil {
				t.Errorf("%s: expected error, got nil", tt.name)
			}
		})
	}
}

func TestRelayData_Errors(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	// Read error
	errorReader := &errReader{}
	var out bytes.Buffer
	relayData(bufio.NewReader(errorReader), &out, "test", "DIR", c2)

	// Write error
	errorWriter := &errWriter{}
	input := "Line 1\n.\n"
	relayData(bufio.NewReader(strings.NewReader(input)), errorWriter, "test", "DIR", c2)
}

func TestRelayQUIT_Errors(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	// Read error
	errorReader := &errReader{}
	var out bytes.Buffer
	relayQUIT(bufio.NewReader(errorReader), &out, "test", "DIR", c2)

	// Write error
	errorWriter := &errWriter{}
	input := "QUIT\n"
	relayQUIT(bufio.NewReader(strings.NewReader(input)), errorWriter, "test", "DIR", c2)
}

type errReader struct{}

func (e *errReader) Read(p []byte) (n int, err error) { return 0, fmt.Errorf("read error") }

type errWriter struct{}

func (e *errWriter) Write(p []byte) (n int, err error) { return 0, fmt.Errorf("write error") }

func TestWriteSMTPResponse(t *testing.T) {
	var out bytes.Buffer
	writeSMTPResponse(&out, "Test Message")
	if out.String() != "Test Message\r\n" {
		t.Errorf("expected %q, got %q", "Test Message\r\n", out.String())
	}
}

func TestRelayData(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	input := "Line 1\nLine 2\n.\n"
	reader := bufio.NewReader(strings.NewReader(input))
	var sink bytes.Buffer

	bytesRelayed, err := relayData(reader, &sink, "id", "dir", c2)
	if err != nil {
		t.Fatalf("relayData failed: %v", err)
	}

	if bytesRelayed != uint64(len(input)) {
		t.Errorf("bytesRelayed = %d; want %d", bytesRelayed, len(input))
	}

	if sink.String() != input {
		t.Errorf("sink.String() = %q; want %q", sink.String(), input)
	}
}

func TestRelayQUIT(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	input := "OTHER CMD\nQUIT\n"
	reader := bufio.NewReader(strings.NewReader(input))
	var sink bytes.Buffer

	relayQUIT(reader, &sink, "id", "dir", c2)

	if sink.String() != input {
		t.Errorf("sink.String() = %q; want %q", sink.String(), input)
	}
}
