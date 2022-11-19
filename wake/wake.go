package main

import (
	"context"
	"flag"
	"log"

	"github.com/stapelberg/zkj-nas-tools/internal/wake"
)

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
	cfg := wake.Config{
		MQTTBroker: *mqttBroker,
		ClientID:   "github.com/stapelberg/zkj-nas-tools/wake",
		Host:       host,
		IP:         ips[host],
		MAC:        macs[host],
	}
	if err := cfg.Wakeup(context.Background()); err != nil && err != wake.ErrAlreadyRunning {
		log.Fatal(err)
	}
}
