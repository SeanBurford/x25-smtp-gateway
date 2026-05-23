package main

import (
	"crypto/rand"
	"encoding/json"
	"expvar"
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"
)

type GatewayStats struct {
	Messages uint64 `json:"messages"`
	Bytes    uint64 `json:"bytes"`
	Success  uint64 `json:"success"`
	Failure  uint64 `json:"failure"`
}

type Stats struct {
	StartTime  time.Time    `json:"start_time"`
	Goroutines int          `json:"goroutines"`
	SMTPToX25  GatewayStats `json:"smtp_to_x25"`
	X25ToSMTP  GatewayStats `json:"x25_to_smtp"`
}

var (
	stats   Stats
	statsMu sync.Mutex
)

func InitStats() {
	stats.StartTime = time.Now()

	// Publish stats to expvar
	expvar.Publish("varz", expvar.Func(func() interface{} {
		statsMu.Lock()
		defer statsMu.Unlock()
		stats.Goroutines = runtime.NumGoroutine()
		return stats
	}))

	// Periodic log of stats
	go func() {
		for {
			time.Sleep(1 * time.Minute)
			LogStats()
		}
	}()
}

func LogStats() {
	statsMu.Lock()
	stats.Goroutines = runtime.NumGoroutine()
	b, _ := json.Marshal(stats)
	statsMu.Unlock()

	log.Printf("STATS: %s", string(b))
}

func GenerateConnId() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%08x", b)
}

func extractEmail(line string) string {
	idx := strings.Index(line, "<")
	if idx != -1 {
		end := strings.Index(line[idx:], ">")
		if end == -1 {
			return ""
		}
		return line[idx+1 : idx+end]
	}
	return line
}

func isNumeric(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func Sanitize(s string) string {
	// Simple sanitization for header values
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return s
}
