// Package proxy is a SOCKS5 proxy that applies the simulated network condition to every connection.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/armon/go-socks5"
	"github.com/nikokozak/train-gap/internal/sim"
)

// While there is no service, connections check this often whether it has returned. The condition
// changes with simulated time, so there is no event to wait on; a blackout lasts tens of seconds.
const blackoutPollInterval = 100 * time.Millisecond

// Stats counts traffic through the proxy.
type Stats struct {
	Connections int64
	BytesSent   int64
	BytesRecv   int64
}

// Server is a SOCKS5 proxy on 127.0.0.1 that shapes traffic with the current condition.
type Server struct {
	port             int
	currentCondition func() sim.Condition
	listener         net.Listener
	server           *socks5.Server
	stats            Stats
	running          bool
	mu               sync.Mutex
}

// NewServer creates a proxy on port (0 picks a free port). currentCondition is called for
// every chunk of data, so it must be fast and safe for concurrent use.
func NewServer(port int, currentCondition func() sim.Condition) *Server {
	return &Server{port: port, currentCondition: currentCondition}
}

// Start begins accepting connections in the background.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server already running")
	}

	conf := &socks5.Config{
		Dial: s.shapedDial,
		// Failed connections are normal here; the library would print them over the TUI.
		Logger: log.New(io.Discard, "", 0),
	}

	server, err := socks5.New(conf)
	if err != nil {
		return fmt.Errorf("failed to create SOCKS5 server: %w", err)
	}
	s.server = server

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", s.port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", s.port, err)
	}
	s.listener = listener
	if s.port == 0 {
		if addr, ok := listener.Addr().(*net.TCPAddr); ok {
			s.port = addr.Port
		}
	}
	s.running = true

	go s.acceptLoop()
	return nil
}

// acceptLoop accepts incoming connections
func (s *Server) acceptLoop() {
	var backoff time.Duration
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// Check if we're shutting down
			s.mu.Lock()
			running := s.running
			s.mu.Unlock()
			if !running {
				return
			}

			// Avoid busy-looping on repeated Accept errors.
			// net.Error.Temporary is deprecated but still used by net.Listener implementations.
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if backoff == 0 {
					backoff = 10 * time.Millisecond
				} else {
					backoff *= 2
					if backoff > 500*time.Millisecond {
						backoff = 500 * time.Millisecond
					}
				}
				time.Sleep(backoff)
				continue
			}

			// Non-temporary error while still running; sleep briefly to avoid pegging CPU.
			time.Sleep(50 * time.Millisecond)
			continue
		}

		backoff = 0
		atomic.AddInt64(&s.stats.Connections, 1)

		go func(c net.Conn) {
			// The error only says how the client's connection ended; a stall that the client
			// gave up on is expected behavior, not a proxy failure.
			_ = s.server.ServeConn(c)
		}(conn)
	}
}

// shapedDial opens the connection to the destination the way a phone would: it waits for
// service, pays the latency once, and returns a connection that keeps applying the condition.
func (s *Server) shapedDial(ctx context.Context, network, addr string) (net.Conn, error) {
	if err := waitForService(ctx.Done(), s.currentCondition); err != nil {
		return nil, err
	}
	if err := sleepWithCancel(ctx.Done(), s.currentCondition().Latency); err != nil {
		return nil, err
	}

	dialer := &net.Dialer{Timeout: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return &shapedConn{
		Conn:             conn,
		currentCondition: s.currentCondition,
		stats:            &s.stats,
		closed:           make(chan struct{}),
	}, nil
}

// Stop stops accepting connections.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.running = false
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

// GetStats returns proxy statistics
func (s *Server) GetStats() Stats {
	return Stats{
		Connections: atomic.LoadInt64(&s.stats.Connections),
		BytesSent:   atomic.LoadInt64(&s.stats.BytesSent),
		BytesRecv:   atomic.LoadInt64(&s.stats.BytesRecv),
	}
}

// Port returns the port the server is running on
func (s *Server) Port() int {
	return s.port
}

// shapedConn is the proxy's connection to the destination. Like TCP through a tunnel, data stalls
// while there is no service and continues when it returns; nothing is dropped. Each chunk of data
// waits the condition's latency once in each direction.
type shapedConn struct {
	net.Conn
	currentCondition func() sim.Condition
	stats            *Stats
	closed           chan struct{} // Closed by Close, so a stalled read or write gives up.
	closeOnce        sync.Once
}

// Read returns data from the destination, held back while there is no service.
func (c *shapedConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		if waitErr := c.delay(); waitErr != nil {
			return 0, waitErr
		}
		atomic.AddInt64(&c.stats.BytesRecv, int64(n))
	}
	return n, err
}

// Write sends data to the destination once there is service.
func (c *shapedConn) Write(b []byte) (int, error) {
	if err := c.delay(); err != nil {
		return 0, err
	}
	n, err := c.Conn.Write(b)
	atomic.AddInt64(&c.stats.BytesSent, int64(n))
	return n, err
}

// Close also releases any read or write that is waiting for service.
func (c *shapedConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

// delay waits for service, then for the latency.
func (c *shapedConn) delay() error {
	if err := waitForService(c.closed, c.currentCondition); err != nil {
		return err
	}
	return sleepWithCancel(c.closed, c.currentCondition().Latency)
}

// errConnectionClosed ends a wait when the connection closes.
var errConnectionClosed = errors.New("connection closed while waiting for service")

// waitForService returns when the condition is connected, or an error when cancel closes first.
func waitForService(cancel <-chan struct{}, currentCondition func() sim.Condition) error {
	for !currentCondition().IsConnected {
		if err := sleepWithCancel(cancel, blackoutPollInterval); err != nil {
			return err
		}
	}
	return nil
}

func sleepWithCancel(cancel <-chan struct{}, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-cancel:
		return errConnectionClosed
	case <-timer.C:
		return nil
	}
}
