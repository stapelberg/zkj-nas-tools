// Automatically calls wakeonlan(1) when traffic to a computer (network
// storage?) is detected via iptables, but the target doesn’t react to pings.
package main

import (
	"flag"
	"github.com/openshift/geard/pkg/go-netfilter-queue"
	"github.com/stapelberg/zkj-nas-tools/ping"
	"log"
	"os/exec"
	"time"
)

const (
	waiting = iota
	pinging
	waking
)

var (
	remoteIp = flag.String("remote_ip",
		"10.0.0.250",
		"IP address of the computer to wake")

	remoteMac = flag.String("remote_mac",
		"00:08:9b:d0:31:ef",
		"MAC address of the computer to wake")

	wakeTimeout = flag.Int("wake_timeout_secs",
		60,
		"Timeout for a wakeup to take until it is considered failed")

	wolCommand = flag.String("wol_command",
		"wakeonlan",
		"Command to call (with target MAC address as first argument) to wake up the computer")

	state = waiting
)

func pingRemote() {
	result := make(chan *time.Duration)
	go ping.Ping(*remoteIp, 1*time.Second, result)
	reachable := <-result != nil
	if reachable {
		state = waiting
		return
	}

	log.Printf("target unresponsive, sending magic packet to %s…\n", *remoteMac)
	if err := exec.Command(*wolCommand, *remoteMac).Run(); err != nil {
		log.Fatal(err)
	}
	for second := 0; second < *wakeTimeout; second++ {
		go ping.Ping(*remoteIp, 1*time.Second, result)
		if <-result != nil {
			log.Printf("target reachable after %d seconds\n", second)
			break
		}
	}
	state = waiting
}

func main() {
	flag.Parse()

	nfq, err := netfilter.NewNFQueue(23, 100, netfilter.NF_DEFAULT_PACKET_SIZE)
	if err != nil {
		log.Fatal(err)
	}
	defer nfq.Close()
	packets := nfq.GetPackets()

	for {
		select {
		case p := <-packets:
			p.SetVerdict(netfilter.NF_ACCEPT)
			if state == waiting {
				state = pinging
				go pingRemote()
			}
		}
	}
}
