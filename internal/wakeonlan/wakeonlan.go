// Package wakeonlan provides functions to wake up other machines on the local
// network.
package wakeonlan

import (
	"bytes"
	"fmt"
	"net"
)

// SendMagicPacket sends the magic Wake On LAN packet to the specified MAC
// address, which is expected to be an ethernet MAC address
// (e.g. b0:6e:bf:30:70:3a).
func SendMagicPacket(localAddr *net.UDPAddr, mac string) error {
	hwaddr, err := net.ParseMAC(mac)
	if err != nil {
		return err
	}
	if got, want := len(hwaddr), 6; got != want {
		return fmt.Errorf("unexpected number of parts in hardware address %q: got %d, want %d", mac, got, want)
	}

	socket, err := net.DialUDP("udp4",
		localAddr,
		&net.UDPAddr{
			IP:   net.IPv4bcast,
			Port: 9, // discard
		})
	if err != nil {
		return fmt.Errorf("DialUDP(broadcast:discard): %v", err)
	}
	// https://en.wikipedia.org/wiki/Wake-on-LAN#Magic_packet
	payload := append(bytes.Repeat([]byte{0xff}, 6), bytes.Repeat(hwaddr, 16)...)
	if _, err := socket.Write(payload); err != nil {
		return err
	}
	return socket.Close()
}
