package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func subscribe(mqttClient mqtt.Client, topic string, hdl mqtt.MessageHandler) error {
	const qosAtMostOnce = 0
	log.Printf("Subscribing to %s", topic)
	token := mqttClient.Subscribe(topic, qosAtMostOnce, hdl)
	token.Wait()
	if err := token.Error(); err != nil {
		return fmt.Errorf("subscription failed: %v", err)
	}
	return nil
}

func suspendNASHandler(_ mqtt.Client, m mqtt.Message) {
	log.Printf("mqtt event: %s: %v", m.Topic(), string(m.Payload()))
	// The payload is currently ignored

	if strings.HasSuffix(m.Topic(), "storage2") {
		suspendNAS("10.0.0.252")
	} else {
		log.Printf("(ignoring command for unknown machine)")
	}
}

func runMQTT() error {
	opts := mqtt.NewClientOptions().AddBroker(*mqttBroker)
	clientID := "https://github.com/stapelberg/zkj-nas-tools/dornroeschen-main"
	if hostname, err := os.Hostname(); err == nil {
		clientID += "@" + hostname
	}
	opts.SetClientID(clientID)
	opts.SetConnectRetry(true)
	opts.OnConnect = func(c mqtt.Client) {
		if err := subscribe(c, "github.com/stapelberg/zkj-nas-tools/dornroeschen/cmd/suspendnas/#", suspendNASHandler); err != nil {
			log.Print(err)
		}
	}
	mqttClient := mqtt.NewClient(opts)
	if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("MQTT connection failed: %v", token.Error())
	}
	return nil
}
