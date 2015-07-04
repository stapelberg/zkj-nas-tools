package main

import (
	"time"

	"github.com/stapelberg/zkj-nas-tools/ping"
)

func pingZbox() {
	for {
		result := make(chan *time.Duration)
		go ping.Ping("openelec", 1*time.Second, result)
		latency := <-result

		stateMu.Lock()
		state.zboxPowered = latency != nil
		if state.zboxPowered {
			lastContact["zbox"] = time.Now()
		}
		stateMu.Unlock()
		stateChanged.Broadcast()

		if latency != nil {
			time.Sleep(1*time.Second - *latency)
		}
	}
}
