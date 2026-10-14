package proxy

import (
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nikokozak/train-gap/internal/sim"
)

// startUDPEchoServer returns the address of a UDP server that sends back every datagram.
func startUDPEchoServer(t *testing.T) *net.UDPAddr {
	t.Helper()
	server, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	go func() {
		buffer := make([]byte, maxDatagramSize)
		for {
			size, client, err := server.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			_, _ = server.WriteToUDP(buffer[:size], client)
		}
	}()
	return server.LocalAddr().(*net.UDPAddr)
}

// startUDPClient relays to a new echo server and returns a client socket connected to the relay.
func startUDPClient(t *testing.T, currentCondition func() sim.Condition) (*net.UDPConn, *Server, *net.UDPAddr) {
	t.Helper()
	echoAddress := startUDPEchoServer(t)
	server := NewServer(0, currentCondition)
	t.Cleanup(func() { _ = server.Stop() })
	port, err := server.RelayUDP(0, echoAddress.String())
	if err != nil {
		t.Fatalf("RelayUDP() error: %v", err)
	}
	client, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, server, echoAddress
}

// exchange sends a datagram and returns the first reply, or "" when none comes within wait.
func exchange(t *testing.T, client *net.UDPConn, message string, wait time.Duration) string {
	t.Helper()
	if _, err := client.Write([]byte(message)); err != nil {
		t.Fatal(err)
	}
	return readReply(t, client, wait)
}

func readReply(t *testing.T, client *net.UDPConn, wait time.Duration) string {
	t.Helper()
	_ = client.SetReadDeadline(time.Now().Add(wait)) // A failure shows as a missing reply.
	buffer := make([]byte, 100)
	size, err := client.Read(buffer)
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(buffer[:size])
}

// A datagram waits the latency each way; without service it is dropped, not held, and the
// request log counts it.
func TestUDPRelay(t *testing.T) {
	var isConnected atomic.Bool
	isConnected.Store(true)
	const latency = 50 * time.Millisecond
	condition := func() sim.Condition { return sim.Condition{IsConnected: isConnected.Load(), Latency: latency} }
	client, server, echoAddress := startUDPClient(t, condition)

	started := time.Now()
	if reply := exchange(t, client, "one", 2*time.Second); reply != "one" {
		t.Fatalf("with service: reply %q; want %q", reply, "one")
	}
	if elapsed := time.Since(started); elapsed < 2*latency {
		t.Fatalf("round trip took %v; want at least %v (the latency each way)", elapsed, 2*latency)
	}

	isConnected.Store(false)
	if reply := exchange(t, client, "two", 300*time.Millisecond); reply != "" {
		t.Fatalf("without service: reply %q; want none", reply)
	}
	isConnected.Store(true)
	if reply := exchange(t, client, "three", 2*time.Second); reply != "three" {
		t.Fatalf("after service returns: reply %q; want %q (the dropped datagram must not come back)", reply, "three")
	}

	connections := server.RecentConnections()
	if len(connections) != 1 {
		t.Fatalf("%d connections in the log; want 1 flow", len(connections))
	}
	if flow := connections[0]; flow.Destination != "udp "+echoAddress.String() || flow.DroppedDatagrams != 1 {
		t.Fatalf("log entry %q with %d dropped; want %q with 1", flow.Destination, flow.DroppedDatagrams, "udp "+echoAddress.String())
	}
}

// When the latency falls, a later datagram does not overtake an earlier one.
func TestUDPRelayKeepsOrder(t *testing.T) {
	var latencyNanoseconds atomic.Int64
	latencyNanoseconds.Store(int64(200 * time.Millisecond))
	condition := func() sim.Condition {
		return sim.Condition{IsConnected: true, Latency: time.Duration(latencyNanoseconds.Load())}
	}
	client, _, _ := startUDPClient(t, condition)

	if _, err := client.Write([]byte("first")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond) // Let the relay read "first" while the latency is still high.
	latencyNanoseconds.Store(0)
	if _, err := client.Write([]byte("second")); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"first", "second"} {
		if reply := readReply(t, client, 2*time.Second); reply != want {
			t.Fatalf("reply %q; want %q", reply, want)
		}
	}
}
