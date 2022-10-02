package wake

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func PollSSH1(ctx context.Context, addr string) error {
	ctx, canc := context.WithTimeout(ctx, 5*time.Second)
	defer canc()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

func PollSSH(ctx context.Context, addr string) error {
	log.Printf("[%s] polling tcp/22 (ssh) port", addr)
	for {
		if err := ctx.Err(); err != nil {
			log.Printf("[%s] polling ended: %v", addr, err)
			return err
		}
		if err := PollSSH1(ctx, addr); err != nil {
			log.Print(err)
			continue
		}
		return nil // port 22 became reachable
	}
}

func PushMainboardPower(mqttBroker, clientID string) error {
	opts := mqtt.NewClientOptions().AddBroker(mqttBroker)
	if hostname, err := os.Hostname(); err == nil {
		clientID += "@" + hostname
	}
	opts.SetClientID(clientID)
	opts.SetConnectRetry(true)
	mqttClient := mqtt.NewClient(opts)
	if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
		return fmt.Errorf("MQTT connection failed: %v", token.Error())
	}

	const topic = "resetesp/switch/powerbtn/command"
	mqttClient.Publish(
		topic,
		0,     /* qos */
		false, /* retained */
		string("on"))

	return nil
}
