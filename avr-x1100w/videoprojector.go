package main

import (
	"bufio"
	"log"
	"strings"
	"time"

	"github.com/dustin/go-rs232"
)

var toVideoProjector = make(chan string)

func pollVideoProjector() {
	go func() {
		for {
			// Query power state.
			toVideoProjector <- "~00124 1\r"
			time.Sleep(5 * time.Second)
		}
	}()

	for {
		serial, err := rs232.OpenPort(*videoProjectorSerialPath, 9600, rs232.S_8N1)
		if err != nil {
			log.Printf("Could not open %q: %v\n", *videoProjectorSerialPath, err)
			continue
		}

		go func() {
			for {
				cmd := <-toVideoProjector
				log.Printf("to video projector: %q\n", cmd)
				if _, err := serial.Write([]byte(cmd)); err != nil {
					log.Printf("Error writing to video projector: %v\n", err)
					return
				}
				log.Printf("draining command channel...\n")
				time.Sleep(2 * time.Second)
			drained:
				for {
					select {
					case <-toVideoProjector:
					default:
						break drained
					}
				}
				log.Printf("command channel drained\n")
			}
		}()

		r := bufio.NewReader(serial)
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				log.Printf("error reading from video projector: %v\n", err)
				break
			}

			// The video projector sends F (or empty lines)
			trimmed := strings.TrimSpace(line)
			log.Printf("line from video projector: %q, bytes = %v\n", trimmed, []byte(line))
			if trimmed == "F" || trimmed == "\x00F" || trimmed == "" {
				continue
			}
			if strings.HasPrefix(line, "OK") {
				stateMu.Lock()
				state.videoProjectorPowered = strings.HasPrefix(line, "OK1")
				lastContact["videoprojector"] = time.Now()
				stateMu.Unlock()
				stateChanged.Broadcast()
				continue
			}
			log.Printf("Unhandled line from video projector: %q, bytes = %v\n", strings.TrimSpace(line), []byte(line))
		}

		serial.Close()
	}
}
