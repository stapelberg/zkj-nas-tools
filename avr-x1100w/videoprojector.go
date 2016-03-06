package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/stapelberg/zkj-nas-tools/serial"
)

var toVideoProjector = make(chan string)

func sendSerialCommand(command []byte) error {
	f, err := os.OpenFile(*videoProjectorSerialPath, os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("Could not open %q: %v", *videoProjectorSerialPath, err)
	}

	if err := serial.Configure(f, 9600); err != nil {
		return fmt.Errorf("Could not configure %q to 9600 8N1: %v", *videoProjectorSerialPath, err)
	}

	if _, err := f.Write(command); err != nil {
		return fmt.Errorf("Error writing to video projector: %v", err)
	}

	return f.Close()
}

func turnOnVideoProjector() {
	if err := setSwitchState(switchStatePoweredOn); err != nil {
		log.Printf("Could not switch on video projector via Homematic: %v\n", err)
		return
	}

	// Wait up to 10 seconds for the video projector to boot up.
	start := time.Now()
	for time.Since(start) < 10*time.Second {
		log.Printf("getting power consumption\n")
		power, err := getPowerConsumption()
		if err != nil {
			log.Printf("Error getting video projector power consumption: %v\n", err)
			return
		}
		log.Printf("video projector power consumption: %f W\n", power)
		if power > 0.01 {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// Try to turn on the video projector.
	if err := sendSerialCommand([]byte("~0000 1\r")); err != nil {
		log.Printf("Error writing to video projector: %v\n", err)
		return
	}
}

func turnOffVideoProjector() {
	if err := sendSerialCommand([]byte("~0000 0\r")); err != nil {
		log.Printf("Error writing to video projector: %v\n", err)
		return
	}

	// Wait up to 30 seconds for the video projector to enter standby mode.
	start := time.Now()
	for time.Since(start) < 30*time.Second {
		power, err := getPowerConsumption()
		if err != nil {
			log.Printf("Error getting video projector power consumption: %v\n", err)
			return
		}
		log.Printf("video projector power consumption: %f W\n", power)
		if power > 0 && power < 0.5 {
			if err := setSwitchState(switchStatePoweredOff); err != nil {
				log.Printf("Could not switch off video projector via Homematic: %v\n", err)
			}
			return
		}
		time.Sleep(1 * time.Second)
	}
}

func pollVideoProjector() {
	for {
		// Query power state.
		switchstate, err := getSwitchState()
		if err != nil {
			log.Printf("Error getting video projector power state: %v", err)
		} else {
			stateMu.Lock()
			state.videoProjectorPowered = (switchstate == switchStatePoweredOn)
			lastContact["videoprojector"] = time.Now()
			stateMu.Unlock()
			stateChanged.Broadcast()
		}
		time.Sleep(5 * time.Second)
	}
}
