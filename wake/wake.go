package main

import (
	"context"
	"flag"
	"log"
	"net"
	"time"

	"github.com/gokrazy/gokrazy"
	"github.com/stapelberg/zkj-nas-tools/internal/wake"
	"github.com/stapelberg/zkj-nas-tools/internal/wakeonlan"
)

func wakeUp(mqttBroker, host, ip, mac string) error {
	ctx := context.Background()

	{
		log.Printf("checking if tcp/22 (ssh) is available on %s", host)
		ctx, canc := context.WithTimeout(ctx, 5*time.Second)
		defer canc()
		if err := wake.PollSSH1(ctx, ip+":22"); err == nil {
			log.Printf("SSH already up and running")
			return nil // already up and running
		}
	}

	if host == "storage2" {
		// push the mainboard power button to turn off the PC part (ESP32 will
		// keep running on USB +5V standby power).
		log.Printf("pushing storage2 mainboard power button")
		const clientID = "github.com/stapelberg/zkj-nas-tools/wake"
		if err := wake.PushMainboardPower(mqttBroker, clientID); err != nil {
			log.Printf("pushing storage2 mainboard power button failed: %v", err)
		}
	} else {
		log.Printf("Sending magic packet to %v", mac)
		ips, err := gokrazy.PrivateInterfaceAddrs()
		if err != nil {
			return err
		}
		var laddr *net.UDPAddr
		_, lan, err := net.ParseCIDR("10.0.0.0/8")
		if err != nil {
			return err
		}
		for _, ipstr := range ips {
			if ip := net.ParseIP(ipstr); lan.Contains(ip) {
				laddr = &net.UDPAddr{IP: ip}
				break
			}
		}
		if err := wakeonlan.SendMagicPacket(laddr, mac); err != nil {
			log.Printf("sendWOL: %v", err)
		} else {
			log.Printf("Sent magic packet to %v", mac)
		}
	}

	{
		ctx, canc := context.WithTimeout(ctx, 5*time.Minute)
		defer canc()
		if err := wake.PollSSH(ctx, ip+":22"); err != nil {
			return err
		}
		log.Printf("host %s now awake", host)
	}

	return nil
}

func main() {
	var (
		mqttBroker = flag.String("mqtt_broker",
			"tcp://dr.lan:1883",
			"MQTT broker address for github.com/eclipse/paho.mqtt.golang")
	)
	flag.Parse()
	if flag.NArg() != 1 {
		log.Fatalf("syntax: wake <storage2|storage3|midna>")
	}
	host := flag.Arg(0)
	ips := map[string]string{
		"storage2": "10.0.0.252",
		"storage3": "10.0.0.253",
		"midna":    "10.0.0.76",
	}
	macs := map[string]string{
		// storage2 is woken up via MQTT
		"storage3": "70:85:c2:b6:02:24",
		// On-board network card connected for WOL only (link not even up in
		// Linux).
		"midna": "04:42:1a:31:9e:97",
	}
	if err := wakeUp(*mqttBroker, host, ips[host], macs[host]); err != nil {
		log.Fatal(err)
	}
}
