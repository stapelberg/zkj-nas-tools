package main

import (
	"context"
	"flag"
	"log"
	"maps"
	"slices"
	"strings"

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
	hostname := flag.Arg(0)
	target, ok := wake.Hosts[hostname]
	if !ok {
		log.Fatalf("syntax: wake <%s>", strings.Join(slices.Sorted(maps.Keys(wake.Hosts)), "|"))
	}
	cfg := wake.Config{
		MQTTBroker: *mqttBroker,
		ClientID:   "github.com/stapelberg/zkj-nas-tools/wake",
		Target:     target,
	}
	if err := cfg.Wakeup(context.Background()); err != nil && err != wake.ErrAlreadyRunning {
		log.Fatal(err)
	}
}
