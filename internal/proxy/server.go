// Package proxy is a SOCKS5 and HTTP proxy that applies the simulated network condition to every
// connection.
package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
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

// How many connections the request log keeps, newest first.
const recentConnectionLimit = 50

// Server is a proxy on 127.0.0.1 that shapes traffic with the current condition. One port speaks
// both SOCKS5 and HTTP (CONNECT, and plain requests with a full URL), because many clients only
// support one of the two: Node's fetch and Python's requests only use an http:// proxy.
type Server struct {
	port             int
	currentCondition func() sim.Condition
	listener         net.Listener
	socksServer      *socks5.Server
	httpServer       *http.Server
	httpConnections  *connectionQueue // Accepted connections that start with an HTTP request.
	stats            Stats
	running          bool
	mu               sync.Mutex

	recentMu sync.Mutex
	recent   []*connectionRecord // Oldest first, at most recentConnectionLimit.
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
		Dial:     s.shapedDial,
		Resolver: passNamesThrough{},
		// Failed connections are normal here; the library would print them over the TUI.
		Logger: log.New(io.Discard, "", 0),
	}

	socksServer, err := socks5.New(conf)
	if err != nil {
		return fmt.Errorf("failed to create SOCKS5 server: %w", err)
	}
	s.socksServer = socksServer

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

	s.httpConnections = &connectionQueue{connections: make(chan net.Conn), closed: make(chan struct{}), address: listener.Addr()}
	forwarder := &httputil.ReverseProxy{
		// A proxy request names its destination in the URL, so there is nothing to rewrite.
		Rewrite: func(*httputil.ProxyRequest) {},
		// Proxy is nil: the forwarder must not send requests back through a proxy from the environment.
		Transport: &http.Transport{DialContext: s.shapedDial},
		ErrorLog:  log.New(io.Discard, "", 0),
	}
	s.httpServer = &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Method == http.MethodConnect {
				s.serveConnect(writer, request)
				return
			}
			forwarder.ServeHTTP(writer, request)
		}),
		ErrorLog: log.New(io.Discard, "", 0),
	}
	// Serve returns when Stop closes the queue; the error only says that.
	go func() { _ = s.httpServer.Serve(s.httpConnections) }()

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
		go s.serveConnection(conn)
	}
}

// The first byte of a SOCKS5 greeting is the protocol version. An HTTP request starts with a letter.
const socks5Version = 0x05

// serveConnection reads the first byte to tell SOCKS5 from HTTP, then hands the connection over.
func (s *Server) serveConnection(conn net.Conn) {
	reader := bufio.NewReader(conn)
	firstByte, err := reader.Peek(1)
	if err != nil {
		_ = conn.Close() // The client left before it sent anything.
		return
	}
	peeked := &peekedConn{Conn: conn, reader: reader}
	if firstByte[0] == socks5Version {
		// The error only says how the client's connection ended; a stall that the client
		// gave up on is expected behavior, not a proxy failure.
		_ = s.socksServer.ServeConn(peeked)
		return
	}
	select {
	case s.httpConnections.connections <- peeked:
	case <-s.httpConnections.closed:
		_ = conn.Close()
	}
}

// serveConnect opens a tunnel for an HTTP CONNECT request: it dials the destination like SOCKS5
// does, answers 200, and copies bytes both ways.
func (s *Server) serveConnect(writer http.ResponseWriter, request *http.Request) {
	destination, err := s.shapedDial(request.Context(), "tcp", request.Host)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadGateway)
		return
	}
	defer destination.Close()

	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		http.Error(writer, "the connection cannot be taken over", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		return // The client is gone.
	}
	defer client.Close()
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}

	// ponytail: when either side ends, both close. A client that half-closes and still waits for
	// data would lose the rest; TLS clients do not do that. Add CloseWrite handling if one does.
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(destination, buffered.Reader) // The reader holds any bytes sent after CONNECT.
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, destination)
		done <- struct{}{}
	}()
	<-done
}

// passNamesThrough leaves host names to shapedDial. The SOCKS5 library would otherwise look them
// up before the dial: without waiting for service, and showing only an address in the request log.
type passNamesThrough struct{}

func (passNamesThrough) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	return ctx, nil, nil
}

// shapedDial opens the connection to the destination the way a phone would: it waits for
// service, pays the latency once, and returns a connection that keeps applying the condition.
// The DNS lookup, if any, happens after the wait, as it would on the phone.
func (s *Server) shapedDial(ctx context.Context, network, addr string) (net.Conn, error) {
	record := s.recordConnection(addr)
	if err := waitForService(ctx.Done(), s.currentCondition, record); err != nil {
		record.fail()
		return nil, err
	}
	if err := sleepWithCancel(ctx.Done(), s.currentCondition().Latency); err != nil {
		record.fail()
		return nil, err
	}

	dialer := &net.Dialer{Timeout: 30 * time.Second}
	conn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		record.fail()
		return nil, err
	}
	record.connect()
	return &shapedConn{
		Conn:             conn,
		currentCondition: s.currentCondition,
		stats:            &s.stats,
		record:           record,
		closed:           make(chan struct{}),
	}, nil
}

// recordConnection adds a connection to the request log.
func (s *Server) recordConnection(destination string) *connectionRecord {
	record := &connectionRecord{destination: destination, openedAt: time.Now()}
	s.recentMu.Lock()
	defer s.recentMu.Unlock()
	s.recent = append(s.recent, record)
	if len(s.recent) > recentConnectionLimit {
		s.recent = s.recent[len(s.recent)-recentConnectionLimit:]
	}
	return record
}

// RecentConnections returns the request log, newest first.
func (s *Server) RecentConnections() []Connection {
	s.recentMu.Lock()
	records := append([]*connectionRecord(nil), s.recent...)
	s.recentMu.Unlock()

	now := time.Now()
	connections := make([]Connection, len(records))
	for index, record := range records {
		connections[len(records)-1-index] = record.snapshot(now)
	}
	return connections
}

// Stop stops accepting connections.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.running = false
	// Close only says that the queue was already closed; the listener error below is the one to report.
	_ = s.httpServer.Close()
	return s.listener.Close()
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
	record           *connectionRecord
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
		c.record.bytesReceived.Add(int64(n))
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
	c.record.bytesSent.Add(int64(n))
	return n, err
}

// Close also releases any read or write that is waiting for service.
func (c *shapedConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.record.close()
	})
	return c.Conn.Close()
}

// delay waits for service, then for the latency.
func (c *shapedConn) delay() error {
	if err := waitForService(c.closed, c.currentCondition, c.record); err != nil {
		return err
	}
	return sleepWithCancel(c.closed, c.currentCondition().Latency)
}

// peekedConn is a client connection whose first bytes were read to choose the protocol.
// Reads come from the buffer first.
type peekedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (conn *peekedConn) Read(b []byte) (int, error) {
	return conn.reader.Read(b)
}

// connectionQueue is a net.Listener for connections that are already accepted. The HTTP server
// takes the connections that start with an HTTP request from it.
type connectionQueue struct {
	connections chan net.Conn
	closed      chan struct{}
	closeOnce   sync.Once
	address     net.Addr
}

func (queue *connectionQueue) Accept() (net.Conn, error) {
	select {
	case conn := <-queue.connections:
		return conn, nil
	case <-queue.closed:
		return nil, net.ErrClosed
	}
}

func (queue *connectionQueue) Close() error {
	queue.closeOnce.Do(func() { close(queue.closed) })
	return nil
}

func (queue *connectionQueue) Addr() net.Addr {
	return queue.address
}

// errConnectionClosed ends a wait when the connection closes.
var errConnectionClosed = errors.New("connection closed while waiting for service")

// waitForService returns when the condition is connected, or an error when cancel closes first.
// The record counts the wait as a stall.
func waitForService(cancel <-chan struct{}, currentCondition func() sim.Condition, record *connectionRecord) error {
	if currentCondition().IsConnected {
		return nil
	}
	record.startWaiting()
	defer record.stopWaiting()
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

// Connection is one connection to a destination, as the request log shows it.
type Connection struct {
	Destination   string    // host:port, as the client asked for it.
	OpenedAt      time.Time // When the client asked for the connection.
	IsConnected   bool      // The dial finished. False while it waits for service, or after it failed.
	HasFailed     bool      // The dial failed, or the client gave up while it waited for service.
	IsClosed      bool
	IsWaiting     bool          // Waiting for service right now: the dial, or data held back.
	Stalled       time.Duration // Total time spent waiting for service, including now.
	BytesSent     int64
	BytesReceived int64
}

// connectionRecord is the live state behind a Connection. The byte counters change with every
// read and write, so they are atomic; the other fields change rarely and use mu.
type connectionRecord struct {
	destination   string
	openedAt      time.Time
	bytesSent     atomic.Int64
	bytesReceived atomic.Int64

	mu           sync.Mutex
	isConnected  bool
	hasFailed    bool
	isClosed     bool
	stalled      time.Duration // Finished waits only.
	waitingSince time.Time     // Zero when not waiting.
}

func (record *connectionRecord) connect() {
	record.mu.Lock()
	defer record.mu.Unlock()
	record.isConnected = true
}

func (record *connectionRecord) fail() {
	record.mu.Lock()
	defer record.mu.Unlock()
	record.hasFailed = true
}

func (record *connectionRecord) close() {
	record.mu.Lock()
	defer record.mu.Unlock()
	record.isClosed = true
}

func (record *connectionRecord) startWaiting() {
	record.mu.Lock()
	defer record.mu.Unlock()
	record.waitingSince = time.Now()
}

func (record *connectionRecord) stopWaiting() {
	record.mu.Lock()
	defer record.mu.Unlock()
	record.stalled += time.Since(record.waitingSince)
	record.waitingSince = time.Time{}
}

func (record *connectionRecord) snapshot(now time.Time) Connection {
	record.mu.Lock()
	defer record.mu.Unlock()
	connection := Connection{
		Destination:   record.destination,
		OpenedAt:      record.openedAt,
		IsConnected:   record.isConnected,
		HasFailed:     record.hasFailed,
		IsClosed:      record.isClosed,
		IsWaiting:     !record.waitingSince.IsZero(),
		Stalled:       record.stalled,
		BytesSent:     record.bytesSent.Load(),
		BytesReceived: record.bytesReceived.Load(),
	}
	if connection.IsWaiting {
		connection.Stalled += now.Sub(record.waitingSince)
	}
	return connection
}
