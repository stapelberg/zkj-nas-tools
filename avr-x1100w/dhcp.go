package main

import (
	"context"
	"log"
	"net/http"
	"time"
)

func pollDHCPGalaxy1() error {
	ctx, canc := context.WithTimeout(context.Background(), 5*time.Second)
	defer canc()
	req, err := http.NewRequest("GET", "http://dhcp4d.router7/lease/Galaxy-S10e", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	active := resp.Header.Get("X-Lease-Active") == "true"
	stateMu.Lock()
	state.galaxyActive.Set(active)
	lastContact["galaxy"] = time.Now()
	stateMu.Unlock()
	stateChanged.Broadcast()
	return nil
}

func pollDHCPiPhone1() error {
	ctx, canc := context.WithTimeout(context.Background(), 5*time.Second)
	defer canc()
	req, err := http.NewRequest("GET", "http://dhcp4d.router7/lease/Michaels-iPhone", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	active := resp.Header.Get("X-Lease-Active") == "true"
	stateMu.Lock()
	state.iphoneActive.Set(active)
	lastContact["iPhone"] = time.Now()
	stateMu.Unlock()
	stateChanged.Broadcast()
	return nil
}

func pollDHCP() {
	for range time.Tick(5 * time.Second) {
		if err := pollDHCPGalaxy1(); err != nil {
			log.Printf("[dhcp] poll failed: %v", err)
		}
		if err := pollDHCPiPhone1(); err != nil {
			log.Printf("[dhcp] poll failed: %v", err)
		}
	}
}
