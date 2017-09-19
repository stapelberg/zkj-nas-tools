package main

import (
	"time"

	"github.com/stapelberg/zkj-nas-tools/ping"
)

func pingBeast() {
	for {
		result := make(chan *time.Duration)
		go ping.Ping("beast", 1*time.Second, result)
		latency := <-result

		stateMu.Lock()
		state.beastPowered = latency != nil
		if state.beastPowered {
			lastContact["beast"] = time.Now()
		}
		stateMu.Unlock()
		stateChanged.Broadcast()

		if latency != nil {
			time.Sleep(1*time.Second - *latency)
		} else {
			time.Sleep(1 * time.Second)
		}
	}
}
