package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gokrazy/gokrazy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	_ "net/http/pprof"
)

var (
	listen = flag.String("listen",
		":8014",
		"[host]:port to listen on (for prometheus HTTP exports)")

	lastSuccessPath = flag.String("last_success_path",
		"/perm/dr-last-success.txt",
		"path to a file in which to load/store the last success timestamp")
)

var lastSuccess = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "last_success",
	Help: "Timestamp of the last success",
})

func init() {
	prometheus.MustRegister(lastSuccess)
}

func loadLastSuccess() error {
	b, err := ioutil.ReadFile(*lastSuccessPath)
	if err != nil {
		return err
	}
	i, err := strconv.ParseInt(strings.TrimSpace(string(b)), 0, 64)
	if err != nil {
		return err
	}
	lastSuccess.Set(float64(i))
	return nil
}

func main() {
	gokrazy.WaitForClock()

	if err := loadLastSuccess(); err != nil {
		log.Printf("could not load last success timestamp from disk: %v", err)
	}

	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(*listen, nil)

	runCh := make(chan struct{})
	go func() {
		// Run forever, trigger a run at 10:00 each Monday through Friday.
		for {
			now := time.Now()
			runToday := now.Hour() < 10 &&
				now.Weekday() != time.Saturday &&
				now.Weekday() != time.Sunday
			today := now.Day()
			log.Printf("now = %v, runToday = %v", now, runToday)
			for {
				if time.Now().Day() != today {
					// Day changed, re-evaluate whether to run today.
					break
				}

				nextHour := time.Now().Truncate(time.Hour).Add(1 * time.Hour)
				log.Printf("today = %d, runToday = %v, next hour: %v", today, runToday, nextHour)
				time.Sleep(time.Until(nextHour))

				if time.Now().Hour() >= 10 && runToday {
					runToday = false
					runCh <- struct{}{}
				}
			}
		}
	}()
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGUSR1)
		for range c {
			log.Printf("received SIGUSR1, starting run")
			runCh <- struct{}{}
		}
	}()

	for range runCh {
		log.Println("Running dornröschen")
		if err := run(); err != nil {
			log.Printf("failed: %v", err)
		}
		unix := time.Now().Unix()
		lastSuccess.Set(float64(unix))
		if err := ioutil.WriteFile(*lastSuccessPath, []byte(fmt.Sprintf("%d", unix)), 0600); err != nil {
			log.Printf("could not persist last success timestamp to disk: %v", err)
		}

	}
}
