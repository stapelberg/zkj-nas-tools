package main

import (
	"context"
	"flag"
	"log"
	"time"

	"github.com/stapelberg/zkj-nas-tools/internal/wake"
	"github.com/stapelberg/zkj-nas-tools/internal/wakeonlan"
)

func wakeUp(mqttBroker, host, ip, mac string) error {
	ctx := context.Background()

	{
		log.Printf("checking if tcp/22 (ssh) is available on %s", host)
		ctx, canc := context.WithTimeout(ctx, 5*time.Second)
		defer canc()
		if err := wake.PollSSH1(ctx, host+":22"); err == nil {
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
		if err := wakeonlan.SendMagicPacket(nil, mac); err != nil {
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
	}

	return nil
}

func main() {
	var (
		host = flag.String("host",
			"storage2",
			"which host to wake up (storage2, storage3)")

		mqttBroker = flag.String("mqtt_broker",
			"tcp://dr.lan:1883",
			"MQTT broker address for github.com/eclipse/paho.mqtt.golang")
	)
	flag.Parse()
	ips := map[string]string{
		"storage2": "10.0.0.252",
		"storage3": "10.0.0.253",
	}
	macs := map[string]string{
		// storage2 is woken up via MQTT
		"storage3": "70:85:c2:b6:02:24",
	}
	if err := wakeUp(*mqttBroker, *host, ips[*host], macs[*host]); err != nil {
		log.Fatal(err)
	}
}
