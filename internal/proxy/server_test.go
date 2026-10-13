package proxy

import (
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nikokozak/train-gap/internal/sim"
)

// A condition the test switches by hand, with no added latency.
func switchableCondition() (func() sim.Condition, *atomic.Bool) {
	var isConnected atomic.Bool
	isConnected.Store(true)
	return func() sim.Condition { return sim.Condition{IsConnected: isConnected.Load()} }, &isConnected
}

func startProxy(t *testing.T, currentCondition func() sim.Condition) *Server {
	t.Helper()
	server := NewServer(0, currentCondition)
	if err := server.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	t.Cleanup(func() { _ = server.Stop() })
	return server
}

// startEchoServer returns the address of a TCP server that sends back everything it receives.
func startEchoServer(t *testing.T) *net.TCPAddr {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(conn, conn) }()
		}
	}()
	return listener.Addr().(*net.TCPAddr)
}

// dialThroughProxy opens a connection to destination with a minimal SOCKS5 handshake
// (no authentication, CONNECT to an IPv4 address).
func dialThroughProxy(t *testing.T, proxyPort int, destination *net.TCPAddr) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(proxyPort)), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	greetingReply := make([]byte, 2)
	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(conn, greetingReply); err != nil || greetingReply[1] != 0 {
		t.Fatalf("SOCKS5 greeting reply %v, error %v", greetingReply, err)
	}
	request := append([]byte{5, 1, 0, 1}, destination.IP.To4()...)
	request = append(request, byte(destination.Port>>8), byte(destination.Port))
	connectReply := make([]byte, 10)
	if _, err := conn.Write(request); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(conn, connectReply); err != nil || connectReply[1] != 0 {
		t.Fatalf("SOCKS5 connect reply %v, error %v", connectReply, err)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn
}

func TestServerStartStopAndStats(t *testing.T) {
	currentCondition, _ := switchableCondition()
	server := startProxy(t, currentCondition)

	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(server.Port())), 500*time.Millisecond)
	if err != nil {
		t.Fatalf("DialTimeout error: %v", err)
	}
	_ = conn.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if server.GetStats().Connections >= 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected Connections >= 1; got %#v", server.GetStats())
}

// During a blackout, data waits instead of failing, and arrives intact when service returns:
// what an app sees from TCP in a subway tunnel.
func TestBlackoutStallsWithoutLosingData(t *testing.T) {
	currentCondition, isConnected := switchableCondition()
	server := startProxy(t, currentCondition)
	conn := dialThroughProxy(t, server.Port(), startEchoServer(t))

	isConnected.Store(false)
	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatalf("write during blackout: %v", err)
	}

	reply := make([]byte, 5)
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if n, err := conn.Read(reply); err == nil {
		t.Fatalf("got %q during the blackout; want the data held back", reply[:n])
	}

	isConnected.Store(true)
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("after service returned: %v", err)
	}
	if string(reply) != "hello" {
		t.Fatalf("got %q after the blackout; want %q", reply, "hello")
	}
}

// A connection opened during a blackout waits for service rather than failing.
func TestDialWaitsForService(t *testing.T) {
	currentCondition, isConnected := switchableCondition()
	isConnected.Store(false)
	server := startProxy(t, currentCondition)
	go func() {
		time.Sleep(300 * time.Millisecond)
		isConnected.Store(true)
	}()

	started := time.Now()
	conn := dialThroughProxy(t, server.Port(), startEchoServer(t))
	if waited := time.Since(started); waited < 250*time.Millisecond {
		t.Fatalf("connected after %v during a blackout; want a wait for service", waited)
	}
	if _, err := conn.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
}
