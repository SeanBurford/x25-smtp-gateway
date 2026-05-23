package main

import (
	"bufio"
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

var (
	allowedAddrs []string
	authMu       sync.Mutex
)

func InitAuth(filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer file.Close()
	return LoadAuth(file)
}

func LoadAuth(r io.Reader) error {
	authMu.Lock()
	defer authMu.Unlock()
	allowedAddrs = nil

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		addr := strings.TrimSpace(scanner.Text())
		if addr != "" && !strings.HasPrefix(addr, "#") {
			allowedAddrs = append(allowedAddrs, addr)
		}
	}
	log.Printf("Auth: Loaded %d allowed addresses", len(allowedAddrs))
	return scanner.Err()
}

func IsAllowed(from string) bool {
	authMu.Lock()
	defer authMu.Unlock()

	if len(allowedAddrs) == 0 {
		return true // Default allow if none specified? Or default deny?
		// "Mandatory -A" was not said, but if provided it should be used.
	}

	for _, addr := range allowedAddrs {
		if addr == from {
			return true
		}
	}
	return false
}

var (
	rejections []string
	rejMu      sync.Mutex
)

func RecordRejection(ip string) {
	rejMu.Lock()
	defer rejMu.Unlock()
	rejections = append(rejections, ip)
}
