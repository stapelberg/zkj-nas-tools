package main

import (
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

func getCurrentChannel(addr string) (int, error) {
	client := http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Get(addr)
	if err != nil {
		return 0, err
	}
	b, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return 0, err
	}
	channel, err := strconv.ParseInt(strings.TrimSpace(string(b)), 0, 64)
	if err != nil {
		return 0, err
	}
	return int(channel), nil
}

var difmxMu sync.Mutex // prevent polling from interfering with switching

func pollDifmx() {
	for {
		time.Sleep(1 * time.Second)
		difmxMu.Lock()
		channel, err := getCurrentChannel("http://selecta:8043/current")
		difmxMu.Unlock()
		if err != nil {
			log.Printf("Could not poll difmx: %v\n", err)
			continue
		}

		stateMu.Lock()
		_ = channel
		//state.difmxChannel = channel
		lastContact["difmx"] = time.Now()
		stateMu.Unlock()
		stateChanged.Broadcast()
	}
}

func switchDifmxChannel(channel int) error {
	difmxMu.Lock()
	defer difmxMu.Unlock()
	_, err := http.Post("http://selecta:8043/select?target="+strconv.Itoa(channel), "application/octet-stream", nil)
	return err
}
