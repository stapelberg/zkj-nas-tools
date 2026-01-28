package wake

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gokrazy/gokrazy/ifaddr"
	"github.com/stapelberg/zkj-nas-tools/internal/wakeonlan"
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
	// Do not try more than one connection attempt per second.
	tick := time.NewTicker(1 * time.Second)
	defer tick.Stop()
	log.Printf("[%s] polling tcp/22 (ssh) port", addr)
	for range tick.C {
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
	return nil
}

func pollHTTPHealthz1(ctx context.Context, addr string) error {
	ctx, canc := context.WithTimeout(ctx, 5*time.Second)
	defer canc()
	req, err := http.NewRequest("GET", "http://"+addr, nil)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		io.ReadAll(resp.Body)
		resp.Body.Close()
	}()
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		return fmt.Errorf("unexpected HTTP status code: got %d, want %d", got, want)
	}
	return nil
}

func PollHTTPHealthz(ctx context.Context, addr string) error {
	log.Printf("[%s] polling http/8200 (healthz) port", addr)
	for {
		time.Sleep(1 * time.Second)
		if err := ctx.Err(); err != nil {
			log.Printf("[%s] polling ended: %v", addr, err)
			return err
		}
		if err := pollHTTPHealthz1(ctx, addr); err != nil {
			log.Print(err)
			continue
		}
		return nil // addr returned HTTP 200
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
	defer mqttClient.Disconnect(1000 /* wait 1s for existing work to be completed */)

	const topic = "resetesp/switch/powerbtn/command"
	token := mqttClient.Publish(
		topic,
		0,     /* qos */
		false, /* retained */
		string("on"))
	if !token.WaitTimeout(30 * time.Second) {
		return fmt.Errorf("could not publish MQTT message within 30s: %v", token.Error())
	}

	return nil
}

type Host struct {
	Name  string
	IP    string
	MAC   string
	Relay string // webwake instance
}

var Hosts = map[string]Host{
	"midna": {
		Name:  "midna",
		IP:    "10.0.0.76", // static lease
		MAC:   "bc:fc:e7:68:12:0e",
		Relay: "router7",
	},
	"storage2": {
		Name: "storage2",
		IP:   "10.0.0.252",
		// No MAC, woken up via MQTT
		Relay: "router7",
	},
	"storage3": {
		Name:  "storage3",
		IP:    "10.0.0.253",
		MAC:   "70:85:c2:8d:b9:76",
		Relay: "router7",
	},
	"verkaufg9": {
		Name:  "verkaufg9",
		IP:    "10.11.0.2",
		MAC:   "7c:4d:8f:00:67:0a",
		Relay: "blr",
	},
}

type Config struct {
	MQTTBroker string
	ClientID   string

	Target Host
}

// The wake tool is invoked using speaking names (storage2, storage3), whereas
// dornroeschen uses the IP address as name. This function identifies storage
// targets using name or IP.
func (c *Config) isStorage() bool {
	return strings.HasPrefix(c.Target.Name, "storage") ||
		c.Target.IP == "10.0.0.252" ||
		c.Target.IP == "10.0.0.253"
}

var ErrAlreadyRunning = errors.New("already running")

// SendWakeSignal sends the wake signal (WoL or MQTT) without any polling.
// This is an atomic building block for CLI orchestration.
func (c *Config) SendWakeSignal() error {
	if c.Target.Name == "storage2" || c.Target.IP == "10.0.0.252" {
		log.Printf("pushing storage2 mainboard power button")
		return PushMainboardPower(c.MQTTBroker, c.ClientID)
	}

	log.Printf("Sending magic packet to %v", c.Target.MAC)
	ips, err := ifaddr.PrivateInterfaceAddrs()
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
	if err := wakeonlan.SendMagicPacket(laddr, c.Target.MAC); err != nil {
		return fmt.Errorf("sendWOL: %w", err)
	}
	log.Printf("Sent magic packet to %v", c.Target.MAC)
	return nil
}

// ProgressFunc is called to report progress during wakeup.
// phase is one of: "checking", "waking", "ssh", "health", "complete"
// status is one of: "start", "done", "skipped", "error", "already_running"
type ProgressFunc func(phase, status, detail string)

// Wakeup wakes up the specified host unless it is already running.
// A host is considered up when it accepts SSH connections (tcp/22).
//
// For hosts storage*, HTTP on port 8200 needs to return HTTP 200, too,
// signaling that the /srv mountpoint was successfully mounted.
func (c *Config) Wakeup(ctx context.Context) error {
	return c.WakeupWithProgress(ctx, nil)
}

// WakeupWithProgress is like Wakeup but calls progressFn to report progress.
func (c *Config) WakeupWithProgress(ctx context.Context, progressFn ProgressFunc) error {
	if progressFn == nil {
		progressFn = func(phase, status, detail string) {}
	}

	// Phase: checking
	progressFn("checking", "start", fmt.Sprintf("checking tcp/22 on %s", c.Target.Name))
	{
		log.Printf("checking if tcp/22 (ssh) is available on %s", c.Target.Name)
		checkCtx, canc := context.WithTimeout(ctx, 5*time.Second)
		defer canc()
		if err := PollSSH1(checkCtx, c.Target.IP+":22"); err == nil {
			log.Printf("SSH already up and running")
			progressFn("checking", "done", "already running")

			if c.isStorage() {
				progressFn("health", "start", "checking /srv mount")
				healthCtx, canc := context.WithTimeout(ctx, 5*time.Minute)
				defer canc()
				if err := PollHTTPHealthz(healthCtx, c.Target.IP+":8200"); err != nil {
					progressFn("health", "error", err.Error())
					return err
				}
				log.Printf("host %s signals /srv is mounted", c.Target.Name)
				progressFn("health", "done", "/srv mounted")
			}

			progressFn("complete", "already_running", "")
			return ErrAlreadyRunning
		}
		progressFn("checking", "done", "host is down")
	}

	// Phase: waking
	progressFn("waking", "start", "sending wake signal")
	if err := c.SendWakeSignal(); err != nil {
		progressFn("waking", "error", err.Error())
		// Note: storage2 continues even on error (existing behavior)
		if c.Target.Name != "storage2" && c.Target.IP != "10.0.0.252" {
			return err
		}
	} else {
		detail := "sent magic packet"
		if c.Target.Name == "storage2" || c.Target.IP == "10.0.0.252" {
			detail = "pushed mainboard power button"
		}
		progressFn("waking", "done", detail)
	}

	// Phase: ssh
	progressFn("ssh", "start", "polling tcp/22")
	{
		sshCtx, canc := context.WithTimeout(ctx, 5*time.Minute)
		defer canc()
		if err := PollSSH(sshCtx, c.Target.IP+":22"); err != nil {
			progressFn("ssh", "error", err.Error())
			return err
		}
		log.Printf("host %s now awake", c.Target.Name)
		progressFn("ssh", "done", "ssh responding")
	}

	// Phase: health
	if c.isStorage() {
		progressFn("health", "start", "checking /srv mount")
		healthCtx, canc := context.WithTimeout(ctx, 5*time.Minute)
		defer canc()
		if err := PollHTTPHealthz(healthCtx, c.Target.IP+":8200"); err != nil {
			progressFn("health", "error", err.Error())
			return err
		}
		log.Printf("host %s signals /srv is mounted", c.Target.Name)
		progressFn("health", "done", "/srv mounted")
	} else {
		progressFn("health", "skipped", "")
	}

	progressFn("complete", "done", "")
	return nil
}
