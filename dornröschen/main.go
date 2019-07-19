// +build !gokrazy

package main

import (
	"flag"
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	pushGateway = flag.String("prometheus_push_gateway",
		"http://pushgateway.zekjur.net:9091/",
		"URL of a https://github.com/prometheus/pushgateway instance")
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	flag.Parse()

	if err := run(); err != nil {
		log.Fatal(err)
	}

	lastSuccess := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "last_success",
		Help: "Timestamp of the last success",
	})
	prometheus.MustRegister(lastSuccess)
	lastSuccess.Set(float64(time.Now().Unix()))
	if err := prometheus.Push("dornroeschen", "dornroeschen", *pushGateway); err != nil {
		log.Fatal(err)
	}
}
