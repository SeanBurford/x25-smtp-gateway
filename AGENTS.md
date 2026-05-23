# AGENTS.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```sh
# Build
go build ./...

# Format code
gofmt -w .

# Run all tests
go test ./...

# Run a single test
go test -run TestProcessEnvelope

# Run with verbose output
go test -v ./...
```

No external dependencies — only the Go standard library.

## Architecture

This is a bi-directional SMTP ↔ X.25 gateway. It bridges TCP-based SMTP and legacy X.25 packet-switched networks using Linux's native AF_X25 socket support (Linux-only; uses raw `syscall` calls).

### Two relay modes

Both modes are controlled by CLI flags and can run simultaneously in the same process:

| Flag | Mode | Direction |
|------|------|-----------|
| `-l <addr>` | SMTP-to-X.25 | Listens on TCP, connects outbound over X.25 |
| `-g <addr>` | X.25-to-SMTP | Listens on X.25, connects outbound over TCP/SMTP |

### Core data flow

Both modes share a single relay path via `handleRelay` in `smtp.go`. The `RelayConfig` struct is the abstraction point: callers supply `GetDestConn` (how to reach the destination) and `ReceivedHeader` (format of the injected `Received:` header). Everything else — envelope processing, data relay, stats, timeouts — is common.

```
SMTP-to-X.25:
  TCP client → processEnvelope → ConnectX25(addr from RCPT TO host) → relayData

X.25-to-SMTP:
  X.25 caller → CUD check → processEnvelope → net.Dial(smtpGateway) → relayData
```

**X.25 address routing (SMTP-to-X.25):** The X.25 destination address is extracted from the numeric hostname in the `RCPT TO` address. E.g., `RCPT TO: user@12345` connects to X.25 address `12345`. The hostname must be all-numeric (`isNumeric`).

**CUD (Call User Data):** Set via `-P` flag (hex, default `C0F70000`). Used both when placing outbound X.25 calls and for filtering inbound X.25 calls via `SIOCX25SCUDMATCHLEN`.

### File map

- `x25.go` — `X25Conn` (`net.Conn`) and `X25Listener` (`net.Listener`) backed by raw Linux syscalls. `ConnectX25` places outbound calls; `ListenX25` accepts inbound calls.
- `smtp.go` — All relay logic: `processEnvelope` (inbound SMTP parsing), `sendEnvelope` (outbound SMTP client), `relayData` (DATA phase passthrough), `handleRelay` (shared path), `StartSMTPToX25` / `StartX25ToSMTP` (goroutine entry points).
- `auth.go` — `MAIL FROM` allowlist loaded from a file (`-A`). When no file is loaded, all senders are permitted. Also accumulates rejected source IPs in `rejections`.
- `util.go` — Stats (`GatewayStats`, exposed via `expvar` at `/debug/vars`), `GenerateConnId` (random 8-hex-char connection identifier used in all log lines), `extractEmail`, `Sanitize` (strips CR/LF from header values), `isNumeric`.
- `main.go` — Flag parsing, signal handling (`SIGUSR1` dumps stats, `SIGTERM`/`SIGINT` log-and-exit), optional varz HTTP server.
