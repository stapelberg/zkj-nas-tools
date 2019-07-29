package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func pollAVR1() error {
	ctx, canc := context.WithTimeout(context.Background(), 5*time.Second)
	defer canc()
	// query hmgo Prometheus metrics
	req, err := http.NewRequest("GET", "http://localhost:8013/metrics", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		return fmt.Errorf("unexpected HTTP status: got %v, want %d", resp.Status, want)
	}
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if !strings.HasPrefix(line, "hmpower_PowerEventPower{") {
			continue
		}
		if !strings.Contains(line, `name="avr"`) {
			continue
		}
		parts := strings.Split(line, " ")
		if len(parts) < 2 {
			log.Printf("line unexpectedly not separated by space: %q", line)
			continue
		}
		power, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			log.Printf("second part unexpectedly not a float: %v", err)
			continue
		}
		stateMu.Lock()
		state.avrPowered.Set(power > 5 /* watts */)
		lastContact["avr"] = time.Now()
		stateMu.Unlock()
		stateChanged.Broadcast()
		break
	}
	return nil
}

func pollAVR() {
	for range time.Tick(5 * time.Second) {
		if err := pollAVR1(); err != nil {
			log.Printf("[avr] poll failed: %v", err)
		}
	}
}
