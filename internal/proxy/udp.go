package proxy

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nikokozak/train-gap/internal/sim"
)

// The largest UDP payload. QUIC and real-time media send far smaller datagrams (about 1,200 bytes).
const maxDatagramSize = 65_535

// How many datagrams may wait for their latency in one direction of one flow. More are dropped,
// as a full router buffer drops them. At the slowest recorded latency (about 2 s each way), this
// holds a video call's traffic (a few hundred datagrams a second).
const datagramQueueLength = 1024

// A flow with no datagram in either direction for this long is closed; the next datagram from
// that client opens a new one. Linux keeps a UDP mapping for 120 s (nf_conntrack_udp_timeout_stream).
const udpFlowIdleTimeout = 2 * time.Minute

// RelayUDP forwards UDP datagrams between 127.0.0.1:port (0 picks a free port) and destination
// (host:port), applying the condition, until Stop. It returns the port. Point a UDP client, for
// example a game, a voice client, or an HTTP/3 client, at the port.
//
// UDP does not resend, so a datagram that meets no service is dropped, not held as TCP data is.
// Each datagram waits the condition's latency once; the datagrams of one direction keep their order.
func (s *Server) RelayUDP(port int, destination string) (int, error) {
	// Resolved once, here, so that a typo fails at start. ponytail: a destination that changes
	// address during a ride keeps the first one; resolve per flow if that matters.
	destinationAddress, err := net.ResolveUDPAddr("udp", destination)
	if err != nil {
		return 0, fmt.Errorf("UDP destination %s: %w", destination, err)
	}
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		return 0, fmt.Errorf("failed to listen on UDP port %d: %w", port, err)
	}
	relay := &udpRelay{
		server:             s,
		listener:           listener,
		destinationName:    destination,
		destinationAddress: destinationAddress,
		flows:              map[string]*udpFlow{},
	}
	s.mu.Lock()
	s.udpRelays = append(s.udpRelays, relay)
	s.mu.Unlock()
	go relay.readFromClients()
	return listener.LocalAddr().(*net.UDPAddr).Port, nil
}

// udpRelay forwards between local clients and one destination. Each client address gets its own
// flow: a socket to the destination, so that replies go back to the right client.
type udpRelay struct {
	server             *Server
	listener           *net.UDPConn
	destinationName    string // As given, for the request log.
	destinationAddress *net.UDPAddr

	mu    sync.Mutex
	flows map[string]*udpFlow // By client address.
}

// udpFlow is one client's traffic to the destination.
type udpFlow struct {
	clientAddress  *net.UDPAddr
	upstream       *net.UDPConn // Connected to the destination: it reads only the destination's replies.
	toDestination  *datagramQueue
	toClient       *datagramQueue
	record         *connectionRecord
	lastFromClient atomic.Int64 // Unix nanoseconds of the client's last datagram.
	closed         chan struct{}
	closeOnce      sync.Once
}

// readFromClients reads datagrams from local clients until the listener closes.
func (relay *udpRelay) readFromClients() {
	buffer := make([]byte, maxDatagramSize)
	for {
		size, clientAddress, err := relay.listener.ReadFromUDP(buffer)
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			continue // An error from one datagram, for example an ICMP report; the next may succeed.
		}
		flow, err := relay.flowFor(clientAddress)
		if err != nil {
			continue // The socket to the destination could not open: the datagram is lost, as UDP allows.
		}
		flow.lastFromClient.Store(time.Now().UnixNano())
		flow.toDestination.send(buffer[:size])
	}
}

// flowFor returns the client's flow, and opens it on the client's first datagram.
func (relay *udpRelay) flowFor(clientAddress *net.UDPAddr) (*udpFlow, error) {
	relay.mu.Lock()
	defer relay.mu.Unlock()
	if flow, ok := relay.flows[clientAddress.String()]; ok {
		return flow, nil
	}
	upstream, err := net.DialUDP("udp", nil, relay.destinationAddress)
	if err != nil {
		return nil, err
	}

	server := relay.server
	atomic.AddInt64(&server.stats.Connections, 1)
	record := server.recordConnection("udp " + relay.destinationName)
	record.connect()
	flow := &udpFlow{clientAddress: clientAddress, upstream: upstream, record: record, closed: make(chan struct{})}
	flow.toDestination = newDatagramQueue(server.currentCondition, record, flow.closed, func(datagram []byte) {
		size, _ := upstream.Write(datagram) // A failed write loses the datagram, as UDP allows.
		atomic.AddInt64(&server.stats.BytesSent, int64(size))
		record.bytesSent.Add(int64(size))
	})
	flow.toClient = newDatagramQueue(server.currentCondition, record, flow.closed, func(datagram []byte) {
		size, _ := relay.listener.WriteToUDP(datagram, clientAddress) // As above.
		atomic.AddInt64(&server.stats.BytesRecv, int64(size))
		record.bytesReceived.Add(int64(size))
	})
	relay.flows[clientAddress.String()] = flow
	go flow.readFromDestination(relay)
	return flow, nil
}

// readFromDestination reads the destination's replies until the flow is idle or closed.
func (flow *udpFlow) readFromDestination(relay *udpRelay) {
	defer relay.closeFlow(flow)
	buffer := make([]byte, maxDatagramSize)
	for {
		// The deadline only wakes the loop to check for an idle flow; the error says nothing else.
		_ = flow.upstream.SetReadDeadline(time.Now().Add(udpFlowIdleTimeout))
		size, err := flow.upstream.Read(buffer)
		var netError net.Error
		if errors.As(err, &netError) && netError.Timeout() {
			if time.Since(time.Unix(0, flow.lastFromClient.Load())) < udpFlowIdleTimeout {
				continue
			}
			return
		}
		if err != nil {
			return // Closed, or the destination refused (ICMP port unreachable). The next datagram opens a new flow.
		}
		flow.toClient.send(buffer[:size])
	}
}

// closeFlow ends a flow and forgets it.
func (relay *udpRelay) closeFlow(flow *udpFlow) {
	relay.mu.Lock()
	if relay.flows[flow.clientAddress.String()] == flow {
		delete(relay.flows, flow.clientAddress.String())
	}
	relay.mu.Unlock()
	flow.closeOnce.Do(func() {
		close(flow.closed)
		flow.record.close()
		_ = flow.upstream.Close() // Nothing reads or writes it after this.
	})
}

// close stops the relay and all its flows.
func (relay *udpRelay) close() {
	_ = relay.listener.Close() // Ends readFromClients; a second close only repeats the error.
	relay.mu.Lock()
	flows := make([]*udpFlow, 0, len(relay.flows))
	for _, flow := range relay.flows {
		flows = append(flows, flow)
	}
	relay.mu.Unlock()
	for _, flow := range flows {
		relay.closeFlow(flow)
	}
}

// datagramQueue is one direction of a flow. One goroutine delivers, first in first out, as on a
// real link: when the latency falls, a datagram does not overtake the ones before it. A datagram
// is dropped if there is no service when it is sent or when it is due: on the phone, it would be
// lost at the radio.
type datagramQueue struct {
	pending          chan delayedDatagram
	currentCondition func() sim.Condition
	record           *connectionRecord
}

type delayedDatagram struct {
	data  []byte
	dueAt time.Time
}

func newDatagramQueue(currentCondition func() sim.Condition, record *connectionRecord, closed <-chan struct{}, deliver func([]byte)) *datagramQueue {
	queue := &datagramQueue{
		pending:          make(chan delayedDatagram, datagramQueueLength),
		currentCondition: currentCondition,
		record:           record,
	}
	go queue.deliverUntil(closed, deliver)
	return queue
}

// send queues a copy of datagram for delivery after the current latency.
func (queue *datagramQueue) send(datagram []byte) {
	condition := queue.currentCondition()
	if !condition.IsConnected {
		queue.record.droppedDatagrams.Add(1)
		return
	}
	// ponytail: one allocation per datagram, because it outlives the read buffer while it waits.
	// Pool the buffers if a profile shows the allocation.
	select {
	case queue.pending <- delayedDatagram{data: bytes.Clone(datagram), dueAt: time.Now().Add(condition.Latency)}:
	default:
		queue.record.droppedDatagrams.Add(1) // The queue is full.
	}
}

// deliverUntil delivers each datagram when it is due, until closed.
func (queue *datagramQueue) deliverUntil(closed <-chan struct{}, deliver func([]byte)) {
	for {
		select {
		case <-closed:
			return
		case datagram := <-queue.pending:
			if sleepWithCancel(closed, time.Until(datagram.dueAt)) != nil {
				return
			}
			if !queue.currentCondition().IsConnected {
				queue.record.droppedDatagrams.Add(1)
				continue
			}
			deliver(datagram.data)
		}
	}
}
