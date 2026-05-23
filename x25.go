package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"syscall"
	"time"
	"unsafe"
)

// Linux X.25 Constants
const (
	AF_X25                = 9
	SOCK_SEQPACKET        = 5
	SOL_X25               = 262
	SIOCX25GSUBSCRIP      = 0x89E0
	SIOCX25SSUBSCRIP      = 0x89E1
	SIOCX25GFACILITIES    = 0x89E2
	SIOCX25SFACILITIES    = 0x89E3
	SIOCX25GCALLUSERDATA  = 0x89E4
	SIOCX25SCALLUSERDATA  = 0x89E5
	SIOCX25GCAUSEDIAG     = 0x89E6
	SIOCX25SCUDMATCHLEN   = 0x89E7
	SIOCX25CALLACCPTAPPRV = 0x89E8
	SIOCX25SENDCALLACCPT  = 0x89E9
	SIOCX25GDTEFACILITIES = 0x89EA
	SIOCX25SDTEFACILITIES = 0x89EB
)

type sockaddrX25 struct {
	Family uint16
	Addr   [16]byte
}

// x25_subaddr matches the kernel's struct used for SIOCX25SCUDMATCHLEN
type x25SubAddr struct {
	CudMatchLength uint32
}

type x25CallUserData struct {
	CudLength uint32
	CudData   [128]byte
}

// X25Conn wraps a file descriptor to implement net.Conn for X.25
type X25Conn struct {
	fd          int
	connId      string
	localAddr   string
	remoteAddr  string
	ReceivedCUD []byte
	isInbound   bool
}

func (c *X25Conn) Read(b []byte) (n int, err error) {
	n, err = syscall.Read(c.fd, b)
	if n < 0 {
		n = 0
	}
	return n, err
}

func (c *X25Conn) Write(b []byte) (n int, err error) {
	// RFC req: send() will use MSG_EOR
	// Each SMTP command/response should be a single packet
	err = syscall.Sendto(c.fd, b, syscall.MSG_EOR, nil)
	if err == nil {
		return len(b), nil
	}
	return 0, err
}

func (c *X25Conn) Close() error {
	return syscall.Close(c.fd)
}

func (c *X25Conn) LocalAddr() net.Addr {
	return &x25Addr{addr: c.localAddr}
}

func (c *X25Conn) RemoteAddr() net.Addr {
	return &x25Addr{addr: c.remoteAddr}
}

func (c *X25Conn) SetDeadline(t time.Time) error      { return nil }
func (c *X25Conn) SetReadDeadline(t time.Time) error  { return nil }
func (c *X25Conn) SetWriteDeadline(t time.Time) error { return nil }

func (c *X25Conn) readSockAddr(sysCall uintptr) (string, error) {
	var sa sockaddrX25
	salen := uint32(unsafe.Sizeof(sa))
	_, _, errno := syscall.Syscall(sysCall, uintptr(c.fd), uintptr(unsafe.Pointer(&sa)), uintptr(unsafe.Pointer(&salen)))
	if errno != 0 {
		return "", errno
	}
	end := 0
	for end < len(sa.Addr) && sa.Addr[end] != 0 {
		end++
	}
	return string(sa.Addr[:end]), nil
}

// GetCallingAddress returns the address of the party that initiated the call.
// On inbound sockets this is the remote peer (getpeername); on outbound sockets
// it is the local address (getsockname).
func (c *X25Conn) GetCallingAddress() (string, error) {
	if c.isInbound {
		return c.readSockAddr(syscall.SYS_GETPEERNAME)
	}
	return c.readSockAddr(syscall.SYS_GETSOCKNAME)
}

// GetCalledAddress returns the address that was called.
// On inbound sockets this is the local address (getsockname); on outbound sockets
// it is the remote peer (getpeername).
func (c *X25Conn) GetCalledAddress() (string, error) {
	if c.isInbound {
		return c.readSockAddr(syscall.SYS_GETSOCKNAME)
	}
	return c.readSockAddr(syscall.SYS_GETPEERNAME)
}

func (c *X25Conn) SetOwner() error {
	// Use syscall directly instead of os.Getpid() to keep it simple, but log.Printf(os.Getpid()) is fine if we add os.
	// Actually, let's keep it as is, we don't strictly need F_SETOWN for basic function.
	return nil
}

func ConnectX25(remoteAddr, localAddr, callDataHex string, connId string) (net.Conn, error) {
	socketFD, err := syscall.Socket(AF_X25, SOCK_SEQPACKET, 0)
	if err != nil {
		return nil, fmt.Errorf("socket: %v", err)
	}
	log.Printf("[%s] X.25: Created socket FD %d", connId, socketFD)

	// 1. Set Local Address (Bind)
	lsa := sockaddrX25{Family: AF_X25}
	copy(lsa.Addr[:], localAddr)
	if _, _, errno := syscall.Syscall(syscall.SYS_BIND, uintptr(socketFD), uintptr(unsafe.Pointer(&lsa)), uintptr(unsafe.Sizeof(lsa))); errno != 0 {
		syscall.Close(socketFD)
		return nil, fmt.Errorf("bind: %v", errno)
	}

	// 2. Set Call User Data
	cud, _ := hex.DecodeString(callDataHex)
	if len(cud) > 0 {
		var xcud x25CallUserData
		xcud.CudLength = uint32(len(cud))
		copy(xcud.CudData[:], cud)
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(socketFD), uintptr(SIOCX25SCALLUSERDATA), uintptr(unsafe.Pointer(&xcud))); errno != 0 {
			log.Printf("[%s] X.25: Failed to set CUD: %v", connId, errno)
		}
	}

	// 4. Connect
	rsa := sockaddrX25{Family: AF_X25}
	copy(rsa.Addr[:], remoteAddr)
	if _, _, errno := syscall.Syscall(syscall.SYS_CONNECT, uintptr(socketFD), uintptr(unsafe.Pointer(&rsa)), uintptr(unsafe.Sizeof(rsa))); errno != 0 {
		syscall.Close(socketFD)
		return nil, fmt.Errorf("connect: %v", errno)
	}

	log.Printf("[%s] X.25: Connected to %s", connId, remoteAddr)

	xc := &X25Conn{
		fd:         socketFD,
		connId:     connId,
		localAddr:  localAddr,
		remoteAddr: remoteAddr,
	}
	return xc, nil
}

type X25Listener struct {
	fd          int
	localAddr   string
	callDataHex string
}

func (l *X25Listener) Accept() (net.Conn, error) {
	var rsa sockaddrX25
	rsalen := uint32(unsafe.Sizeof(rsa))
	nfd, _, errno := syscall.Syscall(syscall.SYS_ACCEPT, uintptr(l.fd), uintptr(unsafe.Pointer(&rsa)), uintptr(unsafe.Pointer(&rsalen)))
	if errno != 0 {
		return nil, errno
	}

	remoteAddr := ""
	end := 0
	for end < len(rsa.Addr) && rsa.Addr[end] != 0 {
		end++
	}
	remoteAddr = string(rsa.Addr[:end])

	// Check CUD
	var xcud x25CallUserData
	var receivedCud []byte
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, nfd, uintptr(SIOCX25GCALLUSERDATA), uintptr(unsafe.Pointer(&xcud))); errno == 0 {
		log.Printf("X.25: Incoming call with CUD: %x", xcud.CudData[:xcud.CudLength])
		receivedCud = make([]byte, xcud.CudLength)
		copy(receivedCud, xcud.CudData[:xcud.CudLength])
	}

	xc := &X25Conn{
		fd:          int(nfd),
		connId:      GenerateConnId(),
		localAddr:   l.localAddr,
		remoteAddr:  remoteAddr,
		ReceivedCUD: receivedCud,
		isInbound:   true,
	}
	return xc, nil
}

func (l *X25Listener) Close() error {
	return syscall.Close(l.fd)
}

func (l *X25Listener) Addr() net.Addr {
	return &x25Addr{addr: l.localAddr}
}

type x25Addr struct {
	addr string
}

func (a *x25Addr) Network() string { return "x25" }
func (a *x25Addr) String() string  { return a.addr }

func ListenX25(localAddr, callDataHex string) (net.Listener, error) {
	// Use syscall constants if they exist
	socketFD, err := syscall.Socket(AF_X25, SOCK_SEQPACKET, 0)
	if err != nil {
		return nil, fmt.Errorf("socket: %v", err)
	}
	log.Printf("X.25: Created listen socket FD %d", socketFD)

	// 1. Bind
	lsa := sockaddrX25{Family: AF_X25}
	copy(lsa.Addr[:], localAddr)
	log.Printf("X.25: Binding FD %d to address %s", socketFD, localAddr)
	if _, _, errno := syscall.Syscall(syscall.SYS_BIND, uintptr(socketFD), uintptr(unsafe.Pointer(&lsa)), uintptr(unsafe.Sizeof(lsa))); errno != 0 {
		syscall.Close(socketFD)
		return nil, fmt.Errorf("bind: %v", errno)
	}

	cud, _ := hex.DecodeString(callDataHex)

	// 2. Set Call User Data for matching
	if len(cud) > 0 {
		var xcud x25CallUserData
		xcud.CudLength = uint32(len(cud))
		copy(xcud.CudData[:], cud)
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(socketFD), uintptr(SIOCX25SCALLUSERDATA), uintptr(unsafe.Pointer(&xcud))); errno != 0 {
			log.Printf("X.25: Failed to set CUD: %v", errno)
		}
	}

	// 3. SIOCX25SCUDMATCHLEN
	var subAddr x25SubAddr
	subAddr.CudMatchLength = uint32(len(cud))
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(socketFD), uintptr(SIOCX25SCUDMATCHLEN), uintptr(unsafe.Pointer(&subAddr))); errno != 0 {
		log.Printf("X.25: Failed to set SCUDMATCHLEN: %v", errno)
	}

	// 4. Listen
	if err := syscall.Listen(socketFD, 10); err != nil {
		syscall.Close(socketFD)
		return nil, fmt.Errorf("listen: %v", err)
	}

	return &X25Listener{fd: socketFD, localAddr: localAddr, callDataHex: callDataHex}, nil
}
