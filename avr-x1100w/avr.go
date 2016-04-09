package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

var toAvr = make(chan string)

func talkWithAvr() {
	fromAvr := make(chan string)
	go func() {
		for r := range fromAvr {
			log.Printf("from avr: %q\n", r)

			stateMu.Lock()
			if strings.HasPrefix(r, "PW") {
				// e.g. PWSTANDBY
				state.avrPowered = r == "PWON"
			} else if strings.HasPrefix(r, "SI") {
				// e.g. SIGAME
				state.avrSource = r[len("SI"):]

				subwooferLevel, ok := subwooferLevel[state.avrSource]
				if !ok {
					subwooferLevel = 38 // -12 dB, i.e. take out all bass
				}
				volume, ok := volume[state.avrSource]
				if !ok {
					volume = 60
				}
				toAvr <- fmt.Sprintf("MV%d\r", volume)
				toAvr <- fmt.Sprintf("PSSWL %d\r", subwooferLevel)
			}
			lastContact["avr"] = time.Now()
			stateMu.Unlock()
			stateChanged.Broadcast()
		}
	}()
	for {
		conn, err := net.Dial("tcp", "avr:23")
		if err != nil {
			log.Print(err)
			continue
		}
		go func() {
			for {
				cmd := <-toAvr
				log.Printf("to avr: %q\n", cmd)
				if _, err := conn.Write([]byte(cmd)); err != nil {
					log.Printf("Error writing to AVR: %v\n", err)
					return
				}
				time.Sleep(1 * time.Second)
			}
		}()
		fmt.Fprintf(conn, "PW?\r")
		fmt.Fprintf(conn, "SI?\r")
		r := bufio.NewReader(conn)
		for {
			line, err := r.ReadString('\r')
			if err != nil {
				break
			}
			fromAvr <- strings.TrimRight(line, "\r")
		}
		conn.Close()
	}
}
