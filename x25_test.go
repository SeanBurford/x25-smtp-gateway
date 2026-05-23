package main

import (
	"testing"
	"time"
)

func TestX25Addr(t *testing.T) {
	addr := &x25Addr{addr: "12345"}
	if addr.Network() != "x25" {
		t.Errorf("Network() = %q; want %q", addr.Network(), "x25")
	}
	if addr.String() != "12345" {
		t.Errorf("String() = %q; want %q", addr.String(), "12345")
	}
}

func TestX25ConnPublicMethods(t *testing.T) {
	// We can't easily test Read/Write without a real X.25 socket,
	// but we can test the address methods.
	conn := &X25Conn{
		localAddr:  "LOCAL",
		remoteAddr: "REMOTE",
	}

	if conn.LocalAddr().String() != "LOCAL" {
		t.Errorf("LocalAddr = %q; want %q", conn.LocalAddr().String(), "LOCAL")
	}
	if conn.RemoteAddr().String() != "REMOTE" {
		t.Errorf("RemoteAddr = %q; want %q", conn.RemoteAddr().String(), "REMOTE")
	}

	// SetDeadline and others return nil, just verify they don't panic
	conn.SetDeadline(time.Now())
	conn.SetReadDeadline(time.Now())
	conn.SetWriteDeadline(time.Now())
}

// We can't easily test ConnectX25 or ListenX25 without actual AF_X25 support in environment.
