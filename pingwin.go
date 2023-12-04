package pingwin

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// Sane defaults close to the original ping utility.
const (
	defaultCount         = 4
	defaultSize          = 56
	defaultInterval      = 1000
	defaultTimeout       = 3000
	defaultPacketTimeout = 1000
)

const ICMPv4 = 1

type Pingwin struct {
	Count         int
	Size          int
	Interval      int
	Timeout       int
	PacketTimeout int
	responder     chan PingwinResponse
	messages      [][]byte
	requests      map[*net.IPAddr][]*time.Time
	responses     []RawResponse
}

type ICMPReply struct {
	*icmp.Message
	Addr       *net.IPAddr
	Err        error
	ReceivedAt time.Time
}

type RawResponse struct {
	message    []byte
	addr       *net.IPAddr
	receivedAt time.Time
}

func (r *RawResponse) ToICMPReply() ICMPReply {
	msg, err := icmp.ParseMessage(ICMPv4, r.message)
	return ICMPReply{msg, r.addr, err, r.receivedAt}
}

func (r RawResponse) String() string {
	return fmt.Sprintf("%s from %s: icmp_seq=%d time=%s", r.message, r.addr.String(), r.receivedAt.Format(time.RFC3339Nano))
}

type PingwinResponse struct {
	Host     string
	MinRTT   float64
	MaxRTT   float64
	AvgRTT   float64
	Sent     int
	Received int
	Loss     float64
}

func NewPingwin(count, size, interval, timeout, packetTimeout int) *Pingwin {
	if count <= 0 {
		count = defaultCount
	}

	if size <= 0 {
		size = defaultSize
	}

	if interval <= 0 {
		interval = defaultInterval
	}

	if timeout <= 0 {
		timeout = defaultTimeout
	}

	return &Pingwin{
		Count:         count,
		Size:          size,
		Interval:      interval,
		Timeout:       timeout,
		PacketTimeout: packetTimeout,
		responder:     make(chan PingwinResponse, count),
	}
}

func (p *Pingwin) Run(ctx context.Context, hosts []string) <-chan PingwinResponse {
	err := p.preparePackets()
	if err != nil {
		log.Fatal(err) // TODO: proper handling?
	}

	sock, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		log.Fatal(err) // TODO: proper handling?
	}
	// defer sock.Close()

	destinations := make([]*net.IPAddr, len(hosts))
	for i, host := range hosts {
		log.Println("Resolving:", host)
		destinations[i], err = net.ResolveIPAddr("ip4", host)

		if err != nil {
			log.Fatal(err) // TODO: proper handling?
		}
	}

	p.responses = make([]RawResponse, p.Count*len(hosts))
	_ = p.send(ctx, sock, destinations)
	recvDone := p.receive(ctx, sock)

	go p.postProcess(ctx, recvDone, destinations)

	return p.responder
}

func (p *Pingwin) send(ctx context.Context, sock *icmp.PacketConn, hosts []*net.IPAddr) <-chan struct{} {
	done := make(chan struct{})
	t := time.NewTicker(time.Duration(p.Interval * int(time.Millisecond)))
	// preallocate all buffers
	p.requests = make(map[*net.IPAddr][]*time.Time)
	for _, host := range hosts {
		p.requests[host] = make([]*time.Time, 0, p.Count)
	}

	go func() {
		defer close(done)
		for i := 0; i < p.Count; i++ {
			for _, host := range hosts {
				select {
				case <-ctx.Done():
					return
				default:
					_, err := sock.WriteTo(p.messages[i], host)
					if err != nil {
						log.Println(err) // TODO: sort out error handling
					} else {
						now := time.Now()
						p.requests[host][i] = &now
					}
				}
			}
			<-t.C
		}
	}()

	return done
}

func (p *Pingwin) receive(ctx context.Context, sock *icmp.PacketConn) <-chan struct{} {
	done := make(chan struct{})
	sock.SetReadDeadline(time.Now().Add(time.Duration(p.Timeout * int(time.Millisecond))))
	go func() {
		buf := make([]byte, p.Size)
		defer close(done)
		var i = 0
		for {
			select {
			case <-ctx.Done():
				return
			default:
				bytes, peer, err := sock.ReadFrom(buf)
				if err, ok := err.(net.Error); ok && err.Timeout() {
					// global timeout is reached, exit the receive loop
					return
				}

				if bytes == 0 || err != nil {
					continue
				}
				p.responses[i] = RawResponse{buf[:bytes], peer.(*net.IPAddr), time.Now()}
				i++
			}
		}
	}()
	return done
}

// preparePackets can go either the memory efficient way, or the CPU efficient way.
// The memory efficient way is to create a single packet and reuse it for each ping. (recalculate the checksum)
// The CPU efficient way is to create a packet for each ping. (calculate the checksum once, but keep many packets to send)
// We're going for CPU efficiency as the library is intended to be used as a part of a computationally-intensive application.
func (p *Pingwin) preparePackets() error {
	var err error
	p.messages = make([][]byte, p.Count)
	payload := make([]byte, p.Size)
	for i := 0; i < p.Count; i++ {
		msg := icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{
				ID:   os.Getpid() & 0xffff,
				Seq:  i + 1, // start counting from 1 for readability
				Data: payload,
			},
		}

		p.messages[i], err = msg.Marshal(nil)
		if err != nil {
			return err
		}
	}
	log.Println("Prepared packets: ", len(p.messages))

	return nil
}

func (p *Pingwin) postProcess(ctx context.Context, recvDone <-chan struct{}, hosts []*net.IPAddr) {
	<-recvDone
	log.Println("Processing responses")
	for _, response := range p.responses {
		icmpReply := response.ToICMPReply()
		if icmpReply.Err != nil {
			log.Println(icmpReply.Err)
			continue
		}
		log.Println(icmpReply)
	}
}
