package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"
)

func StartSMTPToX25(listenAddr, localX25, callData string) {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("SMTP: Listen failed: %v", err)
	}
	log.Printf("SMTP: Server listening on %s", listenAddr)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("SMTP: Accept error: %v", err)
			continue
		}
		go handleInboundSMTP(conn, localX25, callData)
	}
}

func processEnvelope(reader *bufio.Reader, response io.Writer, connId, remoteAddr string) (string, string, string, error) {
	var from, to, ehloName string
	for {
		if *recvTimeout > 0 {
			if conn, ok := response.(net.Conn); ok {
				conn.SetReadDeadline(time.Now().Add(time.Duration(*recvTimeout) * time.Second))
			}
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("[%s] SMTP Client timed out on read", connId)
				return "", "", "", fmt.Errorf("421 Timed out")
			}
			log.Printf("[%s] SMTP Client disconnected: %v", connId, err)
			return "", "", "", fmt.Errorf("Client disconnected")
		}
		line = strings.TrimSpace(line)
		log.Printf("[%s] SMTP: C> %s", connId, line)

		parts := strings.Fields(line)
		if len(parts) < 1 {
			log.Printf("[%s] SMTP: Invalid command", connId)
			writeSMTPResponse(response, "500 Syntax error, command unrecognized")
			continue
		}
		cmd := strings.ToUpper(parts[0])

		if cmd == "QUIT" {
			return "", "", "", fmt.Errorf("550 Access Denied")
		} else if line == "DATA" {
			if ehloName != "" && from != "" && to != "" {
				break
			}
			writeSMTPResponse(response, "503 Illegal command sequence")
		} else if cmd == "HELO" || cmd == "EHLO" {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				ehloName = parts[1]
			} else {
				ehloName = remoteAddr
			}
			hello := fmt.Sprintf("250-smtp Hello [%s]", remoteAddr)
			writeSMTPResponse(response, hello)
			writeSMTPResponse(response, "250-8BITMIME")
			writeSMTPResponse(response, "250 SIZE 10485760")
		} else if strings.HasPrefix(strings.ToUpper(line), "MAIL FROM:") {
			if ehloName == "" || from != "" {
				log.Printf("[%s] SMTP: Invalid MAIL FROM ordering", connId)
				writeSMTPResponse(response, "503 Illegal command sequence")
				continue
			}
			from = extractEmail(strings.TrimSpace(line[len("MAIL FROM:"):]))
			if !IsAllowed(from) {
				log.Printf("[%s] SMTP: Sender not allowed", connId)
				return "", "", "", fmt.Errorf("550 Access Denied")
			}
			writeSMTPResponse(response, "250 Sender OK")
		} else if strings.HasPrefix(strings.ToUpper(line), "RCPT TO:") {
			if ehloName == "" || to != "" {
				log.Printf("[%s] SMTP: Invalid RCPT TO ordering", connId)
				writeSMTPResponse(response, "503 Illegal command sequence")
				continue
			}
			to = extractEmail(strings.TrimSpace(line[len("RCPT TO:"):]))
			parts := strings.Split(to, "@")
			if len(parts) < 2 {
				log.Printf("[%s] SMTP: Missing hostname in RCPT TO: %s", connId, remoteAddr)
				return "", "", "", fmt.Errorf("501 Invalid address")
			}
			writeSMTPResponse(response, "250 Recipient OK")
		} else {
			log.Printf("[%s] SMTP: Invalid command", connId)
			writeSMTPResponse(response, "500 Syntax error, command unrecognized")
			continue
		}
	}
	return ehloName, from, to, nil
}

func readSMTPResponse(reader *bufio.Reader) (bool, string, error) {
	var lines []string
	allSuccess := true
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return false, "", err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		lines = append(lines, trimmed)
		if len(trimmed) > 0 && trimmed[0] != '2' && trimmed[0] != '3' {
			allSuccess = false
		}
		if len(trimmed) < 4 || trimmed[3] != '-' {
			break
		}
	}
	return allSuccess, strings.Join(lines, "\n"), nil
}

func sendEnvelope(reader *bufio.Reader, response io.Writer, ehloName, from, to string) error {
	success, resp, err := readSMTPResponse(reader)
	if err != nil {
		return fmt.Errorf("greeting read failed: %v", err)
	}
	if success == false {
		return fmt.Errorf("%s", resp)
	}

	// EHLO
	if _, err := response.Write([]byte(fmt.Sprintf("EHLO %s\r\n", ehloName))); err != nil {
		return fmt.Errorf("EHLO write failed: %v", err)
	}
	success, resp, err = readSMTPResponse(reader)
	if err != nil {
		return fmt.Errorf("EHLO response read failed: %v", err)
	}
	if success == false {
		return fmt.Errorf("%s", resp)
	}

	// MAIL FROM
	if _, err := response.Write([]byte(fmt.Sprintf("MAIL FROM: %s\r\n", from))); err != nil {
		return fmt.Errorf("MAIL FROM write failed: %v", err)
	}
	success, resp, err = readSMTPResponse(reader)
	if err != nil {
		return fmt.Errorf("MAIL FROM response read failed: %v", err)
	}
	if success == false {
		return fmt.Errorf("%s", resp)
	}

	// RCPT TO
	if _, err := response.Write([]byte(fmt.Sprintf("RCPT TO: %s\r\n", to))); err != nil {
		return fmt.Errorf("RCPT TO write failed: %v", err)
	}
	success, resp, err = readSMTPResponse(reader)
	if err != nil {
		return fmt.Errorf("RCPT TO response read failed: %v", err)
	}
	if success == false {
		return fmt.Errorf("%s", resp)
	}

	// DATA
	if _, err := response.Write([]byte("DATA\r\n")); err != nil {
		return fmt.Errorf("DATA write failed: %v", err)
	}
	success, resp, err = readSMTPResponse(reader)
	if err != nil {
		return fmt.Errorf("DATA response read failed: %v", err)
	}
	if success == false {
		return fmt.Errorf("%s", resp)
	}

	return nil
}

func fqdnToX121(fqdn string) string {
	addr := ""
	addrs := strings.Split(fqdn, ".")
	part := 0
	for part < len(addrs) && isNumeric(addrs[part]) {
		addr = fmt.Sprintf("%s%s", addrs[part], addr)
		part += 1
	}
	if len(addr) > 14 {
		// Maximum address length exceeded.
		return ""
	}
	return addr
}

func handleInboundSMTP(conn net.Conn, localX25, callDataHex string) {
	remoteAddr := conn.RemoteAddr().String()

	cfg := RelayConfig{
		Direction: "SMTP",
		Greeting:  "220 Gateway Ready",
		Stats:     &stats.SMTPToX25,
		GetDestConn: func(connId, ehlo, from, to string) (net.Conn, string, error) {
			// Extract X.25 address from RCPT TO.
			parts := strings.Split(to, "@")
			if len(parts) < 2 {
				return nil, "", fmt.Errorf("501 Invalid address")
			}
			addr := fqdnToX121(parts[1])
			if addr == "" {
				return nil, "", fmt.Errorf("501 Invalid address")
			}
			x25Conn, err := ConnectX25(addr, localX25, callDataHex, connId)
			return x25Conn, addr, err
		},
		ReceivedHeader: func(connId, ehlo, from, to, destAddr string) string {
			dnsName := ehlo
			if dnsName == "" {
				dnsName = "unknown"
			}
			sender := fmt.Sprintf("%s (%s [%s])", Sanitize(ehlo), Sanitize(dnsName), remoteAddr)
			recipientHeader := fmt.Sprintf("<%s> (x.121 [%s])", Sanitize(to), destAddr)
			return fmt.Sprintf("Received: from %s by %s as %s for %s; %s\r\n",
				sender, *software, connId, recipientHeader, time.Now().Format(time.RFC1123Z))
		},
		OnRejection: func() {
			RecordRejection(remoteAddr)
		},
	}

	handleRelay(conn, cfg)
}

type RelayConfig struct {
	Direction      string
	Greeting       string
	Stats          *GatewayStats
	GetDestConn    func(connId, ehlo, from, to string) (net.Conn, string, error)
	ReceivedHeader func(connId, ehlo, from, to, destAddr string) string
	OnRejection    func()
}

func handleRelay(src net.Conn, cfg RelayConfig) {
	connId := ""
	if srcX25, ok := src.(*X25Conn); ok {
		connId = srcX25.connId
	} else {
		connId = GenerateConnId()
	}

	srcAddr := src.RemoteAddr().String()
	if srcX25, ok := src.(*X25Conn); ok {
		if addr, err := srcX25.GetCallingAddress(); err == nil && addr != "" {
			srcAddr = addr
		}
	}

	log.Printf("[%s] %s: Connected from %s", connId, cfg.Direction, srcAddr)
	defer log.Printf("[%s] %s: Connection closed", connId, cfg.Direction)
	defer src.Close()

	if tcp, ok := src.(*net.TCPConn); ok {
		tcp.SetNoDelay(true)
		tcp.SetKeepAlive(true)
	}

	if *recvTimeout > 0 {
		src.SetReadDeadline(time.Now().Add(time.Duration(*recvTimeout) * time.Second))
	}

	reader := bufio.NewReader(src)
	writeSMTPResponse(src, cfg.Greeting)

	ehlo, from, to, err := processEnvelope(reader, src, connId, srcAddr)
	if err != nil {
		if cfg.OnRejection != nil {
			cfg.OnRejection()
		}
		statsMu.Lock()
		cfg.Stats.Failure++
		statsMu.Unlock()
		if !strings.Contains(err.Error(), "disconnected") {
			writeSMTPResponse(src, err.Error())
		}
		return
	}

	dest, destAddr, err := cfg.GetDestConn(connId, ehlo, from, to)
	if err != nil {
		log.Printf("[%s] %s: Destination connection to %v failed: %v", connId, cfg.Direction, destAddr, err)
		if cfg.OnRejection != nil {
			cfg.OnRejection()
		}
		statsMu.Lock()
		cfg.Stats.Failure++
		statsMu.Unlock()
		writeSMTPResponse(src, "421 Service not available")
		return
	}
	defer dest.Close()
	destReader := bufio.NewReader(dest)

	err = sendEnvelope(destReader, dest, ehlo, from, to)
	if err != nil {
		log.Printf("[%s] %s: Envelope relay failed: %v", connId, cfg.Direction, err)
		writeSMTPResponse(src, err.Error())
		return
	}

	writeSMTPResponse(src, "354 Start mail input; end with <CRLF>.<CRLF>")

	go func() {
		for {
			success, resp, err := readSMTPResponse(destReader)
			if err != nil {
				return
			}
			log.Printf("[%s] %s: S> %s", connId, cfg.Direction, resp)
			src.Write([]byte(resp + "\r\n"))
			if !success && strings.HasPrefix(resp, "421") {
				src.Close()
				return
			}
		}
	}()

	received := cfg.ReceivedHeader(connId, ehlo, from, to, destAddr)
	if _, err := dest.Write([]byte(received)); err != nil {
		log.Printf("[%s] %s: Failed to write Received header: %v", connId, cfg.Direction, err)
		return
	}

	totalBytes, err := relayData(reader, dest, connId, cfg.Direction, src)
	if err == nil {
		totalBytes += uint64(len(received))
		statsMu.Lock()
		cfg.Stats.Messages++
		cfg.Stats.Bytes += totalBytes
		cfg.Stats.Success++
		statsMu.Unlock()
		log.Printf("[%s] %s: Message relayed. From: %s, To: %s, Bytes: %d, Source: %s, Dest: %s",
			connId, cfg.Direction, Sanitize(from), Sanitize(to), totalBytes, srcAddr, destAddr)
	} else if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		log.Printf("[%s] %s: Client timed out during data relay", connId, cfg.Direction)
		return
	}

	relayQUIT(reader, dest, connId, cfg.Direction, src)
}

func relayData(reader *bufio.Reader, dest io.Writer, connId, direction string, src net.Conn) (uint64, error) {
	totalBytes := uint64(0)
	for {
		if *recvTimeout > 0 {
			src.SetReadDeadline(time.Now().Add(time.Duration(*recvTimeout) * time.Second))
		}
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			if _, werr := dest.Write([]byte(line)); werr != nil {
				log.Printf("[%s] %s: Write error during data relay: %v", connId, direction, werr)
				return totalBytes, werr
			}
			totalBytes += uint64(len(line))
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("[%s] %s: Read error during data relay: %v", connId, direction, err)
			}
			return totalBytes, err
		}
		if strings.TrimSpace(line) == "." {
			return totalBytes, nil
		}
	}
}

func relayQUIT(reader *bufio.Reader, dest io.Writer, connId, direction string, src net.Conn) {
	for {
		if *recvTimeout > 0 {
			src.SetReadDeadline(time.Now().Add(time.Duration(*recvTimeout) * time.Second))
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		cleanLine := strings.TrimSpace(line)
		log.Printf("[%s] %s: C> %s", connId, direction, cleanLine)
		if _, err := dest.Write([]byte(line)); err != nil {
			log.Printf("[%s] %s: Write error during post-data relay: %v", connId, direction, err)
			break
		}
		if strings.ToUpper(cleanLine) == "QUIT" {
			break
		}
	}
}

func writeSMTPResponse(writer io.Writer, msg string) {
	writer.Write([]byte(msg + "\r\n"))
}

func StartX25ToSMTP(smtpGateway, localX25, callDataHex string) {
	ln, err := ListenX25(localX25, callDataHex)
	if err != nil {
		log.Fatalf("X25: Listen failed: %v", err)
	}
	log.Printf("X25: Listening for incoming calls on %s with CUD matching %s", localX25, callDataHex)

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("X25: Accept error: %v", err)
			continue
		}
		go handleInboundX25(conn, smtpGateway, callDataHex)
	}
}

func handleInboundX25(conn net.Conn, smtpGateway, expectedCudHex string) {
	var connId string
	var receivedCUD []byte

	if x25, ok := conn.(*X25Conn); ok {
		connId = x25.connId
		receivedCUD = x25.ReceivedCUD
	} else {
		connId = GenerateConnId()
	}

	// Verify CUD
	if len(expectedCudHex) > 0 {
		expectedCUD, _ := hex.DecodeString(expectedCudHex)
		if bytes.Equal(receivedCUD, expectedCUD) == false {
			log.Printf("[%s] X.25: CUD mismatch. Expected %x, got %x", connId, expectedCUD, receivedCUD)
			writeSMTPResponse(conn, "521 Server Does Not Accept Mail")
			conn.Close()
			return
		}
	}

	cfg := RelayConfig{
		Direction: "X.25",
		Greeting:  "220 X25 Gateway Ready",
		Stats:     &stats.X25ToSMTP,
		GetDestConn: func(connId, ehlo, from, to string) (net.Conn, string, error) {
			dest, err := net.Dial("tcp", smtpGateway)
			return dest, smtpGateway, err
		},
		ReceivedHeader: func(connId, ehlo, from, to, destAddr string) string {
			callerAddr := ""
			if x25, ok := conn.(*X25Conn); ok {
				callerAddr, _ = x25.GetCallingAddress()
			}
			sender := fmt.Sprintf("x25-gateway (x.121 [%s])", Sanitize(callerAddr))
			return fmt.Sprintf("Received: from %s by %s as %s for <%s>; %s\r\n",
				sender, *software, connId, Sanitize(to), time.Now().Format(time.RFC1123Z))
		},
	}

	handleRelay(conn, cfg)
}
