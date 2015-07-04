package main

import (
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"
)

func getUnlockedStatus(addr string) (string, error) {
	client := http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Get(addr)
	if err != nil {
		return "", err
	}
	status, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(status)), nil
}

func pollMidna() {
	for {
		time.Sleep(1 * time.Second)
		status, err := getUnlockedStatus(*midnaURL)
		if err != nil {
			log.Printf("Could not poll midna: %v\n", err)
			// falling through with status == "", i.e. midnaUnlocked = false.
		}

		stateMu.Lock()
		state.midnaUnlocked = (status == "notrunning")
		lastContact["midna"] = time.Now()
		stateMu.Unlock()
		stateChanged.Broadcast()
	}
}
