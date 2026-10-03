package proxy

import (
	"net"
	"strconv"
	"testing"
	"time"
)

func TestServerStartStopAndStats(t *testing.T) {
	s := NewServer(0)
	if err := s.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer func() { _ = s.Stop() }()

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(s.Port()))
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("DialTimeout error: %v", err)
	}
	_ = conn.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		stats := s.GetStats()
		if stats.Connections >= 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected Connections >= 1; got %#v", s.GetStats())
}
