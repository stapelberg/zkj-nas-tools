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
	ips := wake.IPs()
	macs := wake.MACs()
	if _, ok := ips[host]; !ok {
		log.Fatalf("syntax: wake <storage2|storage3|midna>")
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
