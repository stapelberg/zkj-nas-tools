package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/eclipse/paho.mqtt.golang"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	blid = flag.String("blid",
		"9995801850128999",
		"Roomba BLID, identifying the roomba which should be controlled")

	password = flag.String("password",
		"secret",
		"Roomba password (TODO: how to obtain?)")
)

var (
	wifiSignalRSSI = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "roomba_wifi_signal_rssi",
		Help: "Received Signal Strength Indicator",
	})

	wifiSignalSNR = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "roomba_wifi_signal_snr",
		Help: "Signal to Noise Ratio",
	})

	dustBinFull = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "roomba_dust_bin_full",
		Help: "Whether the dust bin is full and should be emptied",
	})

	batteryPercentage = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "roomba_battery_percentage",
		Help: "Battery charge percentage",
	})
)

func init() {
	prometheus.MustRegister(wifiSignalRSSI)
	prometheus.MustRegister(wifiSignalSNR)
	prometheus.MustRegister(dustBinFull)
	prometheus.MustRegister(batteryPercentage)

	mqtt.DEBUG = log.New(os.Stdout, "mqtt ", log.LstdFlags)
}

var toRoomba = make(chan string)

func scheduleRoomba() {
	for {
		if err := schedule(); err != nil {
			log.Printf("scheduling roomba failed: %v", err)
		}
		time.Sleep(1 * time.Second)
		continue
	}
}

func schedule() error {
	u, err := url.Parse("tls://Roomba-" + *blid + ":8883")
	if err != nil {
		return err
	}
	cl := mqtt.NewClient(&mqtt.ClientOptions{
		Servers:         []*url.URL{u},
		ClientID:        *blid,
		Username:        *blid,
		Password:        *password,
		CleanSession:    false,
		ProtocolVersion: 4,
		KeepAlive:       1 * time.Minute,
		PingTimeout:     5 * time.Minute,
		AutoReconnect:   true,
		// TODO: can we verify the certificate?
		TLSConfig: tls.Config{InsecureSkipVerify: true},
		DefaultPublishHander: func(cl mqtt.Client, msg mqtt.Message) {
			//log.Printf("cl: %v, msg = %v", cl, msg)
			log.Printf("topic %q, id %d, payload %s", msg.Topic(), msg.MessageID(), string(msg.Payload()))
		},
	})
	var shadow struct {
		State struct {
			Reported struct {
				Name              string `json:"name"`
				Country           string `json:"country"`
				BatteryPercentage int    `json:"batPct"`

				DustBin struct {
					Present bool `json:"present"`
					Full    bool `json:"full"`
				} `json:"bin"`

				CleanMissionStatus struct {
					Phase string `json:"phase"`
				} `json:"cleanMissionStatus"`

				Bbchg3 struct {
					AvgMin int `json:"avgMin"` // TODO
					EstCap int `json:"estCap"` // TODO
				} `json:"bbchg3"`

				Bbrun struct {
					Hours      int `json:"hr"`
					Minutes    int `json:"min"`
					SquareFeet int `json:"sqft"`
					NumStuck   int `json:"nStuck"`
					NumScrubs  int `json:"nScrubs"`
					NumPanics  int `json:"nPanics"`
				} `json:"bbrun"`
			} `json:"reported"`
		} `json:"state"`
	}

	// See https://docs.aws.amazon.com/iot/latest/developerguide/thing-shadow-mqtt.html
	cl.AddRoute("$aws/things/"+*blid+"/shadow/update", func(cl mqtt.Client, msg mqtt.Message) {
		log.Printf("shadow: %s", string(msg.Payload()))
		if err := json.Unmarshal(msg.Payload(), &shadow); err != nil {
			log.Printf("could not unmarshal shadow payload as JSON: %v", err)
			return
		}
		log.Printf("shadow now %+v", shadow)
		if shadow.State.Reported.DustBin.Full {
			dustBinFull.Set(1)
		} else {
			dustBinFull.Set(0)
		}
		batteryPercentage.Set(float64(shadow.State.Reported.BatteryPercentage))

		stateMu.Lock()
		lastContact["roomba"] = time.Now()
		state.roombaCleaning = shadow.State.Reported.CleanMissionStatus.Phase == "run"
		stateMu.Unlock()
	})

	cl.AddRoute("wifistat", func(cl mqtt.Client, msg mqtt.Message) {
		var wifi struct {
			State struct {
				Reported struct {
					Signal struct {
						RSSI int `json:"rssi"`
						SNR  int `json:"snr"`
					} `json:"signal"`
				} `json:"reported"`
			} `json:"state"`
		}

		if err := json.Unmarshal(msg.Payload(), &wifi); err != nil {
			log.Printf("could not unmarshal wifi payload as JSON: %v", err)
			return
		}

		wifiSignalRSSI.Set(float64(wifi.State.Reported.Signal.RSSI))
		wifiSignalSNR.Set(float64(wifi.State.Reported.Signal.SNR))

		stateMu.Lock()
		lastContact["roomba"] = time.Now()
		stateMu.Unlock()
	})

	if token := cl.Connect(); token.WaitTimeout(5*time.Second) && token.Error() != nil {
		return token.Error()
	}

	for {
		select {
		case cmd := <-toRoomba:
			b, err := json.Marshal(struct {
				Command   string `json:"command"`
				TimeUnix  int64  `json:"time"`
				Initiator string `json:"initiator"`
			}{
				Command:   cmd,
				TimeUnix:  time.Now().Unix(),
				Initiator: "localApp",
			})
			if err != nil {
				return err
			}
			log.Printf("publishing %q", string(b))
			if token := cl.Publish("cmd", 0, false, string(b)); token.Wait() && token.Error() != nil {
				return token.Error()
			}
		}
	}
	return nil
}
