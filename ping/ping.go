// Provides IPv4 ICMP echo round trip time measurements (“ping”).
package ping

// This code is mostly copied from go/src/pkg/net/ipraw_test.go,
// which is published under the following license:
//
// Copyright (c) 2012 The Go Authors. All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions are
// met:
//
//    * Redistributions of source code must retain the above copyright
// notice, this list of conditions and the following disclaimer.
//    * Redistributions in binary form must reproduce the above
// copyright notice, this list of conditions and the following disclaimer
// in the documentation and/or other materials provided with the
// distribution.
//    * Neither the name of Google Inc. nor the names of its
// contributors may be used to endorse or promote products derived from
// this software without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
// "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
// LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
// A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
// OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
// SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
// LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
// DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
// THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
// (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
// OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	icmpv4EchoRequest = 8
	icmpv4EchoReply   = 0
	icmpv6EchoRequest = 128
	icmpv6EchoReply   = 129
)

// icmpMessage represents an ICMP message.
type icmpMessage struct {
	Type     int             // type
	Code     int             // code
	Checksum int             // checksum
	Body     icmpMessageBody // body
}

// icmpMessageBody represents an ICMP message body.
type icmpMessageBody interface {
	Len() int
	Marshal() ([]byte, error)
}

// Marshal returns the binary enconding of the ICMP echo request or
// reply message m.
func (m *icmpMessage) Marshal() ([]byte, error) {
	b := []byte{byte(m.Type), byte(m.Code), 0, 0}
	if m.Body != nil && m.Body.Len() != 0 {
		mb, err := m.Body.Marshal()
		if err != nil {
			return nil, err
		}
		b = append(b, mb...)
	}
	switch m.Type {
	case icmpv6EchoRequest, icmpv6EchoReply:
		return b, nil
	}
	csumcv := len(b) - 1 // checksum coverage
	s := uint32(0)
	for i := 0; i < csumcv; i += 2 {
		s += uint32(b[i+1])<<8 | uint32(b[i])
	}
	if csumcv&1 == 0 {
		s += uint32(b[csumcv])
	}
	s = s>>16 + s&0xffff
	s = s + s>>16
	// Place checksum back in header; using ^= avoids the
	// assumption the checksum bytes are zero.
	b[2] ^= byte(^s & 0xff)
	b[3] ^= byte(^s >> 8)
	return b, nil
}

// parseICMPMessage parses b as an ICMP message.
func parseICMPMessage(b []byte) (*icmpMessage, error) {
	msglen := len(b)
	if msglen < 4 {
		return nil, errors.New("message too short")
	}
	m := &icmpMessage{Type: int(b[0]), Code: int(b[1]), Checksum: int(b[2])<<8 | int(b[3])}
	if msglen > 4 {
		var err error
		switch m.Type {
		case icmpv4EchoRequest, icmpv4EchoReply, icmpv6EchoRequest, icmpv6EchoReply:
			m.Body, err = parseICMPEcho(b[4:])
			if err != nil {
				return nil, err
			}
		}
	}
	return m, nil
}

// imcpEcho represenets an ICMP echo request or reply message body.
type icmpEcho struct {
	ID   int    // identifier
	Seq  int    // sequence number
	Data []byte // data
}

func (p *icmpEcho) Len() int {
	if p == nil {
		return 0
	}
	return 4 + len(p.Data)
}

// Marshal returns the binary enconding of the ICMP echo request or
// reply message body p.
func (p *icmpEcho) Marshal() ([]byte, error) {
	b := make([]byte, 4+len(p.Data))
	b[0], b[1] = byte(p.ID>>8), byte(p.ID&0xff)
	b[2], b[3] = byte(p.Seq>>8), byte(p.Seq&0xff)
	copy(b[4:], p.Data)
	return b, nil
}

// parseICMPEcho parses b as an ICMP echo request or reply message
// body.
func parseICMPEcho(b []byte) (*icmpEcho, error) {
	bodylen := len(b)
	p := &icmpEcho{ID: int(b[0])<<8 | int(b[1]), Seq: int(b[2])<<8 | int(b[3])}
	if bodylen > 4 {
		p.Data = make([]byte, bodylen-4)
		copy(p.Data, b[4:])
	}
	return p, nil
}

func ipv4Payload(b []byte) []byte {
	if len(b) < 20 {
		return b
	}
	hdrlen := int(b[0]&0x0f) << 2
	return b[hdrlen:]
}

func Ping(addr string, timeout time.Duration, result chan *time.Duration) {
	conn, err := net.Dial("ip4:icmp", addr)
	if err != nil {
		log.Print(err)
		result <- nil
		return
	}
	defer conn.Close()
	start := time.Now()
	conn.SetDeadline(time.Now().Add(timeout))

	xid, xseq := os.Getpid()&0xffff, 1
	b, err := (&icmpMessage{
		Type: icmpv4EchoRequest, Code: 0,
		Body: &icmpEcho{
			ID: xid, Seq: xseq,
			Data: bytes.Repeat([]byte("Go Go Gadget Ping!!!"), 3),
		},
	}).Marshal()
	if err != nil {
		log.Fatalf("icmpMessage.Marshal failed: %v", err)
	}
	if _, err := conn.Write(b); err != nil {
		log.Fatalf("Conn.Write failed: %v", err)
	}
	var m *icmpMessage
	for {
		if _, err := conn.Read(b); err != nil {
			result <- nil
			return
		}
		b = ipv4Payload(b)

		if m, err = parseICMPMessage(b); err != nil {
			log.Fatalf("[ping %s] parseICMPMessage failed: %v", addr, err)
		}
		switch p := m.Body.(type) {
		case *icmpEcho:
			if p.ID != xid || p.Seq != xseq {
				// This can happen when somebody else is also sending ICMP echo
				// requests — replies are sent to all the open sockets, so we
				// get replies for other programs, too. Skip this reply and
				// wait until either the right reply arrives or we run into the
				// timeout.
				continue
			}
			duration := time.Since(start)
			result <- &duration
			return
		default:
			log.Printf("[ping %s] got type=%v, code=%v; expected type=%v, code=%v", addr, m.Type, m.Code, icmpv4EchoRequest, 0)
			// In case the target answered with an ICMP reply that is not ICMP
			// echo, we skip it and either receive a proper reply later (in
			// which case the other ICMP message was not a reply for us) or run
			// into the timeout.
			continue
		}
	}
}

var seq uint // TODO: atomic

func PingUnprivileged(ctx context.Context, host string) (time.Duration, error) {
	const protocol = 1 // iana.ProtocolICMP
	c, err := icmp.ListenPacket("udp4", "0.0.0.0")
	if err != nil {
		return 0, err
	}
	defer c.Close()

	ips, err := net.LookupIP(host)
	if err != nil {
		return 0, err
	}
	if len(ips) == 0 {
		return 0, fmt.Errorf("Lookup(%v) = no IPs", host)
	}
	addr := &net.UDPAddr{IP: ips[0]}

	m := icmp.Message{
		Type: ipv4.ICMPTypeEcho, Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Data: []byte("HELLO-R-U-THERE"),
			Seq:  1 << uint(seq), // TODO: atomic
		},
	}
	seq++ // TODO: atomic

	wb, err := m.Marshal(nil)
	if err != nil {
		return 0, err
	}
	if n, err := c.WriteTo(wb, addr); err != nil {
		return 0, err
	} else if n != len(wb) {
		return 0, fmt.Errorf("got %v; want %v", n, len(wb))
	}

	start := time.Now()
	rb := make([]byte, 1500)
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.SetReadDeadline(deadline); err != nil {
			return 0, err
		}
	}

	n, peer, err := c.ReadFrom(rb)
	if err != nil {
		return 0, err
	}
	rm, err := icmp.ParseMessage(protocol, rb[:n])
	if err != nil {
		return 0, err
	}
	switch {
	case m.Type == ipv4.ICMPTypeEcho && rm.Type == ipv4.ICMPTypeEchoReply:
		fallthrough
	case m.Type == ipv6.ICMPTypeEchoRequest && rm.Type == ipv6.ICMPTypeEchoReply:
		fallthrough
	case m.Type == ipv4.ICMPTypeExtendedEchoRequest && rm.Type == ipv4.ICMPTypeExtendedEchoReply:
		fallthrough
	case m.Type == ipv6.ICMPTypeExtendedEchoRequest && rm.Type == ipv6.ICMPTypeExtendedEchoReply:
		return time.Since(start), nil
	default:
		return 0, fmt.Errorf("got %+v from %v; want echo reply or extended echo reply", rm, peer)
	}
}

func PingContext(ctx context.Context, addr string) (time.Duration, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp4", addr)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	start := time.Now()
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	xid, xseq := os.Getpid()&0xffff, 1
	b, err := (&icmpMessage{
		Type: icmpv4EchoRequest, Code: 0,
		Body: &icmpEcho{
			ID: xid, Seq: xseq,
			Data: bytes.Repeat([]byte("Go Go Gadget Ping!!!"), 3),
		},
	}).Marshal()
	if err != nil {
		return 0, fmt.Errorf("icmpMessage.Marshal: %v", err)
	}
	if _, err := conn.Write(b); err != nil {
		return 0, fmt.Errorf("Write: %v", err)
	}
	var m *icmpMessage
	for {
		if _, err := conn.Read(b); err != nil {
			return 0, fmt.Errorf("Read: %v", err)
		}
		b = ipv4Payload(b)

		if m, err = parseICMPMessage(b); err != nil {
			return 0, fmt.Errorf("parseICMPMessage: %v", err)
		}
		switch p := m.Body.(type) {
		case *icmpEcho:
			if p.ID != xid || p.Seq != xseq {
				// This can happen when somebody else is also sending ICMP echo
				// requests — replies are sent to all the open sockets, so we
				// get replies for other programs, too. Skip this reply and
				// wait until either the right reply arrives or we run into the
				// timeout.
				continue
			}
			return time.Since(start), nil
		default:
			log.Printf("[ping %s] got type=%v, code=%v; expected type=%v, code=%v", addr, m.Type, m.Code, icmpv4EchoRequest, 0)
			// In case the target answered with an ICMP reply that is not ICMP
			// echo, we skip it and either receive a proper reply later (in
			// which case the other ICMP message was not a reply for us) or run
			// into the timeout.
			continue
		}
	}
}
