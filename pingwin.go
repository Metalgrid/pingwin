package pingwin

import (
	"context"
	"log"
	"net"
	"os"

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

type Pingwin struct {
	Count         int
	Size          int
	Interval      int
	Timeout       int
	PacketTimeout int
	responder     chan PingwinResponse
	messages      [][]byte
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

	go p.send(ctx, sock, hosts)
	go p.receive(ctx, sock)

	return p.responder
}

func (p *Pingwin) send(ctx context.Context, sock *icmp.PacketConn, hosts []string) {
	for _, msg := range p.messages {
		for _, host := range hosts {
			select {
			case <-ctx.Done():
				return
			default:
				log.Println("Sending to:", host)
				_, err := sock.WriteTo(msg, &net.IPAddr{IP: net.ParseIP(host)})
				if err != nil {
					log.Println(err)
				}
			}
		}
	}
}

func (p *Pingwin) receive(ctx context.Context, sock *icmp.PacketConn) {
	buf := make([]byte, p.Size)
	select {
	case <-ctx.Done():
		return
	default:
		for {
			select {
			case <-ctx.Done():
				return
			default:
				bytes, peer, err := sock.ReadFrom(buf)
				if bytes == 0 || err != nil {
					continue
				}
				resp, err := icmp.ParseMessage(ipv4.ICMPTypeEcho.Protocol(), buf[:bytes])
				log.Println(peer, resp, err)
			}
		}
	}
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
