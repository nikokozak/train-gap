package proxy

import (
	"io"
	"net"
	"time"
)

// ShapedConn wraps a net.Conn and applies traffic shaping
type ShapedConn struct {
	net.Conn
	shaper *Shaper
}

// NewShapedConn wraps a connection with traffic shaping
func NewShapedConn(conn net.Conn, shaper *Shaper) *ShapedConn {
	return &ShapedConn{
		Conn:   conn,
		shaper: shaper,
	}
}

// Read reads data from the connection, potentially with delays or drops
func (c *ShapedConn) Read(b []byte) (int, error) {
	// Check if blocked
	if c.shaper.IsBlocked() {
		// Simulate connection timeout in tunnel
		time.Sleep(100 * time.Millisecond)
		return 0, &net.OpError{Op: "read", Err: io.EOF}
	}

	// Apply delay
	if delay := c.shaper.Delay(); delay > 0 {
		time.Sleep(delay)
	}

	// Check for packet loss
	if c.shaper.ShouldDrop() {
		// Simulate packet loss by returning a short read
		// In practice, this means the higher-level protocol will need to retry
		time.Sleep(50 * time.Millisecond)
		return 0, nil
	}

	return c.Conn.Read(b)
}

// Write writes data to the connection, potentially with delays or drops
func (c *ShapedConn) Write(b []byte) (int, error) {
	// Check if blocked
	if c.shaper.IsBlocked() {
		// Simulate connection timeout in tunnel
		time.Sleep(100 * time.Millisecond)
		return 0, &net.OpError{Op: "write", Err: io.EOF}
	}

	// Apply delay
	if delay := c.shaper.Delay(); delay > 0 {
		time.Sleep(delay)
	}

	// Check for packet loss
	if c.shaper.ShouldDrop() {
		// Pretend we wrote but actually dropped it
		return len(b), nil
	}

	return c.Conn.Write(b)
}
