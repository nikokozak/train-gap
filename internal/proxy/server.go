package proxy

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/armon/go-socks5"
	"train-gap/internal/sim"
)

// Stats holds proxy statistics
type Stats struct {
	Connections  int64
	BytesSent    int64
	BytesRecv    int64
	DroppedConns int64
}

// Server is a SOCKS5 proxy server with traffic shaping
type Server struct {
	port     int
	shaper   *Shaper
	listener net.Listener
	server   *socks5.Server
	stats    Stats
	running  bool
	mu       sync.Mutex
}

type timeoutError string

func (e timeoutError) Error() string   { return string(e) }
func (e timeoutError) Timeout() bool   { return true }
func (e timeoutError) Temporary() bool { return true }

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// NewServer creates a new proxy server
func NewServer(port int) *Server {
	return &Server{
		port:   port,
		shaper: NewShaper(),
	}
}

// Start starts the proxy server
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server already running")
	}

	// Create SOCKS5 config with our custom dial function
	conf := &socks5.Config{
		Dial: s.shapedDial,
	}

	server, err := socks5.New(conf)
	if err != nil {
		return fmt.Errorf("failed to create SOCKS5 server: %w", err)
	}
	s.server = server

	// Start listening
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

	// Accept connections in background
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

		// Handle connection in background
		go func(c net.Conn) {
			if err := s.server.ServeConn(c); err != nil {
				// Connection error (likely shaped/dropped)
				atomic.AddInt64(&s.stats.DroppedConns, 1)
			}
		}(conn)
	}
}

// shapedDial is a custom dial function that wraps connections with shaping
func (s *Server) shapedDial(ctx context.Context, network, addr string) (net.Conn, error) {
	// Check if completely blocked
	if s.shaper.IsBlocked() {
		// Simulate connection timeout
		_ = sleepWithContext(ctx, 500*time.Millisecond)
		return nil, timeoutError("connection timeout (tunnel)")
	}

	// Add connection delay
	if delay := s.shaper.Delay(); delay > 0 {
		if err := sleepWithContext(ctx, delay); err != nil {
			return nil, err
		}
	}

	// Dial the actual connection
	dialer := &net.Dialer{
		Timeout: 30 * time.Second,
	}
	conn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}

	// Wrap with shaping
	return &shapedConnWrapper{
		Conn:   conn,
		shaper: s.shaper,
		stats:  &s.stats,
	}, nil
}

// Stop stops the proxy server
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

// SetCondition updates the network condition
func (s *Server) SetCondition(c sim.NetworkCondition) {
	s.shaper.SetCondition(c)
}

// GetStats returns proxy statistics
func (s *Server) GetStats() Stats {
	return Stats{
		Connections:  atomic.LoadInt64(&s.stats.Connections),
		BytesSent:    atomic.LoadInt64(&s.stats.BytesSent),
		BytesRecv:    atomic.LoadInt64(&s.stats.BytesRecv),
		DroppedConns: atomic.LoadInt64(&s.stats.DroppedConns),
	}
}

// Port returns the port the server is running on
func (s *Server) Port() int {
	return s.port
}

// shapedConnWrapper wraps a connection with shaping and stats tracking
type shapedConnWrapper struct {
	net.Conn
	shaper *Shaper
	stats  *Stats
}

func (c *shapedConnWrapper) Read(b []byte) (int, error) {
	// Check if blocked
	if c.shaper.IsBlocked() {
		time.Sleep(100 * time.Millisecond)
		return 0, &net.OpError{Op: "read", Err: timeoutError("read timeout (tunnel)")}
	}

	// Apply delay
	if delay := c.shaper.Delay(); delay > 0 {
		time.Sleep(delay)
	}

	// Check for packet loss on read
	if c.shaper.ShouldDrop() {
		// Return timeout-like behavior
		time.Sleep(50 * time.Millisecond)
		// Don't actually drop reads completely, just delay them significantly
		// This simulates packet loss at the TCP level better
	}

	n, err := c.Conn.Read(b)
	if n > 0 {
		atomic.AddInt64(&c.stats.BytesRecv, int64(n))
	}
	return n, err
}

func (c *shapedConnWrapper) Write(b []byte) (int, error) {
	// Check if blocked
	if c.shaper.IsBlocked() {
		time.Sleep(100 * time.Millisecond)
		return 0, &net.OpError{Op: "write", Err: timeoutError("write timeout (tunnel)")}
	}

	// Apply delay
	if delay := c.shaper.Delay(); delay > 0 {
		time.Sleep(delay)
	}

	// Check for packet loss on write
	if c.shaper.ShouldDrop() {
		// Pretend we wrote it but drop
		atomic.AddInt64(&c.stats.BytesSent, int64(len(b)))
		return len(b), nil
	}

	n, err := c.Conn.Write(b)
	if n > 0 {
		atomic.AddInt64(&c.stats.BytesSent, int64(n))
	}
	return n, err
}
