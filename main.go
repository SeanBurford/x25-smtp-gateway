package main

import (
	_ "expvar"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

var (
	localAddr   = flag.String("a", "999999999", "local x.25 address (mandatory)")
	callData    = flag.String("P", "C0F70000", "protocol / call user data (hex)")
	smtpListen  = flag.String("l", "", "SMTP listen address (e.g. 127.0.0.1:2525)")
	smtpGateway = flag.String("g", "", "SMTP destination address for X.25 relay")
	software    = flag.String("S", "x25smtp", "software name for Received header")
	authFile    = flag.String("A", "", "allowed MAIL FROM addresses file")
	recvTimeout = flag.Int("t", 60, "receive timeout in seconds for inbound connections")
	varzAddr    = flag.String("V", "", "address for varz HTTP server (e.g. 127.0.0.1:5555)")
)

func main() {
	flag.Parse()

	if *localAddr == "999999999" {
		fmt.Println("Error: -a (local address) is mandatory")
		flag.Usage()
		os.Exit(1)
	}

	// Setup logging
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("Starting %s SMTP/X.25 Gateway...", *software)

	// Initialize Stats
	InitStats()

	// Start varz HTTP server if requested
	if *varzAddr != "" {
		log.Printf("Starting varz HTTP server on %s", *varzAddr)
		go func() {
			if err := http.ListenAndServe(*varzAddr, nil); err != nil {
				log.Printf("Varz HTTP server failed: %v", err)
			}
		}()
	}

	// Initialize Auth if provided
	if *authFile != "" {
		if err := InitAuth(*authFile); err != nil {
			log.Fatalf("Failed to initialize auth: %v", err)
		}
	}

	// Handle Signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGPIPE, syscall.SIGURG, syscall.SIGUSR1, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for sig := range sigChan {
			switch sig {
			case syscall.SIGPIPE:
				log.Println("Received SIGPIPE: Ignoring")
			case syscall.SIGURG:
				log.Println("Received SIGURG: Out-of-band data check (logging only)")
			case syscall.SIGUSR1:
				log.Println("Received SIGUSR1: Dumping Statistics")
				LogStats()
			case syscall.SIGINT, syscall.SIGTERM:
				log.Printf("Received %v: Shutting down", sig)
				LogStats()
				os.Exit(0)
			}
		}
	}()

	// Start Gateways
	if *smtpListen != "" {
		go StartSMTPToX25(*smtpListen, *localAddr, *callData)
	}

	if *smtpGateway != "" {
		go StartX25ToSMTP(*smtpGateway, *localAddr, *callData)
	}

	// Block forever
	select {}
}
